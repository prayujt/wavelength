// Package sharing serves capability-scoped public metadata and audio.
package sharing

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/prayujt/wavelength/internal/library"
	access "github.com/prayujt/wavelength/internal/sharing"
	"github.com/prayujt/wavelength/internal/transport/media"
)

func Register(mux *http.ServeMux, lib *library.Library, links *access.Service) {
	handle := func(pattern string, run func(http.ResponseWriter, *http.Request) error) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "private, no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
			if err := run(w, r); err != nil {
				// Capability URLs are credentials; never include their paths in application logs.
				code := http.StatusInternalServerError
				if errors.Is(err, access.ErrNotFound) || errors.Is(err, library.ErrNotFound) {
					code = http.StatusNotFound
				}
				http.Error(w, http.StatusText(code), code)
			}
		})
	}
	handle("GET /s/{token}/info", func(w http.ResponseWriter, r *http.Request) error {
		g, err := links.ResolveShare(r.Context(), r.PathValue("token"))
		if err != nil {
			return err
		}
		tracks := []map[string]any{}
		for _, id := range g.TrackIDs {
			t, err := lib.Get(r.Context(), id)
			if err != nil {
				return err
			}
			tracks = append(tracks, map[string]any{"id": t.ID, "title": t.Title, "artist": t.Artist, "album": t.Album, "duration_seconds": t.Duration, "artwork_url": "/s/" + r.PathValue("token") + "/artwork/" + t.ID})
		}
		w.Header().Set("Content-Type", "application/json")
		return json.NewEncoder(w).Encode(map[string]any{"name": g.Name, "tracks": tracks})
	})
	handle("GET /s/{token}/artwork/{id}", func(w http.ResponseWriter, r *http.Request) error {
		t, err := links.ShareTrack(r.Context(), r.PathValue("token"), r.PathValue("id"))
		if err != nil {
			return err
		}
		return media.Artwork(w, r, lib, t.ID)
	})
	handle("POST /s/{token}/play/{id}", func(w http.ResponseWriter, r *http.Request) error {
		// No browser session is consumed here. The share token alone authorizes this operation.
		link, err := links.PlaybackFromShare(r.Context(), r.PathValue("token"), r.PathValue("id"))
		if err != nil {
			return err
		}
		w.Header().Set("Content-Type", "application/json")
		return json.NewEncoder(w).Encode(map[string]any{"url": link.URL, "expires_at": link.Grant.ExpiresAt})
	})
	handle("GET /play/{token}/{file}", func(w http.ResponseWriter, r *http.Request) error {
		if !strings.HasPrefix(r.PathValue("file"), "audio") {
			return access.ErrNotFound
		}
		id, err := links.AuthorizePlayback(r.Context(), r.PathValue("token"))
		if err != nil {
			return err
		}
		// Keep conditional and byte-range handling identical to authenticated playback.
		return media.Stream(w, r, lib, id)
	})
}
