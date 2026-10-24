package subsonic_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestCompatibilityStubsHaveNoSideEffects(t *testing.T) {
	f := newFixture(t, "", "")
	var requests atomic.Int64
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "must not fetch", http.StatusInternalServerError)
	}))
	defer remote.Close()
	for _, scope := range []string{"podcasts", "shares", "radio", "chat"} {
		if _, err := f.db.Exec("INSERT INTO app_state(scope,key,value) VALUES (?,?,?)", scope, "old", `{"legacy":true}`); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		endpoint string
		args     url.Values
	}{
		{"createPodcastChannel", url.Values{"url": {remote.URL}}},
		{"refreshPodcasts", nil},
		{"deletePodcastChannel", url.Values{"id": {"old"}}},
		{"downloadPodcastEpisode", url.Values{"id": {"old"}}},
		{"deletePodcastEpisode", url.Values{"id": {"old"}}},
		{"createInternetRadioStation", url.Values{"name": {"Ignored"}, "streamUrl": {remote.URL}}},
		{"updateInternetRadioStation", url.Values{"id": {"old"}, "name": {"Ignored"}, "streamUrl": {remote.URL}}},
		{"deleteInternetRadioStation", url.Values{"id": {"old"}}},
		{"addChatMessage", url.Values{"message": {"Ignored"}}},
		{"createShare", url.Values{"id": {"old"}}},
		{"updateShare", url.Values{"id": {"old"}}},
		{"deleteShare", url.Values{"id": {"old"}}},
		{"star", url.Values{"id": {"old"}, "albumId": {"old"}, "artistId": {"old"}}},
		{"unstar", url.Values{"id": {"old"}}},
		{"setRating", url.Values{"id": {"old"}, "rating": {"5"}}},
	}
	for _, tc := range cases {
		t.Run(tc.endpoint, func(t *testing.T) { f.ok(t, tc.endpoint, tc.args) })
	}
	for _, tc := range []struct{ endpoint, parent, child string }{
		{"getPodcasts", "podcasts", "channel"},
		{"getNewestPodcasts", "newestPodcasts", "episode"},
		{"getVideos", "videos", "video"},
		{"getInternetRadioStations", "internetRadioStations", "internetRadioStation"},
		{"getChatMessages", "chatMessages", "chatMessage"},
		{"getShares", "shares", "share"},
		{"createShare", "shares", "share"},
		{"getStarred", "starred", "song"},
		{"getStarred2", "starred2", "album"},
	} {
		t.Run(tc.endpoint+"/empty", func(t *testing.T) {
			body := f.ok(t, tc.endpoint, url.Values{"id": {"old"}})
			value := body[tc.parent].(map[string]any)[tc.child]
			if items, ok := value.([]any); !ok || len(items) != 0 {
				t.Fatalf("expected an empty array, got %#v", value)
			}
		})
	}
	info := f.ok(t, "getVideoInfo", url.Values{"id": {"old"}})["videoInfo"].(map[string]any)
	if info["id"] != "old" {
		t.Fatal(info)
	}
	for _, key := range []string{"captions", "audioTrack", "conversion"} {
		if entries, ok := info[key].([]any); !ok || len(entries) != 0 {
			t.Fatalf("videoInfo.%s = %#v", key, info[key])
		}
	}
	if requests.Load() != 0 {
		t.Fatal("stub made outbound requests")
	}
	var count int
	if err := f.db.QueryRow("SELECT count(*) FROM app_state").Scan(&count); err != nil || count != 4 {
		t.Fatalf("stub changed stored state: count=%d, err=%v", count, err)
	}
	for _, scope := range []string{"podcasts", "shares", "radio", "chat"} {
		var raw string
		if err := f.db.QueryRow("SELECT value FROM app_state WHERE scope=? AND key='old'", scope).Scan(&raw); err != nil || raw != `{"legacy":true}` {
			t.Fatalf("stub accessed old %s state: %q, %v", scope, raw, err)
		}
	}
}

