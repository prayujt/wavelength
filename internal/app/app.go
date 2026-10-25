// Package app assembles services and owns their process lifetime.
package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/config"
	"github.com/prayujt/wavelength/internal/database"
	"github.com/prayujt/wavelength/internal/discovery"
	"github.com/prayujt/wavelength/internal/ingest"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/providers/itunes"
	"github.com/prayujt/wavelength/internal/providers/jev"
	"github.com/prayujt/wavelength/internal/providers/youtube"
	"github.com/prayujt/wavelength/internal/sharing"
	"github.com/prayujt/wavelength/internal/transport/httpserver"
	transportmcp "github.com/prayujt/wavelength/internal/transport/mcp"
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
	if c.WebDir != "" {
		if _, err := os.Stat(filepath.Join(c.WebDir, "index.html")); err != nil {
			return fmt.Errorf("open web app: %w", err)
		}
		options = append(options, httpserver.WithWeb(os.DirFS(c.WebDir)))
	}
	if lib != nil {
		data := userdata.New(database.StateStore{DB: db}, lib)
		links := sharing.New(database.GrantStore{DB: db}, lib, c.PublicURL)
		options = append(options, httpserver.WithSharing(lib, links))
		source := youtube.New()
		var ranking []discovery.Option
		if c.Jev.APIKey != "" {
			ranking = append(ranking, discovery.WithRanking(jev.New(c.Jev.APIKey, c.Jev.Model), jev.Policy()))
		}
		discovery := discovery.New(itunes.New(), source, ranking...)
		rootID, rootPath := lib.SourceRoot()
		imports, err := ingest.New(ctx, ingest.Root{ID: rootID, Path: rootPath}, filepath.Join(c.DataDir, "ingest"), ingest.Dependencies{Store: database.IngestStore{DB: db}, Discovery: discovery, Downloader: source, Media: ingest.NewAudioMedia(), Playlists: data})
		if err != nil {
			return fmt.Errorf("initialize ingestion: %w", err)
		}
		defer imports.Close()
		options = append(options, httpserver.WithNative(lib, manager), httpserver.WithPersonal(lib, data, manager, transportmcp.Features{Discovery: discovery, Ingest: imports, Links: links}))
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
