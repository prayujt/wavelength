// Package app assembles services and owns their process lifetime.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/config"
	"github.com/prayujt/wavelength/internal/database"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/transport/httpserver"
)

// Run keeps resources alive until shutdown; scanOnly exits after catalog reconciliation.
func Run(ctx context.Context, c config.Config, scanOnly bool) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	db, err := database.Open(ctx, c.DataDir)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	var lib *library.Library
	if dir := c.MusicDir; dir != "" {
		lib, err = library.Open(dir, database.Catalog{DB: db})
		if err != nil {
			return fmt.Errorf("open library: %w", err)
		}
		defer func() {
			cancel()
			lib.Close()
		}()
		result, err := lib.Scan(ctx)
		if err != nil {
			return fmt.Errorf("scan library: %w", err)
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
		return fmt.Errorf("initialize authentication: %w", err)
	}
	options := []httpserver.Option{httpserver.WithAuth(manager)}
	if lib != nil {
		options = append(options, httpserver.WithNative(lib, manager))
	}
	servers := []server{{name: "native", http: httpserver.New(c.Addr, db.PingContext, options...)}}
	if c.Subsonic.Enabled() {
		legacy, cleanup, err := legacyServer(ctx, c, lib, database.StateStore{DB: db})
		if err != nil {
			return fmt.Errorf("initialize legacy API: %w", err)
		}
		defer cleanup()
		servers = append(servers, server{name: "legacy-subsonic", http: legacy})
	}
	return serve(ctx, servers)
}
