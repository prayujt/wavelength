package subsonic

import (
	"github.com/prayujt/wavelength/internal/accounts"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/podcasts"
	"github.com/prayujt/wavelength/internal/sharing"
	"github.com/prayujt/wavelength/internal/transcode"
	"github.com/prayujt/wavelength/internal/userdata"
)

type Config struct{ Addr, PublicURL, AvatarDir string }

// Dependencies are assembled by the application. The adapter owns no service lifetime.
type Dependencies struct {
	Library    *library.Library
	Accounts   *accounts.Service
	UserData   *userdata.Service
	Podcasts   *podcasts.Service
	Shares     *sharing.Service
	Transcoder *transcode.Service
}
