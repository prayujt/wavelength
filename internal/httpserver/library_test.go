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
	"testing"

	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/database"
	"github.com/prayujt/wavelength/internal/httpserver"
	"github.com/prayujt/wavelength/internal/library"
)

func TestCatalogStreamingAndRescan(t *testing.T) {
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
	if _, err = lib.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	manager, err := auth.New(ctx, auth.Config{Token: "test-token"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpserver.New("", db.PingContext, httpserver.WithLibrary(lib, manager.Protect)).Handler)
	defer server.Close()
	request := func(method, path, token, byteRange string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
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
		response := request("GET", "/api/tracks", token, "")
		if response.StatusCode != 401 {
			t.Fatalf("auth bypass: %d", response.StatusCode)
		}
	}
	response := request("GET", "/api/tracks?limit=1&q=tone", "test-token", "")
	var page library.Page
	if err = json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Tracks) != 1 {
		t.Fatalf("catalog: %+v", page)
	}
	stream := "/api/tracks/" + page.Tracks[0].ID + "/stream"
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
	if r := request("GET", "/api/tracks/missing/stream", "test-token", ""); r.StatusCode != 404 {
		t.Fatalf("unknown track: %d", r.StatusCode)
	}
	if r := request("GET", "/api/tracks?limit=-1", "test-token", ""); r.StatusCode != 400 {
		t.Fatalf("bad limit: %d", r.StatusCode)
	}
	if r := request("GET", "/api/tracks?offset=nope", "test-token", ""); r.StatusCode != 400 {
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
	response = request("POST", "/api/scan", "test-token", "")
	if response.StatusCode != 200 {
		t.Fatalf("rescan: %d", response.StatusCode)
	}
	if r := request("GET", stream, "test-token", ""); r.StatusCode != 404 {
		t.Fatal("rescan did not remove symlinked file")
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