func TestOnlyConfiguredAccountCanAuthenticate(t *testing.T) {
	f := newFixture(t, "", "")
	const oldAccount = `{"Username":"admin","Password":"obsolete encrypted credential"}`
	if _, err := f.db.Exec("INSERT INTO app_state(scope,key,value) VALUES ('subsonic-users','admin',?)", oldAccount); err != nil {
		t.Fatal(err)
	}
	f.ok(t, "createUser", url.Values{"username": {"other"}, "password": {"pass"}, "email": {"x@example.test"}})
	f.ok(t, "updateUser", url.Values{"username": {"admin"}, "password": {"changed"}, "maxBitRate": {"64"}, "podcastRole": {"true"}})
	f.ok(t, "changePassword", url.Values{"username": {"admin"}, "password": {"changed"}})
	f.ok(t, "deleteUser", url.Values{"username": {"admin"}})
	f.ok(t, "ping", nil)
	expectCode(t, f.call(t, "ping", url.Values{"u": {"other"}, "p": {"pass"}}), 40)
	expectCode(t, f.call(t, "ping", url.Values{"p": {"changed"}}), 40)
	users := list(f.ok(t, "getUsers", nil), "users", "user")
	if len(users) != 1 || users[0].(map[string]any)["username"] != "admin" {
		t.Fatal(users)
	}
	user := f.ok(t, "getUser", url.Values{"username": {"admin"}})["user"].(map[string]any)
	for _, role := range []string{"settingsRole", "jukeboxRole", "podcastRole", "shareRole", "videoConversionRole", "commentRole"} {
		if user[role] != false {
			t.Fatalf("advertised removed role: %s", role)
		}
	}
	var count int
	if err := f.db.QueryRow("SELECT count(*) FROM app_state WHERE scope='subsonic-users'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("account management wrote state: %d, %v", count, err)
	}
	var raw string
	if err := f.db.QueryRow("SELECT value FROM app_state WHERE scope='subsonic-users' AND key='admin'").Scan(&raw); err != nil || raw != oldAccount {
		t.Fatalf("account management changed old state: %q, %v", raw, err)
	}
	expectCode(t, f.call(t, "getUser", url.Values{"username": {"other"}}), 70)
}

func TestRemovedRatingsDoNotLeakFromOldState(t *testing.T) {
	f := newFixture(t, "", "")
	track, _, _ := f.ids(t)
	if _, err := f.db.Exec("INSERT INTO app_state(scope,key,value) VALUES (?,?,?)", "annotations/admin", track, `{"starred":"2020-01-01T00:00:00Z","rating":5,"plays":3}`); err != nil {
		t.Fatal(err)
	}
	f.ok(t, "setRating", url.Values{"id": {track}, "rating": {"1"}})
	f.ok(t, "star", url.Values{"id": {track}})
	song := f.ok(t, "getSong", url.Values{"id": {track}})["song"].(map[string]any)
	if song["starred"] != nil || song["userRating"] != nil || song["playCount"] != float64(3) {
		t.Fatal(song)
	}
	if len(list(f.ok(t, "getStarred2", nil), "starred2", "song")) != 0 {
		t.Fatal("old favorites are still visible")
	}
	for _, endpoint := range []string{"getAlbumList", "getAlbumList2"} {
		parent := "albumList"
		if endpoint == "getAlbumList2" {
			parent = "albumList2"
		}
		for _, kind := range []string{"highest", "starred"} {
			if len(list(f.ok(t, endpoint, url.Values{"type": {kind}}), parent, "album")) != 0 {
				t.Fatalf("%s still has %s albums", endpoint, kind)
			}
		}
	}
}

func TestEmptyBinaryCompatibilityResponses(t *testing.T) {
	f := newFixture(t, "", "")
	if err := os.MkdirAll(filepath.Join(f.data, "avatars"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.data, "avatars", "admin.png"), []byte("old avatar"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ endpoint, key, value, contentType, body string }{
		{"getAvatar", "username", "admin", "image/png", ""},
		{"getCaptions", "id", "old", "text/plain; charset=utf-8", ""},
		{"getCaptions", "id", "old", "text/vtt; charset=utf-8", "WEBVTT\n\n"},
	} {
		args := url.Values{tc.key: {tc.value}}
		if tc.contentType == "text/vtt; charset=utf-8" {
			args.Set("format", "vtt")
		}
		for _, method := range []string{"GET", "HEAD"} {
			args.Set("u", "admin")
			args.Set("p", "secret")
			args.Set("v", "1.16.1")
			args.Set("c", "tests")
			response := httptest.NewRecorder()
			f.server.Handler.ServeHTTP(response, httptest.NewRequest(method, "/rest/"+tc.endpoint+"?"+args.Encode(), nil))
			body := tc.body
			if method == "HEAD" {
				body = ""
			}
			if response.Code != 200 || response.Header().Get("Content-Type") != tc.contentType || response.Body.String() != body {
				t.Fatalf("%s %s: %d %s %q", method, tc.endpoint, response.Code, response.Header().Get("Content-Type"), response.Body.String())
			}
		}
	}
}

func TestPublicSharingRoutesAreGone(t *testing.T) {
	f := newFixture(t, "", "")
	for _, path := range []string{"/share/old", "/share/old/stream/track"} {
		response := httptest.NewRecorder()
		f.server.Handler.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s: %d", path, response.Code)
		}
	}
}
