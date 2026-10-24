package subsonic

import (
	"errors"
	"log/slog"

	"github.com/prayujt/wavelength/internal/accounts"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/podcasts"
	"github.com/prayujt/wavelength/internal/sharing"
	"github.com/prayujt/wavelength/internal/state"
	"github.com/prayujt/wavelength/internal/transcode"
	"github.com/prayujt/wavelength/internal/userdata"
)

func legacyError(result *response, err error) {
	result.Status = "failed"
	result.Error = &apiError{Code: 0, Message: "Library operation failed"}
	if errors.Is(err, transcode.ErrInvalid) || errors.Is(err, sharing.ErrInvalid) || errors.Is(err, userdata.ErrInvalid) || errors.Is(err, accounts.ErrInvalid) || errors.Is(err, podcasts.ErrInvalid) {
		failure(result, 10, err.Error())
		return
	}
	if errors.Is(err, sharing.ErrForbidden) || errors.Is(err, userdata.ErrForbidden) || errors.Is(err, accounts.ErrForbidden) {
		failure(result, 50, err.Error())
		return
	}
	if errors.Is(err, accounts.ErrExists) {
		failure(result, 0, err.Error())
		return
	}
	if errors.Is(err, state.ErrNotFound) {
		failure(result, 70, "Resource not found")
		return
	}
	if errors.Is(err, library.ErrNotFound) {
		result.Error = &apiError{Code: 70, Message: "Track not found"}
		return
	}
	slog.Error("legacy library operation failed", "error", err)
}
