package httpserver

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/prayujt/wavelength/internal/library"
)

func apiError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, library.ErrNotFound) {
		respond(w, 404, "track not found")
		return
	}
	slog.Error("library request failed", "method", r.Method, "path", strings.Split(r.URL.Path, "?")[0], "error", err)
	respond(w, 500, "library operation failed")
}
