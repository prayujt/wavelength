package httpserver

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
)

func TestHealthIndependentOfDatabaseReadiness(t *testing.T) {
	server := New("", func(context.Context) error { return errors.New("private database error") })
	for path, status := range map[string]int{"/healthz": 200, "/readyz": 503} {
		response := httptest.NewRecorder()
		server.Handler.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != status {
			t.Fatalf("%s: %d", path, response.Code)
		}
		if response.Header().Get("Content-Type") != "application/json" {
			t.Fatal("missing JSON content type")
		}
		if path == "/readyz" && response.Body.String() != "{\"status\":\"unavailable\"}\n" {
			t.Fatal("readiness leaks internal error")
		}
	}
}
