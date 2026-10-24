package subsonic

import (
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/transcode"
	"github.com/prayujt/wavelength/internal/userdata"
)

type Config struct{ Addr, Username, Password string }

// Dependencies are assembled by the application. The adapter owns no service lifetime.
type Dependencies struct {
	Library    *library.Library
	UserData   *userdata.Service
	Transcoder *transcode.Service
}
