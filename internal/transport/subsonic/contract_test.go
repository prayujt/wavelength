package subsonic_test

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMusicFolderIDsDecodeAsJSONIntegers(t *testing.T) {
	f := newFixture(t, "", "")
	type user struct {
		Folders []int `json:"folder"`
	}
	var envelope struct {
		Response struct {
			User         user
			Users        struct{ User []user }
			MusicFolders struct {
				MusicFolder []struct{ ID int }
			}
		} `json:"subsonic-response"`
	}
	for _, endpoint := range []string{"getUser", "getUsers", "getMusicFolders"} {
		t.Run(endpoint, func(t *testing.T) {
			response := f.raw(t, endpoint, url.Values{"username": {"admin"}})
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("strict client could not decode %s: %v", endpoint, err)
			}
		})
	}
	if !reflect.DeepEqual(envelope.Response.User.Folders, []int{1}) ||
		len(envelope.Response.Users.User) != 1 || !reflect.DeepEqual(envelope.Response.Users.User[0].Folders, []int{1}) ||
		len(envelope.Response.MusicFolders.MusicFolder) != 1 || envelope.Response.MusicFolders.MusicFolder[0].ID != 1 {
		t.Fatal("user permissions and music folders must reference the same integer ID")
	}
}

func TestDocumentedCountsAreNotLimitedTo500(t *testing.T) {
	f := newFixture(t, "", "")
	_, _, artist := f.ids(t)
	for _, tc := range []struct {
		endpoint string
		args     url.Values
	}{
		{"search", url.Values{"count": {"1000"}}},
		{"search2", url.Values{"query": {""}, "artistCount": {"1000"}, "albumCount": {"1000"}, "songCount": {"1000"}}},
		{"search3", url.Values{"query": {""}, "artistCount": {"1000"}, "albumCount": {"1000"}, "songCount": {"1000"}}},
		{"getArtistInfo", url.Values{"id": {artist}, "count": {"1000"}}},
		{"getArtistInfo2", url.Values{"id": {artist}, "count": {"1000"}}},
		{"getSimilarSongs", url.Values{"id": {artist}, "count": {"1000"}}},
		{"getSimilarSongs2", url.Values{"id": {artist}, "count": {"1000"}}},
		{"getTopSongs", url.Values{"artist": {"Artist A"}, "count": {"1000"}}},
	} {
		t.Run(tc.endpoint, func(t *testing.T) { f.ok(t, tc.endpoint, tc.args) })
	}
	for _, endpoint := range []string{"getAlbumList", "getAlbumList2", "getRandomSongs", "getSongsByGenre"} {
		args := url.Values{"type": {"random"}, "genre": {"Rock"}, "size": {"501"}, "count": {"501"}}
		expectCode(t, f.call(t, endpoint, args), 10)
	}
}

func TestLyricsDoNotMatchADifferentTitle(t *testing.T) {
	music := t.TempDir()
	writeSong(t, music, "one.mp3", "One", "Artist", "Album", 1)
	writeSong(t, music, "more.mp3", "One More", "Artist", "Album", 2)
	if err := os.WriteFile(filepath.Join(music, "more.lrc"), []byte("Wrong song"), 0600); err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, "", music)
	lyrics := f.ok(t, "getLyrics", url.Values{"artist": {"Artist"}, "title": {"One"}})["lyrics"].(map[string]any)
	if lyrics["value"] != "" {
		t.Fatal("lyrics for a different title were returned", lyrics)
	}
}

func TestLyricsSearchContinuesPastTheFirstCatalogPage(t *testing.T) {
	music := t.TempDir()
	for i := range 500 {
		writeSong(t, music, fmt.Sprintf("%03d.mp3", i), "One", "A", "Album", 1)
	}
	writeSong(t, music, "target.mp3", "One", "Z", "Album", 1)
	if err := os.WriteFile(filepath.Join(music, "target.lrc"), []byte("Requested lyrics"), 0600); err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, "", music)
	lyrics := f.ok(t, "getLyrics", url.Values{"artist": {"Z"}, "title": {"One"}})["lyrics"].(map[string]any)
	if lyrics["value"] != "Requested lyrics" {
		t.Fatal("matching lyrics beyond the first 500 catalog entries were omitted", lyrics)
	}
}

