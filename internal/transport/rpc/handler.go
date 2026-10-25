// Package rpc serves the native library contract over Connect and gRPC.
package rpc

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"
	"github.com/prayujt/wavelength/gen/wavelength/v1/wavelengthv1connect"
	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/discovery"
	"github.com/prayujt/wavelength/internal/ingest"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/listening"
	"github.com/prayujt/wavelength/internal/sharing"
	"github.com/prayujt/wavelength/internal/state"
	"github.com/prayujt/wavelength/internal/userdata"
)

type service struct{ library *library.Library }

var _ wavelengthv1connect.LibraryServiceHandler = (*service)(nil)

func Handler(lib *library.Library, manager *auth.Manager) (string, http.Handler) {
	path, handler := wavelengthv1connect.NewLibraryServiceHandler(&service{library: lib})
	return path, protect(handler, manager)
}
func protect(handler http.Handler, manager *auth.Manager) http.Handler {
	writer := connect.NewErrorWriter()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		ctx, err := manager.Authorize(r)
		if err != nil {
			code := connect.CodeInternal
			var failure *auth.RequestError
			if errors.As(err, &failure) {
				switch failure.StatusCode {
				case 401:
					code = connect.CodeUnauthenticated
				case 403:
					code = connect.CodePermissionDenied
				}
			}
			_ = writer.Write(w, r, connect.NewError(code, err))
			return
		}
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}
func failure(err error) error {
	var authError *auth.RequestError
	if errors.As(err, &authError) {
		code := connect.CodePermissionDenied
		if authError.StatusCode == 401 {
			code = connect.CodeUnauthenticated
		}
		return connect.NewError(code, err)
	}
	if errors.Is(err, listening.ErrConflict) || errors.Is(err, ingest.ErrConflict) {
		return connect.NewError(connect.CodeAlreadyExists, err)
	}
	if errors.Is(err, ingest.ErrCapacity) {
		return connect.NewError(connect.CodeResourceExhausted, err)
	}
	if errors.Is(err, sharing.ErrUnavailable) {
		return connect.NewError(connect.CodeUnavailable, sharing.ErrUnavailable)
	}
	if errors.Is(err, discovery.ErrUnavailable) {
		return connect.NewError(connect.CodeUnavailable, errors.New("music provider is unavailable"))
	}
	if errors.Is(err, listening.ErrInvalid) || errors.Is(err, sharing.ErrInvalid) || errors.Is(err, userdata.ErrInvalid) || errors.Is(err, ingest.ErrInvalid) || errors.Is(err, discovery.ErrInvalid) {
		return invalid("invalid request parameters")
	}
	if errors.Is(err, userdata.ErrForbidden) {
		return connect.NewError(connect.CodePermissionDenied, errors.New("operation is not permitted"))
	}
	if errors.Is(err, listening.ErrNotFound) || errors.Is(err, sharing.ErrNotFound) || errors.Is(err, library.ErrNotFound) || errors.Is(err, state.ErrNotFound) || errors.Is(err, ingest.ErrNotFound) || errors.Is(err, discovery.ErrNotFound) {
		return connect.NewError(connect.CodeNotFound, errors.New("requested item not found"))
	}
	if errors.Is(err, context.Canceled) {
		return connect.NewError(connect.CodeCanceled, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return connect.NewError(connect.CodeDeadlineExceeded, err)
	}
	return connect.NewError(connect.CodeInternal, errors.New("operation failed"))
}
func invalid(message string) error {
	return connect.NewError(connect.CodeInvalidArgument, errors.New(message))
}
func pagination(limit, offset int32) (int, int, error) {
	if limit < 0 || limit > 500 || offset < 0 {
		return 0, 0, invalid("limit must be 0..500 and offset must be nonnegative")
	}
	if limit == 0 {
		limit = 100
	}
	return int(limit), int(offset), nil
}
func slice[T any](items []T, offset, count int) []T {
	if offset >= len(items) {
		return []T{}
	}
	end := offset + count
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}
