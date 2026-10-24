package app

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"

	"github.com/prayujt/wavelength/internal/config"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/metadata"
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
	if c.LastFM.APIKey != "" {
		provider := &metadata.LastFM{APIKey: c.LastFM.APIKey, Secret: c.LastFM.Secret, Sessions: c.LastFM.Sessions}
		options = append(options, subsonic.WithMetadataProvider(provider), subsonic.WithScrobbler(provider))
	}
	if c.JukeboxPlayer != "" {
		player, err := exec.LookPath(c.JukeboxPlayer)
		if err != nil {
			return nil, fmt.Errorf("jukebox player: %w", err)
		}
		options = append(options, subsonic.WithJukeboxBackend(subsonic.FFplayBackend{Executable: player}))
	}
	return subsonic.New(c.Subsonic.Addr, lib, c.Subsonic.User, c.Subsonic.Password, options...)
}
