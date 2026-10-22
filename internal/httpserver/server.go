// Package httpserver provides the server's operational HTTP endpoints.
package httpserver

import (
	"context"
	"encoding/json"
	"github.com/prayujt/wavelength/internal/auth"
	"net/http"
	"time"
)

type Option func(*http.ServeMux)

// New constructs a server without opening a listener.
func New(addr string, ping func(context.Context) error, options ...Option) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { respond(w, http.StatusOK, "ok") })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := ping(ctx); err != nil {
			respond(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
		respond(w, http.StatusOK, "ok")
	})
	for _, option := range options {
		option(mux)
	}
	return &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
}

func respond(w http.ResponseWriter, status int, value string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": value})
}

// WithAuth registers provider-neutral browser login routes only on the native server.
func WithAuth(manager *auth.Manager) Option {
	return func(mux *http.ServeMux) { mux.Handle("/auth/", manager.Handler()) }
}
