package httpserver

import (
	"net/http"

	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/sharing"
	transportsharing "github.com/prayujt/wavelength/internal/transport/sharing"
)

func WithSharing(lib *library.Library, links *sharing.Service) Option {
	return func(mux *http.ServeMux) { transportsharing.Register(mux, lib, links) }
}
