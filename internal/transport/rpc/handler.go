// Package rpc serves the native library contract over Connect and gRPC.
package rpc

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"
	"github.com/prayujt/wavelength/gen/wavelength/v1/wavelengthv1connect"
	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/library"
)

type service struct{ library *library.Library }

var _ wavelengthv1connect.LibraryServiceHandler = (*service)(nil)

func Handler(lib *library.Library, manager *auth.Manager) (string, http.Handler) {
	path, handler := wavelengthv1connect.NewLibraryServiceHandler(&service{library: lib})
	writer := connect.NewErrorWriter()
	return path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	if errors.Is(err, library.ErrNotFound) {
		return connect.NewError(connect.CodeNotFound, errors.New("media not found"))
	}
	if errors.Is(err, context.Canceled) {
		return connect.NewError(connect.CodeCanceled, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return connect.NewError(connect.CodeDeadlineExceeded, err)
	}
	return connect.NewError(connect.CodeInternal, errors.New("library operation failed"))
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
