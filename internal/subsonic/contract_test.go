package subsonic_test

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
)

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
	f.ok(t, "updateUser", url.Values{"username": {"admin"}, "shareRole": {"true"}, "podcastRole": {"true"}, "jukeboxRole": {"true"}})
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
func TestFilesystemIDsRoundTripAcrossLegacyEndpoints(t *testing.T) {
	f := newFixture(t, "", "")
	indexes := list(f.ok(t, "getIndexes", nil), "indexes", "index")
	artist := indexes[0].(map[string]any)["artist"].([]any)[0].(map[string]any)["id"].(string)
	dir := list(f.ok(t, "getMusicDirectory", url.Values{"id": {artist}}), "directory", "child")[0].(map[string]any)["id"].(string)
	f.ok(t, "star", url.Values{"id": {artist, dir}})
	starred := f.ok(t, "getStarred", nil)
	if len(list(starred, "starred", "artist")) != 1 || len(list(starred, "starred", "album")) != 1 {
		t.Fatal(starred)
	}
	if got := list(starred, "starred", "album")[0].(map[string]any); got["id"] != dir || got["isDir"] != true || got["title"] != "First" {
		t.Fatal(got)
	}
	search := f.ok(t, "search2", url.Values{"query": {"First"}})
	if got := list(search, "searchResult2", "album")[0].(map[string]any)["id"]; got != dir {
		t.Fatal(got)
	}
	f.ok(t, "setRating", url.Values{"id": {dir}, "rating": {"5"}})
	albums := list(f.ok(t, "getAlbumList", url.Values{"type": {"highest"}}), "albumList", "album")
	if albums[0].(map[string]any)["id"] != dir {
		t.Fatal(albums)
	}
	for _, endpoint := range []string{"getArtistInfo", "getAlbumInfo", "getSimilarSongs"} {
		f.ok(t, endpoint, url.Values{"id": {dir}})
	}
	f.ok(t, "updateUser", url.Values{"username": {"admin"}, "shareRole": {"true"}})
	share := list(f.ok(t, "createShare", url.Values{"id": {artist}}), "shares", "share")[0].(map[string]any)
	if len(share["entry"].([]any)) != 2 {
		t.Fatal(share)
	}
	f.ok(t, "unstar", url.Values{"id": {artist, dir}})
	if len(list(f.ok(t, "getStarred", nil), "starred", "album")) != 0 {
		t.Fatal("directory unstar failed")
	}
}
func TestAccountDefaultsBitrateAndAdminPlaylistListing(t *testing.T) {
	f := newFixture(t, "", "")
	f.ok(t, "createUser", url.Values{"username": {"reader"}, "password": {"pass"}, "email": {"r@example.test"}})
	reader := url.Values{"u": {"reader"}, "p": {"pass"}, "username": {"reader"}}
	account := f.ok(t, "getUser", reader)["user"].(map[string]any)
	if account["playlistRole"] != false {
		t.Fatal(account)
	}
	f.ok(t, "updateUser", url.Values{"username": {"reader"}, "maxBitRate": {"64"}})
	if f.ok(t, "getUser", reader)["user"].(map[string]any)["maxBitRate"] != float64(64) {
		t.Fatal("bitrate not persisted")
	}
	expectCode(t, f.call(t, "updateUser", url.Values{"username": {"reader"}, "maxBitRate": {"65"}}), 10)
	f.ok(t, "createPlaylist", url.Values{"u": {"reader"}, "p": {"pass"}, "name": {"Private"}})
	expectCode(t, f.call(t, "getPlaylists", reader), 50)
	if got := list(f.ok(t, "getPlaylists", url.Values{"username": {"reader"}}), "playlists", "playlist"); len(got) != 1 {
		t.Fatal(got)
	}
}
