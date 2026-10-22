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

	"github.com/prayujt/wavelength/internal/database"
	"github.com/prayujt/wavelength/internal/httpserver"
	"github.com/prayujt/wavelength/internal/library"
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
	addr := env("WAVELENGTH_ADDR", "127.0.0.1:8080")
	token := os.Getenv("WAVELENGTH_TOKEN")
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}
	ip := net.ParseIP(host)
	if token == "" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("WAVELENGTH_TOKEN is required for non-loopback listeners")
	}
	var options []httpserver.Option
	musicDir := os.Getenv("WAVELENGTH_MUSIC_DIR")
	if musicDir != "" {
		lib, err := library.Open(musicDir, database.Catalog{DB: db})
		if err != nil {
			return err
		}
		defer lib.Close()
		result, err := lib.Scan(ctx)
		if err != nil {
			return err
		}
		slog.Info("library indexed", "tracks", result.Indexed, "metadata_warnings", result.MetadataWarnings)
		if *scanOnly {
			return nil
		}
		options = append(options, httpserver.WithLibrary(lib, token))
	} else if *scanOnly {
		return fmt.Errorf("WAVELENGTH_MUSIC_DIR is required for --scan-only")
	}
	server := httpserver.New(addr, db.PingContext, options...)
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	slog.Info("wavelength listening", "address", listener.Addr().String())
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return err
		}
		return nil
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
