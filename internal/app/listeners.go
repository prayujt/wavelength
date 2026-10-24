package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

type server struct {
	name string
	http *http.Server
}

// Bind all listeners before serving any, and stop both if either listener fails.
func serve(ctx context.Context, servers []server) error {
	listeners := make([]net.Listener, 0, len(servers))
	defer func() {
		for _, l := range listeners {
			_ = l.Close()
		}
	}()
	for _, server := range servers {
		listener, err := net.Listen("tcp", server.http.Addr)
		if err != nil {
			return fmt.Errorf("listen %s: %w", server.http.Addr, err)
		}
		listeners = append(listeners, listener)
	}
	done := make(chan error, len(servers))
	for i, server := range servers {
		defer server.http.Close()
		go func() { done <- server.http.Serve(listeners[i]) }()
		slog.Info("wavelength listening", "protocol", server.name, "address", listeners[i].Addr().String())
	}
	var serveErr error
	select {
	case serveErr = <-done:
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, server := range servers {
		if err := server.http.Shutdown(shutdown); err != nil {
			serveErr = errors.Join(serveErr, err)
		}
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		return nil
	}
	return serveErr
}
