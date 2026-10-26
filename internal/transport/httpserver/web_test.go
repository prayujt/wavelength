package httpserver_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/transport/httpserver"
)

func TestPWACompressedBundleDelivery(t *testing.T) {
	script := []byte(strings.Repeat("console.log('music');", 200))
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	_, _ = gz.Write(script)
	_ = gz.Close()
	assets := fstest.MapFS{
		"app/_expo/static/js/web/entry-abc.js":    {Data: script},
		"app/_expo/static/js/web/entry-abc.js.gz": {Data: compressed.Bytes()},
		"assets/website.js":                       {Data: script},
	}
	handler := httpserver.New("", func(context.Context) error { return nil }, httpserver.WithWeb(assets)).Handler
	for _, tc := range []struct {
		accept     string
		compressed bool
	}{
		{"gzip, deflate, br", true}, {"GZIP; q=0.5", true}, {"*", true},
		{"", false}, {"br", false}, {"gzip;q=0, *;q=1", false}, {"gzip;q=invalid", false},
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			request := httptest.NewRequest(method, "/app/_expo/static/js/web/entry-abc.js", nil)
			request.Header.Set("Accept-Encoding", tc.accept)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != 200 || (response.Header().Get("Content-Encoding") == "gzip") != tc.compressed || response.Header().Get("Vary") != "Accept-Encoding" || !strings.Contains(response.Header().Get("Content-Type"), "javascript") {
				t.Fatalf("%s %q: %d %v", method, tc.accept, response.Code, response.Header())
			}
			if method == http.MethodHead {
				if response.Body.Len() != 0 {
					t.Fatal("HEAD returned a body")
				}
				continue
			}
			body := response.Body.Bytes()
			if tc.compressed {
				reader, err := gzip.NewReader(bytes.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				body, err = io.ReadAll(reader)
				if err != nil {
					t.Fatal(err)
				}
				_ = reader.Close()
			}
			if !bytes.Equal(body, script) {
				t.Fatal("bundle changed")
			}
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/assets/website.js", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Header().Get("Content-Encoding") != "" || !bytes.Equal(response.Body.Bytes(), script) {
		t.Fatal("website delivery changed")
	}
}

func TestWebAssetsDoNotReplaceAPIRoutes(t *testing.T) {
	assets := fstest.MapFS{"index.html": {Data: []byte("<h1>Wavelength</h1>")}, "assets/app-123.js": {Data: []byte("console.log('music')")}, "mobile-auth.html": {Data: []byte("<h1>Return to Wavelength</h1>")}}
	manager, err := auth.New(context.Background(), auth.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := httpserver.New("", func(context.Context) error { return nil }, httpserver.WithWeb(assets), httpserver.WithAuth(manager)).Handler
	for _, tc := range []struct {
		path, cache string
		status      int
	}{
		{"/", "no-cache", 200}, {"/assets/app-123.js", "public, max-age=31536000, immutable", 200}, {"/missing", "", 404}, {"/readyz", "", 200},
		{"/mobile-auth.html?code=one-time&state=test", "private, no-store", 200},
	} {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest("GET", tc.path, nil))
		if r.Code != tc.status || r.Header().Get("Cache-Control") != tc.cache {
			t.Fatalf("%s: %d %v", tc.path, r.Code, r.Header())
		}
		if tc.path == "/" && !strings.Contains(r.Body.String(), "Wavelength") {
			t.Fatal("missing client")
		}
	}
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodHead, "/", nil))
	if body, _ := io.ReadAll(r.Result().Body); len(body) != 0 || r.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("HEAD or client security headers missing")
	}
}

func TestPWASubpathFallbackAndHeaders(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":                           {Data: []byte("website")},
		"app/index.html":                       {Data: []byte("expo application")},
		"app/sw.js":                            {Data: []byte("worker")},
		"app/manifest.webmanifest":             {Data: []byte("{}")},
		"app/_expo/static/js/web/entry-abc.js": {Data: []byte("script")},
	}
	manager, err := auth.New(context.Background(), auth.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := httpserver.New("", func(context.Context) error { return nil }, httpserver.WithWeb(assets), httpserver.WithAuth(manager)).Handler
	for _, tc := range []struct {
		path   string
		status int
		body   string
	}{
		{"/", 200, "website"}, {"/app", 308, ""}, {"/app/", 200, "expo application"},
		{"/app/index.html", 301, ""},
		{"/app/library", 200, "expo application"}, {"/app/album/123", 200, "expo application"},
		{"/app/oauth/callback?code=secret", 200, "expo application"},
		{"/app/missing.js", 404, ""}, {"/app/_expo/missing", 404, ""},
		{"/oauth/callback", 404, ""}, {"/app/sw.js", 200, "worker"},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", tc.path, nil))
		if response.Code != tc.status || (tc.body != "" && !strings.Contains(response.Body.String(), tc.body)) {
			t.Fatal(tc.path, response.Code, response.Body.String())
		}
		if strings.Contains(tc.path, "/app/oauth/") && response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("callback caching", response.Header())
		}
		if tc.path == "/app/sw.js" && (response.Header().Get("Cache-Control") != "no-cache" || response.Header().Get("Service-Worker-Allowed") != "/app/") {
			t.Fatal("worker headers", response.Header())
		}
	}
}
