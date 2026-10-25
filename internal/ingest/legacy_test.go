package ingest_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/prayujt/wavelength/internal/ingest"
	"github.com/prayujt/wavelength/internal/transcode"
	"github.com/prayujt/wavelength/internal/transport/subsonic"
)

func TestLegacyViewsObservePublicationWithoutAScan(t *testing.T) {
	f := setup(t, writeAudio, metadata{}, nil)
	encoder := transcode.New(context.Background(), f.lib)
	defer encoder.Close()
	legacy, err := subsonic.New(subsonic.Config{Username: "legacy", Password: "secret"}, subsonic.Dependencies{Library: f.lib, UserData: f.data, Transcoder: encoder})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(legacy.Handler)
	defer server.Close()
	fetch := func(endpoint string, extra url.Values) map[string]any {
		t.Helper()
		q := url.Values{"u": {"legacy"}, "p": {"secret"}, "v": {"1.16.1"}, "c": {"ingestion-test"}, "f": {"json"}}
		for key, value := range extra {
			q[key] = value
		}
		response, err := http.Get(server.URL + "/rest/" + endpoint + "?" + q.Encode())
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var envelope map[string]any
		if err = json.NewDecoder(response.Body).Decode(&envelope); err != nil {
			t.Fatal(err)
		}
		out := envelope["subsonic-response"].(map[string]any)
		if out["status"] != "ok" {
			t.Fatal(out)
		}
		return out
	}
	before := fetch("getIndexes", nil)["indexes"].(map[string]any)
	if len(before["index"].([]any)) != 0 {
		t.Fatal(before)
	}
	j := create(t, f.s, "alice", request("legacy"))
	j = wait(t, f.s, "alice", j.ID, ingest.Completed)
	after := fetch("getIndexes", nil)["indexes"].(map[string]any)
	indexes := after["index"].([]any)
	if len(indexes) != 1 {
		t.Fatal(after)
	}
	artist := indexes[0].(map[string]any)["artist"].([]any)[0].(map[string]any)
	if artist["name"] != "Artist" {
		t.Fatal("generated folders do not support legacy artist browsing", artist)
	}
	found := fetch("search3", url.Values{"query": {"Track"}})["searchResult3"].(map[string]any)["song"].([]any)
	if len(found) != 1 || found[0].(map[string]any)["id"] != j.Items[0].TrackID {
		t.Fatal(found)
	}
	scan := fetch("startScan", nil)["scanStatus"].(map[string]any)
	if scan["scanning"] != false || scan["count"] != float64(1) {
		t.Fatal(scan)
	}
	q := url.Values{"u": {"legacy"}, "p": {"secret"}, "v": {"1.16.1"}, "c": {"ingestion-test"}, "id": {j.Items[0].TrackID}}
	response, err := http.Get(server.URL + "/rest/stream?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil || string(raw) != "prepared audio" {
		t.Fatal(string(raw), err)
	}
}
