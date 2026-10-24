// Package media serves original media files independently of transport authentication.
package media

import (
	"net/http"

	"github.com/prayujt/wavelength/internal/library"
)

// Stream returns errors before writing headers, allowing each protocol to encode them.
func Stream(w http.ResponseWriter, r *http.Request, lib *library.Library, id string) error {
	file, track, err := lib.OpenTrack(r.Context(), id)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", track.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, track.Path, info.ModTime(), file)
	return nil
}
