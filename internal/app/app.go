// Package app assembles services and owns their process lifetime.
package app

import (
	"context"
	"fmt"

	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/config"
	"github.com/prayujt/wavelength/internal/database"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/transport/httpserver"
	"github.com/prayujt/wavelength/internal/userdata"
)

// Run serves the persisted catalog without inspecting or indexing source files.
func Run(ctx context.Context, c config.Config) error {
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
	}
	manager, err := auth.New(ctx, auth.Config{
		Issuer: c.OIDC.Issuer, ClientID: c.OIDC.ClientID, ClientSecret: c.OIDC.ClientSecret,
		PublicURL: c.PublicURL, Token: c.Token,
	}, database.AuthStore{DB: db}, auth.WithOAuthStore(database.StateStore{DB: db}))
	if err != nil {
		return fmt.Errorf("initialize authentication: %w", err)
	}
	options := []httpserver.Option{httpserver.WithAuth(manager)}
	if lib != nil {
		data := userdata.New(database.StateStore{DB: db}, lib)
		options = append(options, httpserver.WithNative(lib, manager), httpserver.WithPersonal(lib, data, manager))
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
