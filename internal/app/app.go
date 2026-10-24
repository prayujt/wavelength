// Package app assembles services and owns their process lifetime.
package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/config"
	"github.com/prayujt/wavelength/internal/database"
	"github.com/prayujt/wavelength/internal/httpserver"
	"github.com/prayujt/wavelength/internal/library"
)

// Run keeps resources alive until shutdown; scanOnly exits after catalog reconciliation.
func Run(ctx context.Context, c config.Config, scanOnly bool) error {
	db, err := database.Open(ctx, c.DataDir)
	if err != nil {
		return err
	}
	defer db.Close()
	var lib *library.Library
	if dir := c.MusicDir; dir != "" {
		lib, err = library.Open(dir, database.Catalog{DB: db})
		if err != nil {
			return err
		}
		defer lib.Close()
		result, err := lib.Scan(ctx)
		if err != nil {
			return err
		}
		slog.Info("library indexed", "tracks", result.Indexed, "metadata_warnings", result.MetadataWarnings)
	}
	if scanOnly {
		if lib == nil {
			return errors.New("WAVELENGTH_MUSIC_DIR is required for --scan-only")
		}
		return nil
	}
	manager, err := auth.New(ctx, auth.Config{
		Issuer: c.OIDC.Issuer, ClientID: c.OIDC.ClientID, ClientSecret: c.OIDC.ClientSecret,
		PublicURL: c.PublicURL, Token: c.Token,
	}, database.AuthStore{DB: db})
	if err != nil {
		return err
	}
	options := []httpserver.Option{httpserver.WithAuth(manager)}
	if lib != nil {
		options = append(options, httpserver.WithNative(lib, manager))
	}
	servers := []server{{name: "native", http: httpserver.New(c.Addr, db.PingContext, options...)}}
	if c.Subsonic.Enabled() {
		legacy, err := legacyServer(ctx, c, lib, database.StateStore{DB: db})
		if err != nil {
			return err
		}
		servers = append(servers, server{name: "legacy-subsonic", http: legacy})
	}
	return serve(ctx, servers)
}
