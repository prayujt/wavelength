// Package app assembles services and owns their process lifetime.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/config"
	"github.com/prayujt/wavelength/internal/database"
	"github.com/prayujt/wavelength/internal/discovery"
	"github.com/prayujt/wavelength/internal/ingest"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/listening"
	"github.com/prayujt/wavelength/internal/playfeed"
	"github.com/prayujt/wavelength/internal/providers/itunes"
	"github.com/prayujt/wavelength/internal/providers/jev"
	"github.com/prayujt/wavelength/internal/providers/youtube"
	"github.com/prayujt/wavelength/internal/realtime"
	"github.com/prayujt/wavelength/internal/sharing"
	"github.com/prayujt/wavelength/internal/telemetry"
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
	concurrency := c.IngestConcurrency
	if concurrency == 0 {
		concurrency = 2
	}
	metrics := telemetry.New(concurrency)
	metrics.Database(db)
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
		source := youtube.NewWithConcurrency(concurrency + 1)
		source.SetMetrics(metrics)
		var ranking []discovery.Option
		var recommender playfeed.Ranker
		if c.Jev.APIKey != "" {
			client := jev.New(c.Jev.APIKey, c.Jev.Model)
			client.SetMetrics(metrics)
			recommender = client
			ranking = append(ranking, discovery.WithRanking(measuredRanker{client, metrics}, jev.Policy()))
		}
		feed := playfeed.New(ctx, lib, database.PlayFeedStore{DB: db}, database.ListeningStore{DB: db}, recommender, metrics)
		options = append(options, httpserver.WithPlayFeed(feed, manager))
		metadata := itunes.New()
		metadata.SetMetrics(metrics)
		discovery := discovery.New(measuredMetadata{metadata, metrics}, measuredSources{source, metrics}, ranking...)
		rootID, rootPath := lib.SourceRoot()
		store := database.IngestStore{DB: db}
		metrics.SetSnapshot(func(ctx context.Context) (telemetry.Snapshot, error) {
			stats, err := store.QueueStats(ctx, rootID)
			return telemetry.Snapshot{Jobs: stats.Jobs, Items: stats.Items, LibraryTracks: stats.LibraryTracks, LibraryBytes: stats.LibraryBytes, LibraryDuration: stats.LibraryDuration, OldestQueuedAt: stats.OldestQueuedAt}, err
		})
		audio := ingest.NewAudioMedia()
		audio.SetMetrics(metrics)
		imports, err := ingest.New(ctx, ingest.Root{ID: rootID, Path: rootPath}, filepath.Join(c.DataDir, "ingest"), ingest.Dependencies{Store: store, Discovery: discovery, Downloader: measuredDownloader{source, metrics}, Media: measuredMedia{audio, metrics}, Playlists: data, Concurrency: concurrency, Metrics: metrics})
		if err != nil {
			return fmt.Errorf("initialize ingestion: %w", err)
		}
		defer imports.Close()
		lib.ObserveDeletions(func(id string) { imports.Events().Publish(realtime.Event{Change: realtime.TrackDeleted{TrackID: id}}) })
		// Retry only recorded deletions. This is not a filesystem watcher or scan.
		cleanupCtx, stopCleanup := context.WithCancel(ctx)
		cleanupDone := make(chan struct{})
		go func() {
			defer close(cleanupDone)
			timer := time.NewTicker(30 * time.Second)
			defer timer.Stop()
			for {
				if err := lib.RecoverDeletions(cleanupCtx); err != nil && cleanupCtx.Err() == nil {
					slog.Warn("library deletion cleanup pending", "error", err)
				}
				select {
				case <-cleanupCtx.Done():
					return
				case <-timer.C:
				}
			}
		}()
		defer func() { stopCleanup(); <-cleanupDone }()

		options = append(options, httpserver.WithNative(lib, manager), httpserver.WithPersonal(lib, data, manager, transportmcp.Features{Discovery: discovery, Ingest: imports, Links: links, Listening: listening.New(database.ListeningStore{DB: db}, lib)}))
	}
	servers := []server{{name: "native", http: httpserver.NewWithMetrics(c.Addr, db.PingContext, metrics, options...)}}
	if c.MetricsAddr != "" {
		servers = append(servers, server{name: "metrics", http: httpserver.Metrics(c.MetricsAddr, metrics.Registry)})
	}
	if c.Subsonic.Enabled() {
		legacy, cleanup, err := legacyServer(ctx, c, lib, database.StateStore{DB: db})
		if err != nil {
			return fmt.Errorf("initialize legacy API: %w", err)
		}
		defer cleanup()
		servers = append(servers, server{name: "legacy-subsonic", http: legacy})
	}
	for _, server := range servers {
		if server.name != "native" {
			server.http.Handler = metrics.HTTP(server.name, server.http.Handler)
		}
	}
	return serve(ctx, servers)
}
