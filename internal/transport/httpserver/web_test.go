package httpserver_test

import (
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