func TestRecentAlbumsCompareScrobbleTimesChronologically(t *testing.T) {
	f := newFixture(t, "", "")
	a, _, _ := f.ids(t)
	b := list(f.ok(t, "search3", url.Values{"query": {"Three"}}), "searchResult3", "song")[0].(map[string]any)["id"].(string)
	stamp := time.Now().Truncate(time.Second).UnixMilli()
	f.ok(t, "scrobble", url.Values{"id": {a, b}, "time": {fmt.Sprint(stamp), fmt.Sprint(stamp + 1)}})
	for _, endpoint := range []string{"getAlbumList", "getAlbumList2"} {
		name := "albumList"
		if strings.HasSuffix(endpoint, "2") {
			name += "2"
		}
		albums := list(f.ok(t, endpoint, url.Values{"type": {"recent"}}), name, "album")
		if len(albums) != 2 || albums[0].(map[string]any)["name"] != "Second" {
			t.Fatalf("%s: most recently scrobbled album must come first: %v", endpoint, albums)
		}
	}
	playing := list(f.ok(t, "getNowPlaying", nil), "nowPlaying", "entry")
	if len(playing) != 1 || playing[0].(map[string]any)["id"] != b {
		t.Fatal("submitted scrobbles must also update now playing", playing)
	}
}

func TestMediaErrorsUseLegacyXMLContentType(t *testing.T) {
	f := newFixture(t, "", "")
	for _, endpoint := range []string{"stream", "download", "hls", "getCoverArt"} {
		t.Run(endpoint, func(t *testing.T) {
			response := f.raw(t, endpoint, url.Values{"id": {"missing"}, "f": {"xml"}})
			if !strings.HasPrefix(response.Header().Get("Content-Type"), "text/xml") {
				t.Fatalf("client cannot distinguish media from a legacy XML error: %v", response.Header())
			}
			var body struct {
				Status string `xml:"status,attr"`
				Error  struct {
					Code int `xml:"code,attr"`
				} `xml:"error"`
			}
			if err := xml.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Status != "failed" || body.Error.Code != 70 {
				t.Fatalf("missing media error: %s (%v)", response.Body, err)
			}
		})
	}
}

func TestInvalidTranscodingParametersReturnProtocolErrors(t *testing.T) {
	mediaTools(t)
	f := newFixture(t, "", "")
	track, _, _ := f.ids(t)
	for _, args := range []url.Values{
		{"id": {track}, "format": {"not-an-audio-format"}},
		{"id": {track}, "format": {"mp3"}, "maxBitRate": {"64"}, "estimateContentLength": {"not-a-boolean"}},
	} {
		expectCode(t, f.call(t, "stream", args), 10)
	}
}

func TestDocumentedRequiredParameters(t *testing.T) {
	raw, err := os.ReadFile("testdata/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		Endpoints map[string][]struct{ Name, Required, Default string }
	}
	if err = json.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	if len(contract.Endpoints) != 76 {
		t.Fatalf("contract has %d endpoints", len(contract.Endpoints))
	}
	f := newFixture(t, "", "")
	f.ok(t, "updateUser", url.Values{"username": {"admin"}, "shareRole": {"true"}, "podcastRole": {"true"}})
	for endpoint, params := range contract.Endpoints {
		for _, param := range params {
			if !strings.EqualFold(param.Required, "yes") || endpoint == "savePlayQueue" {
				continue
			} // Empty queue is the clearing operation.
			t.Run(endpoint+"/"+param.Name, func(t *testing.T) {
				args := url.Values{}
				for _, p := range params {
					if strings.EqualFold(p.Required, "yes") {
						args.Set(p.Name, "missing-resource")
					}
				}
				args.Del(param.Name)
				expectCode(t, f.call(t, endpoint, args), 10)
			})
		}
	}
}

func TestJukeboxIsExplicitlyUnsupported(t *testing.T) {
	f := newFixture(t, "", "")
	expectCode(t, f.call(t, "jukeboxControl", url.Values{"action": {"status"}}), 0)
	user := f.ok(t, "getUser", url.Values{"username": {"admin"}})["user"].(map[string]any)
	if user["jukeboxRole"] != false || user["scrobblingEnabled"] != false {
		t.Fatal("removed integrations advertised as enabled")
	}
	f.ok(t, "updateUser", url.Values{"username": {"admin"}, "jukeboxRole": {"true"}})
}
func TestFilesystemIDsRoundTripAcrossLegacyEndpoints(t *testing.T) {
	f := newFixture(t, "", "")
	indexes := list(f.ok(t, "getIndexes", nil), "indexes", "index")
	artist := indexes[0].(map[string]any)["artist"].([]any)[0].(map[string]any)["id"].(string)
	dir := list(f.ok(t, "getMusicDirectory", url.Values{"id": {artist}}), "directory", "child")[0].(map[string]any)["id"].(string)
	search := f.ok(t, "search2", url.Values{"query": {"First"}})
	if got := list(search, "searchResult2", "album")[0].(map[string]any)["id"]; got != dir {
		t.Fatal(got)
	}
	f.ok(t, "getMusicDirectory", url.Values{"id": {dir}})
	for _, endpoint := range []string{"getArtistInfo", "getAlbumInfo", "getSimilarSongs"} {
		f.ok(t, endpoint, url.Values{"id": {dir}})
	}
}
