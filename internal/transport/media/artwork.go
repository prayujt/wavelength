package media

import (
	"bytes"
	"net/http"
	"time"

	"github.com/prayujt/wavelength/internal/library"
)

// Artwork reads existing embedded or adjacent art only when an image is requested.
func Artwork(w http.ResponseWriter, r *http.Request, lib *library.Library, id string) error {
	body, _, err := lib.Artwork(r.Context(), id)
	if err != nil {
		return err
	}
	if len(body) == 0 || len(body) > 20<<20 {
		return library.ErrNotFound
	}
	kind := http.DetectContentType(body)
	switch kind {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
	default:
		return library.ErrNotFound
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "artwork", time.Time{}, bytes.NewReader(body))
	return nil
}
