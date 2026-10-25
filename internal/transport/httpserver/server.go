// Package httpserver provides the server's operational HTTP endpoints.
package httpserver

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"time"

	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/telemetry"
	transportmcp "github.com/prayujt/wavelength/internal/transport/mcp"
	"github.com/prayujt/wavelength/internal/transport/media"
	"github.com/prayujt/wavelength/internal/transport/rpc"
	"github.com/prayujt/wavelength/internal/userdata"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type Option func(*http.ServeMux)

// New constructs a server without opening a listener.
func New(addr string, ping func(context.Context) error, options ...Option) *http.Server {
	return newServer(addr, ping, nil, options...)
}

func NewWithMetrics(addr string, ping func(context.Context) error, metrics *telemetry.Metrics, options ...Option) *http.Server {
	return newServer(addr, ping, metrics, options...)
}

func newServer(addr string, ping func(context.Context) error, metrics *telemetry.Metrics, options ...Option) *http.Server {
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
	requests, cancelRequests := context.WithCancel(context.Background())
	var handler http.Handler = mux
	if metrics != nil {
		handler = metrics.HTTP("native", handler)
	}
	server := &http.Server{
		Addr: addr, Handler: h2c.NewHandler(handler, &http2.Server{}),
		ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second,
		BaseContext: func(net.Listener) context.Context { return requests },
	}
	// Long-lived streams must finish before Shutdown waits for active requests.
	server.RegisterOnShutdown(cancelRequests)
	return server
}

func respond(w http.ResponseWriter, status int, value string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": value})
}

// WithAuth registers OIDC login and shared OAuth routes only on the native server.
func WithAuth(manager *auth.Manager) Option {
	return func(mux *http.ServeMux) {
		mux.Handle("/auth/", manager.Handler())
		oauth := manager.OAuthHandler()
		mux.Handle("/oauth/", oauth)
		mux.Handle("/.well-known/oauth-authorization-server", oauth)
		mux.Handle("/.well-known/oauth-protected-resource", oauth)
		mux.Handle("/.well-known/oauth-protected-resource/mcp", oauth)
	}
}

// WithNative registers protobuf RPCs and the byte-range media endpoint, not legacy REST controls.
func WithNative(lib *library.Library, manager *auth.Manager) Option {
	return func(mux *http.ServeMux) {
		path, handler := rpc.Handler(lib, manager)
		mux.Handle(path, handler)
		mux.Handle("GET /media/tracks/{id}/artwork", manager.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := media.Artwork(w, r, lib, r.PathValue("id")); err != nil {
				apiError(w, r, err)
			}
		})))
		mux.Handle("GET /media/tracks/{id}", manager.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := auth.RequireScope(r.Context(), auth.ScopeStream); err != nil {
				http.Error(w, "insufficient scope", http.StatusForbidden)
				return
			}
			if err := media.Stream(w, r, lib, r.PathValue("id")); err != nil {
				apiError(w, r, err)
			}
		})))
	}
}

// WithPersonal shares feature services and OIDC identities across native RPC and MCP.
func WithPersonal(lib *library.Library, data *userdata.Service, manager *auth.Manager, features ...transportmcp.Features) Option {
	return func(mux *http.ServeMux) {
		path, handler := rpc.UserDataHandler(data, manager)
		mux.Handle(path, handler)
		if len(features) > 0 {
			if features[0].Listening != nil {
				path, handler := rpc.ListeningHandler(features[0].Listening, manager)
				mux.Handle(path, handler)
			}
			if features[0].Links != nil {
				path, handler := rpc.SharingHandler(features[0].Links, manager)
				mux.Handle(path, handler)
			}
			if features[0].Discovery != nil && features[0].Ingest != nil {
				path, handler := rpc.DiscoveryHandler(features[0].Discovery, manager)
				mux.Handle(path, handler)
				path, handler = rpc.IngestionHandler(features[0].Ingest, manager)
				mux.Handle(path, handler)
				path, handler = rpc.RealtimeHandler(features[0].Ingest, manager)
				mux.Handle(path, handler)
			}
		}
		if manager.OAuthEnabled() {
			mux.Handle("/mcp", transportmcp.Handler(lib, data, manager, features...))
		}
	}
}
