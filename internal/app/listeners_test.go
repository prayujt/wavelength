package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestSecondBindFailureReleasesFirstListener(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	first, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := first.Addr().String()
	first.Close()
	err = serve(context.Background(), []server{{name: "native", http: &http.Server{Addr: addr}}, {name: "legacy", http: &http.Server{Addr: occupied.Addr().String()}}})
	if err == nil {
		t.Fatal("expected bind failure")
	}
	reopened, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("first listener leaked: %v", err)
	}
	reopened.Close()
}

func TestBothListenersStopOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, []server{{name: "native", http: &http.Server{Addr: "127.0.0.1:0"}}, {name: "legacy", http: &http.Server{Addr: "127.0.0.1:0"}}})
	}()
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("listeners failed to stop")
	}
}
