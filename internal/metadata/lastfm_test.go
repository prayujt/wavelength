package metadata_test

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prayujt/wavelength/internal/metadata"
)

func TestProviderMetadataAndSignedScrobbles(t *testing.T) {
	methods := map[string]int{}
	var mutex sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		method := r.Form.Get("method")
		mutex.Lock()
		methods[method]++
		mutex.Unlock()
		if r.Form.Get("api_key") != "test-key" {
			t.Error("missing API key")
		}
		w.Header().Set("Content-Type", "application/json")
		switch method {
		case "artist.getInfo":
			_ = json.NewEncoder(w).Encode(map[string]any{"artist": map[string]any{"name": "Fixture", "mbid": "artist-mbid", "url": "https://last.fm/fixture", "bio": map[string]any{"content": "Fixture biography"}, "similar": map[string]any{"artist": []any{map[string]any{"name": "Neighbor"}}}}})
		case "album.getInfo":
			_ = json.NewEncoder(w).Encode(map[string]any{"album": map[string]any{"name": "Album", "wiki": map[string]any{"content": "Fixture notes"}}})
		case "artist.getTopTracks":
			_ = json.NewEncoder(w).Encode(map[string]any{"toptracks": map[string]any{"track": []any{map[string]any{"name": "Track", "artist": map[string]any{"name": "Fixture"}}}}})
		case "track.scrobble", "track.updateNowPlaying":
			if r.Method != "POST" || r.Form.Get("sk") != "test-session" {
				t.Error("incorrect authenticated POST")
			}
			keys := []string{}
			for key := range r.Form {
				if key != "api_sig" && key != "format" {
					keys = append(keys, key)
				}
			}
			sort.Strings(keys)
			var signature strings.Builder
			for _, key := range keys {
				signature.WriteString(key)
				signature.WriteString(r.Form.Get(key))
			}
			signature.WriteString("test-secret")
			if r.Form.Get("api_sig") != fmt.Sprintf("%x", md5.Sum([]byte(signature.String()))) {
				t.Error("invalid Last.fm signature")
			}
			if method == "track.scrobble" && r.Form.Get("timestamp") != "1234567890" {
				t.Error("wrong scrobble timestamp")
			}
			_, _ = w.Write([]byte(`{"scrobbles":{"@attr":{"accepted":"1","ignored":"0"}}}`))
		default:
			t.Errorf("unexpected method %s", method)
			w.WriteHeader(400)
		}
	}))
	defer server.Close()
	provider := &metadata.LastFM{APIKey: "test-key", Secret: "test-secret", BaseURL: server.URL, Sessions: map[string]string{"listener": "test-session"}}
	artist, err := provider.Artist(context.Background(), "Fixture")
	if err != nil || artist.Bio.Content != "Fixture biography" || len(artist.Similar.Artists) != 1 {
		t.Fatalf("artist %v %v", artist, err)
	}
	album, err := provider.Album(context.Background(), "Fixture", "Album")
	if err != nil || album.Wiki.Content != "Fixture notes" {
		t.Fatalf("album %v %v", album, err)
	}
	tracks, err := provider.TopTracks(context.Background(), "Fixture", 20)
	if err != nil || len(tracks) != 1 {
		t.Fatalf("tracks %v %v", tracks, err)
	}
	if err = provider.Submit(context.Background(), "listener", "Fixture", "Track", "Album", time.Unix(1234567890, 0), true); err != nil {
		t.Fatal(err)
	}
	if err = provider.Submit(context.Background(), "listener", "Fixture", "Track", "Album", time.Now(), false); err != nil {
		t.Fatal(err)
	}
	if provider.Enabled("unknown") || !provider.Enabled("listener") {
		t.Fatal("per-user scrobbling config ignored")
	}
	mutex.Lock()
	defer mutex.Unlock()
	for _, method := range []string{"artist.getInfo", "album.getInfo", "artist.getTopTracks", "track.scrobble", "track.updateNowPlaying"} {
		if methods[method] != 1 {
			t.Fatalf("method %s: %d", method, methods[method])
		}
	}
}
func TestProviderDoesNotLeakCredentialsInFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	address := server.URL
	server.Close()
	provider := &metadata.LastFM{APIKey: "secret-key", BaseURL: address}
	_, err := provider.Artist(context.Background(), "Fixture")
	if err == nil || strings.Contains(err.Error(), "secret-key") {
		t.Fatal("unsafe provider error", err)
	}
	_ = url.Values{}
}
