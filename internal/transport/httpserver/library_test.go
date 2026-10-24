package httpserver_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/database"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/testutil"
	"github.com/prayujt/wavelength/internal/transport/httpserver"
)

func TestPersistedCatalogStreaming(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	root := t.TempDir()
	content := wave()
	path := filepath.Join(root, "tone.wav")
	if err = os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	lib, err := library.Open(root, database.Catalog{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	testutil.SeedCatalog(t, db, root)
	manager, err := auth.New(ctx, auth.Config{Token: "test-token"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpserver.New("", db.PingContext, httpserver.WithNative(lib, manager)).Handler)
	defer server.Close()
	request := func(method, path, token, byteRange string, payload ...string) *http.Response {
		t.Helper()
		var body io.Reader
		if strings.HasPrefix(path, "/wavelength.") {
			value := "{}"
			if len(payload) > 0 {
				value = payload[0]
			}
			body = strings.NewReader(value)
		}
		req, err := http.NewRequest(method, server.URL+path, body)
		if err != nil {
			t.Fatal(err)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if byteRange != "" {
			req.Header.Set("Range", byteRange)
		}
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { response.Body.Close() })
		return response
	}
	for _, token := range []string{"", "wrong"} {
		response := request("POST", "/wavelength.v1.LibraryService/ListTracks", token, "")
		if response.StatusCode != 401 {
			t.Fatalf("auth bypass: %d", response.StatusCode)
		}
	}
	response := request("POST", "/wavelength.v1.LibraryService/ListTracks", "test-token", "", `{"limit":1,"query":"tone"}`)
	var page library.Page
	if err = json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Tracks) != 1 {
		t.Fatalf("catalog: %+v", page)
	}
	stream := "/media/tracks/" + page.Tracks[0].ID
	response = request("GET", stream, "test-token", "")
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || !bytes.Equal(body, content) || response.Header.Get("Content-Type") != "audio/wav" {
		t.Fatal("original audio did not round trip")
	}
	response = request("GET", stream, "test-token", "bytes=4-11")
	body, err = io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 206 || !bytes.Equal(body, content[4:12]) || response.Header.Get("Content-Range") != "bytes 4-11/76" {
		t.Fatalf("range: %d %v", response.StatusCode, response.Header)
	}
	response = request("HEAD", stream, "test-token", "")
	body, err = io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || len(body) != 0 || response.ContentLength != int64(len(content)) {
		t.Fatal("HEAD failed")
	}
	if r := request("GET", stream, "test-token", "bytes=99999-"); r.StatusCode != 416 {
		t.Fatalf("unsatisfiable range: %d", r.StatusCode)
	}
	if r := request("GET", "/media/tracks/missing", "test-token", ""); r.StatusCode != 404 {
		t.Fatalf("unknown track: %d", r.StatusCode)
	}
	if r := request("POST", "/wavelength.v1.LibraryService/ListTracks", "test-token", "", `{"limit":-1}`); r.StatusCode != 400 {
		t.Fatalf("bad limit: %d", r.StatusCode)
	}
	if r := request("POST", "/wavelength.v1.LibraryService/ListTracks", "test-token", "", `{"offset":"nope"}`); r.StatusCode != 400 {
		t.Fatalf("bad offset: %d", r.StatusCode)
	}
	// A file replaced by a symlink must not let an indexed ID stream outside the root.
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(t.TempDir(), "private.wav")
	if err = os.WriteFile(secret, []byte("private outside root"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(secret, path); err != nil {
		t.Fatal(err)
	}
	response = request("GET", stream, "test-token", "")
	body, err = io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode < 400 || bytes.Contains(body, []byte("private outside root")) {
		t.Fatal("symlink escape")
	}
	for _, method := range []string{"StartScan", "GetScanStatus"} {
		if r := request("POST", "/wavelength.v1.LibraryService/"+method, "test-token", ""); r.StatusCode != 404 {
			t.Fatalf("removed RPC %s returned %d", method, r.StatusCode)
		}
	}
	if page, err := lib.List(ctx, library.Query{}); err != nil || page.Total != 1 {
		t.Fatal("source changes modified the persisted catalog", page, err)
	}
}

func wave() []byte {
	// Valid 16-sample mono PCM WAV, generated in memory for an exact byte comparison.
	var out bytes.Buffer
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(68))
	out.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(8000), uint32(16000), uint16(2), uint16(16)} {
		_ = binary.Write(&out, binary.LittleEndian, v)
	}
	out.WriteString("data")
	_ = binary.Write(&out, binary.LittleEndian, uint32(32))
	out.Write(make([]byte, 32))
	return out.Bytes()
}
