package app

import (
	"context"
	"net/http"

	"github.com/prayujt/wavelength/internal/config"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/state"
	"github.com/prayujt/wavelength/internal/transcode"
	"github.com/prayujt/wavelength/internal/transport/subsonic"
	"github.com/prayujt/wavelength/internal/userdata"
)

func legacyServer(ctx context.Context, c config.Config, lib *library.Library, store state.Store) (*http.Server, func(), error) {
	transcoder := transcode.New(ctx, lib)
	services := subsonic.Dependencies{Library: lib, UserData: userdata.New(store, lib), Transcoder: transcoder}
	server, err := subsonic.New(subsonic.Config{Addr: c.Subsonic.Addr, Username: c.Subsonic.User, Password: c.Subsonic.Password}, services)
	if err != nil {
		transcoder.Close()
		return nil, nil, err
	}
	return server, transcoder.Close, nil
}
