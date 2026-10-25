package httpserver

import (
	"io/fs"
	"net/http"
	"strings"
)

// WithWeb serves the built client alongside the API so browser sessions stay same-origin.
func WithWeb(assets fs.FS) Option {
	return func(mux *http.ServeMux) {
		files := http.FileServerFS(assets)
		mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' https://*.mzstatic.com; media-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
			w.Header().Set("Cache-Control", "no-cache")
			if strings.HasPrefix(r.URL.Path, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			if strings.HasPrefix(r.URL.Path, "/s/") && strings.Count(r.URL.Path, "/") == 2 {
				w.Header().Set("Cache-Control", "private, no-store")
				w.Header().Set("Referrer-Policy", "no-referrer")
				w.Header().Set("X-Robots-Tag", "noindex, nofollow")
				r = r.Clone(r.Context())
				r.URL.Path = "/"
			}
			files.ServeHTTP(w, r)
		}))
	}
}
