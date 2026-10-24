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
