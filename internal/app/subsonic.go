package app

import (
	"context"
	"net/http"
	"path/filepath"

	"github.com/prayujt/wavelength/internal/config"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/state"
	"github.com/prayujt/wavelength/internal/subsonic"
)

func legacyServer(ctx context.Context, c config.Config, lib *library.Library, store state.Store) (*http.Server, error) {
	key, err := state.LoadKey(c.DataDir)
	if err != nil {
		return nil, err
	}
	options := []subsonic.Option{
		subsonic.WithState(store), subsonic.WithContext(ctx), subsonic.WithEncryptionKey(key),
		subsonic.WithPublicURL(c.Subsonic.PublicURL),
		subsonic.WithPodcastDirectory(filepath.Join(c.DataDir, "podcasts")),
		subsonic.WithVideoDirectory(filepath.Join(c.DataDir, "converted")),
		subsonic.WithAvatarDirectory(filepath.Join(c.DataDir, "avatars")),
	}
	return subsonic.New(c.Subsonic.Addr, lib, c.Subsonic.User, c.Subsonic.Password, options...)
}
