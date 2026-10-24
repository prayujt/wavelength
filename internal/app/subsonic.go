package app

import (
	"context"
	"net/http"
	"path/filepath"

	"github.com/prayujt/wavelength/internal/accounts"
	"github.com/prayujt/wavelength/internal/config"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/podcasts"
	"github.com/prayujt/wavelength/internal/sharing"
	"github.com/prayujt/wavelength/internal/state"
	"github.com/prayujt/wavelength/internal/transcode"
	"github.com/prayujt/wavelength/internal/transport/subsonic"
	"github.com/prayujt/wavelength/internal/userdata"
)

func legacyServer(ctx context.Context, c config.Config, lib *library.Library, store state.Store) (*http.Server, func(), error) {
	key, err := state.LoadKey(c.DataDir)
	if err != nil {
		return nil, nil, err
	}
	accountService, err := accounts.New(store, key, c.Subsonic.User, c.Subsonic.Password)
	if err != nil {
		return nil, nil, err
	}
	podcastService, err := podcasts.New(ctx, store, filepath.Join(c.DataDir, "podcasts"))
	if err != nil {
		return nil, nil, err
	}
	transcoder, err := transcode.New(ctx, lib, filepath.Join(c.DataDir, "converted"))
	if err != nil {
		podcastService.Close()
		return nil, nil, err
	}
	cleanup := func() {
		podcastService.Close()
		transcoder.Close()
	}
	dependencies := subsonic.Dependencies{
		Library:    lib,
		Accounts:   accountService,
		UserData:   userdata.New(store, lib),
		Podcasts:   podcastService,
		Shares:     sharing.New(store, lib),
		Transcoder: transcoder,
	}
	server, err := subsonic.New(ctx, subsonic.Config{Addr: c.Subsonic.Addr, PublicURL: c.Subsonic.PublicURL, AvatarDir: filepath.Join(c.DataDir, "avatars")}, dependencies)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return server, cleanup, nil
}
