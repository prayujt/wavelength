package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/prayujt/wavelength/internal/app"
	"github.com/prayujt/wavelength/internal/config"
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
	c, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return app.Run(ctx, c, *scanOnly)
}
