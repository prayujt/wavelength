package httpserver

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/playback"
)

// WithLibrary adds the native catalog API. A nonempty token protects all /api routes.
func WithLibrary(lib *library.Library, token string) Option {
	return func(mux *http.ServeMux) {
		api := http.NewServeMux()
		api.HandleFunc("GET /api/tracks", func(w http.ResponseWriter, r *http.Request) {
			limit, err := integer(r, "limit", 100, 1, 500)
			if err != nil {
				respond(w, 400, err.Error())
				return
			}
			offset, err := integer(r, "offset", 0, 0, int(^uint(0)>>1))
			if err != nil {
				respond(w, 400, err.Error())
				return
			}
			page, err := lib.List(r.Context(), library.Query{Search: r.URL.Query().Get("q"), Limit: limit, Offset: offset})
			if err != nil {
				apiError(w, r, err)
				return
			}
			writeJSON(w, 200, page)
		})
		api.HandleFunc("GET /api/tracks/{id}", func(w http.ResponseWriter, r *http.Request) {
			track, err := lib.Get(r.Context(), r.PathValue("id"))
			if err != nil {
				apiError(w, r, err)
				return
			}
			writeJSON(w, 200, track)
		})
		api.HandleFunc("GET /api/tracks/{id}/stream", func(w http.ResponseWriter, r *http.Request) {
			if err := playback.Stream(w, r, lib, r.PathValue("id")); err != nil {
				apiError(w, r, err)
			}
		})
		api.HandleFunc("POST /api/scan", func(w http.ResponseWriter, r *http.Request) {
			result, err := lib.Scan(r.Context())
			if err != nil {
				apiError(w, r, err)
				return
			}
			writeJSON(w, 200, result)
		})
		mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token != "" {
				expected := sha256.Sum256([]byte("Bearer " + token))
				actual := sha256.Sum256([]byte(r.Header.Get("Authorization")))
				if subtle.ConstantTimeCompare(expected[:], actual[:]) != 1 {
					w.Header().Set("WWW-Authenticate", `Bearer realm="wavelength"`)
					respond(w, 401, "unauthorized")
					return
				}
			}
			w.Header().Set("Cache-Control", "no-store")
			api.ServeHTTP(w, r)
		}))
	}
}

func integer(r *http.Request, key string, fallback, min, max int) (int, error) {
	raw, ok := r.URL.Query()[key]
	if !ok {
		return fallback, nil
	}
	if len(raw) != 1 {
		return 0, errors.New("invalid " + key)
	}
	n, err := strconv.Atoi(raw[0])
	if err != nil || n < min || n > max {
		return 0, errors.New("invalid " + key)
	}
	return n, nil
}
func apiError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, library.ErrNotFound) {
		respond(w, 404, "track not found")
		return
	}
	if errors.Is(err, library.ErrScanRunning) {
		respond(w, 409, "scan already running")
		return
	}
	slog.Error("library request failed", "method", r.Method, "path", strings.Split(r.URL.Path, "?")[0], "error", err)
	respond(w, 500, "library operation failed")
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
