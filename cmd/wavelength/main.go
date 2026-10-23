package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prayujt/wavelength/internal/auth"
	"github.com/prayujt/wavelength/internal/database"
	"github.com/prayujt/wavelength/internal/httpserver"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/state"
	"github.com/prayujt/wavelength/internal/subsonic"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	scanOnly := flag.Bool("scan-only", false, "scan the configured music directory and exit")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := database.Open(ctx, env("WAVELENGTH_DATA_DIR", "./data"))
	if err != nil {
		return err
	}
	defer db.Close()
	var lib *library.Library
	if dir := os.Getenv("WAVELENGTH_MUSIC_DIR"); dir != "" {
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
	if *scanOnly {
		if lib == nil {
			return errors.New("WAVELENGTH_MUSIC_DIR is required for --scan-only")
		}
		return nil
	}
	addr := env("WAVELENGTH_ADDR", "127.0.0.1:8080")
	token := os.Getenv("WAVELENGTH_TOKEN")
	manager, err := auth.New(ctx, auth.Config{
		Issuer: os.Getenv("WAVELENGTH_OIDC_ISSUER"), ClientID: os.Getenv("WAVELENGTH_OIDC_CLIENT_ID"),
		ClientSecret: os.Getenv("WAVELENGTH_OIDC_CLIENT_SECRET"), PublicURL: os.Getenv("WAVELENGTH_PUBLIC_URL"), Token: token,
	}, database.AuthStore{DB: db})
	if err != nil {
		return err
	}
	if err = validateNativeAddress(addr, token, manager.OIDCEnabled()); err != nil {
		return err
	}
	options := []httpserver.Option{httpserver.WithAuth(manager)}
	if lib != nil {
		options = append(options, httpserver.WithNative(lib, manager))
	}
	servers := []*http.Server{httpserver.New(addr, db.PingContext, options...)}
	// Legacy protocol is opt-in and has its own listener and credentials.
	if password := os.Getenv("WAVELENGTH_SUBSONIC_PASSWORD"); password != "" {
		if lib == nil {
			return errors.New("legacy Subsonic requires WAVELENGTH_MUSIC_DIR")
		}
		key, err := state.LoadKey(env("WAVELENGTH_DATA_DIR", "./data"))
		if err != nil {
			return err
		}
		legacy, err := subsonic.New(env("WAVELENGTH_SUBSONIC_ADDR", "127.0.0.1:8081"), lib, env("WAVELENGTH_SUBSONIC_USER", "admin"), password, subsonic.WithState(database.StateStore{DB: db}), subsonic.WithContext(ctx), subsonic.WithEncryptionKey(key))
		if err != nil {
			return err
		}
		servers = append(servers, legacy)
	} else if os.Getenv("WAVELENGTH_SUBSONIC_ADDR") != "" || os.Getenv("WAVELENGTH_SUBSONIC_USER") != "" {
		return errors.New("WAVELENGTH_SUBSONIC_PASSWORD is required to enable the legacy listener")
	}
	return serve(ctx, servers)
}

// Bind all listeners before serving any, and stop both if either listener fails.
func serve(ctx context.Context, servers []*http.Server) error {
	listeners := make([]net.Listener, 0, len(servers))
	defer func() {
		for _, l := range listeners {
			_ = l.Close()
		}
	}()
	for _, server := range servers {
		listener, err := net.Listen("tcp", server.Addr)
		if err != nil {
			return fmt.Errorf("listen %s: %w", server.Addr, err)
		}
		listeners = append(listeners, listener)
	}
	done := make(chan error, len(servers))
	for i, server := range servers {
		defer server.Close()
		go func() { done <- server.Serve(listeners[i]) }()
		slog.Info("wavelength listening", "protocol", []string{"native", "legacy-subsonic"}[i], "address", listeners[i].Addr().String())
	}
	var serveErr error
	select {
	case serveErr = <-done:
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, server := range servers {
		if err := server.Shutdown(shutdown); err != nil {
			return err
		}
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		return nil
	}
	return serveErr
}

func validateNativeAddress(addr, token string, oidcEnabled bool) error {
	protected := token != "" || oidcEnabled
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid native listen address: %w", err)
	}
	ip := net.ParseIP(host)
	if !protected && (ip == nil || !ip.IsLoopback()) {
		return errors.New("OIDC or WAVELENGTH_TOKEN is required for non-loopback native listeners")
	}
	return nil
}
func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
