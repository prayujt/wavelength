package subsonic_test

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prayujt/wavelength/internal/database"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/subsonic"
)

func TestLegacyAuthenticationBrowseAndStreaming(t *testing.T) {
	db, err := database.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	root := t.TempDir()
	data := make([]byte, 256)
	copy(data[128:], "TAG")
	copy(data[131:161], "A song")
	copy(data[161:191], "Artist")
	copy(data[191:221], "Album")
	if err = os.WriteFile(filepath.Join(root, "song.mp3"), data, 0600); err != nil {
		t.Fatal(err)
	}
	lib, err := library.Open(root, database.Catalog{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	if _, err = lib.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	server, err := subsonic.New("", lib, "listener", "secret")
	if err != nil {
		t.Fatal(err)
	}
	get := func(endpoint string, args url.Values) *httptest.ResponseRecorder {
		t.Helper()
		if args == nil {
			args = url.Values{}
		}
		if !args.Has("u") {
			args.Set("u", "listener")
		}
		if !args.Has("p") && !args.Has("t") {
			args.Set("p", "secret")
		}
		args.Set("v", "1.16.1")
		args.Set("c", "tests")
		r := httptest.NewRequest("GET", "/rest/"+endpoint+"?"+args.Encode(), nil)
		w := httptest.NewRecorder()
		server.Handler.ServeHTTP(w, r)
		return w
	}
	decode := func(w *httptest.ResponseRecorder) map[string]any {
		t.Helper()
		var body map[string]map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body["subsonic-response"]
	}
	args := url.Values{"f": {"json"}, "s": {"salt123"}, "t": {fmt.Sprintf("%x", md5.Sum([]byte("secretsalt123")))}}
	if body := decode(get("ping.view", args)); body["status"] != "ok" {
		t.Fatalf("token auth: %v", body)
	}
	if body := decode(get("ping", url.Values{"f": {"json"}, "p": {"enc:736563726574"}})); body["status"] != "ok" {
		t.Fatal(body)
	}
	if body := decode(get("ping", url.Values{"f": {"json"}, "p": {"wrong"}})); body["error"].(map[string]any)["code"] != float64(40) {
		t.Fatal(body)
	}
	xmlResult := get("ping", nil)
	var envelope struct {
		XMLName xml.Name
		Status  string `xml:"status,attr"`
	}
	if err = xml.Unmarshal(xmlResult.Body.Bytes(), &envelope); err != nil || envelope.Status != "ok" || envelope.XMLName.Space != "http://subsonic.org/restapi" {
		t.Fatalf("XML: %s %v", xmlResult.Body, err)
	}
	body := decode(get("search3", url.Values{"f": {"json"}, "query": {"song"}}))
	songs := body["searchResult3"].(map[string]any)["song"].([]any)
	if len(songs) != 1 {
		t.Fatal(body)
	}
	song := songs[0].(map[string]any)
	trackID := song["id"].(string)
	if body := decode(get("getAlbum", url.Values{"f": {"json"}, "id": {song["albumId"].(string)}})); body["album"].(map[string]any)["songCount"] != float64(1) {
		t.Fatal(body)
	}
	if body := decode(get("getArtist", url.Values{"f": {"json"}, "id": {song["artistId"].(string)}})); body["artist"].(map[string]any)["albumCount"] != float64(1) {
		t.Fatal(body)
	}
	if body := decode(get("getArtists", url.Values{"f": {"json"}})); body["artists"] == nil {
		t.Fatal(body)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/rest/stream.view?u=listener&p=secret&v=1.16.1&c=tests&id="+trackID, nil)
	req.Header.Set("Range", "bytes=128-130")
	server.Handler.ServeHTTP(w, req)
	if w.Code != 206 || w.Body.String() != "TAG" {
		t.Fatalf("stream: %d %s", w.Code, w.Body)
	}
	if body := decode(get("getSong", url.Values{"f": {"json"}, "id": {"missing"}})); body["error"].(map[string]any)["code"] != float64(70) {
		t.Fatal(body)
	}
	if body := decode(get("search3", url.Values{"f": {"json"}, "songCount": {"-1"}})); body["status"] != "failed" {
		t.Fatal(body)
	}
	post := httptest.NewRequest("POST", "/rest/ping.view", strings.NewReader("u=listener&p=secret&v=1.16.1&c=tests&f=json"))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	server.Handler.ServeHTTP(w, post)
	if body := decode(w); body["status"] != "ok" {
		t.Fatal(body)
	}
	req = httptest.NewRequest("GET", "/api/tracks?u=listener&p=secret&v=1.16.1&c=tests&f=json", nil)
	w = httptest.NewRecorder()
	server.Handler.ServeHTTP(w, req)
	if body := decode(w); body["status"] != "failed" {
		t.Fatal("native route leaked onto legacy listener")
	}
	if _, err = io.Copy(io.Discard, xmlResult.Result().Body); err != nil {
		t.Fatal(err)
	}
}
