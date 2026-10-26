package httpserver

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
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
			if r.URL.Path == "/app" {
				http.Redirect(w, r, "/app/", http.StatusPermanentRedirect)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/app/") {
				w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' blob: data: https://*.mzstatic.com; font-src 'self' data:; media-src 'self' blob:; connect-src 'self'; worker-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
				w.Header().Set("Referrer-Policy", "no-referrer")
				if r.URL.Path == "/app/sw.js" {
					w.Header().Set("Service-Worker-Allowed", "/app/")
					w.Header().Set("Content-Type", "application/javascript")
				}
				if r.URL.Path == "/app/manifest.webmanifest" {
					w.Header().Set("Content-Type", "application/manifest+json")
				}
				if strings.HasPrefix(r.URL.Path, "/app/_expo/static/") || strings.HasPrefix(r.URL.Path, "/app/assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					if ext := path.Ext(r.URL.Path); ext == ".js" || ext == ".css" {
						if _, err := fs.Stat(assets, strings.TrimPrefix(r.URL.Path, "/")+".gz"); err == nil {
							w.Header().Add("Vary", "Accept-Encoding")
							if acceptsGzip(r.Header.Get("Accept-Encoding")) {
								w.Header().Set("Content-Type", mime.TypeByExtension(ext))
								w.Header().Set("Content-Encoding", "gzip")
								r = r.Clone(r.Context())
								r.URL.Path += ".gz"
							}
						}
					}
				}
				if strings.HasPrefix(r.URL.Path, "/app/oauth/") {
					w.Header().Set("Cache-Control", "private, no-store")
				}
				if _, err := fs.Stat(assets, strings.TrimPrefix(r.URL.Path, "/")); err != nil && path.Ext(r.URL.Path) == "" && !strings.HasPrefix(r.URL.Path, "/app/_expo/") && !strings.HasPrefix(r.URL.Path, "/app/assets/") {
					r = r.Clone(r.Context())
					r.URL.Path = "/app/"
				}
			}
			if strings.HasPrefix(r.URL.Path, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			if r.URL.Path == "/mobile-auth.html" {
				w.Header().Set("Cache-Control", "private, no-store")
				w.Header().Set("Referrer-Policy", "no-referrer")
				w.Header().Set("X-Robots-Tag", "noindex, nofollow")
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

func acceptsGzip(header string) bool {
	wildcard := false
	for _, offer := range strings.Split(header, ",") {
		parts := strings.Split(offer, ";")
		coding, quality := strings.ToLower(strings.TrimSpace(parts[0])), 1.0
		for _, parameter := range parts[1:] {
			key, value, _ := strings.Cut(parameter, "=")
			if strings.EqualFold(strings.TrimSpace(key), "q") {
				q, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
				if err != nil || q < 0 || q > 1 {
					quality = 0
				} else {
					quality = q
				}
			}
		}
		if coding == "gzip" {
			return quality > 0
		}
		if coding == "*" {
			wildcard = quality > 0
		}
	}
	return wildcard
}
