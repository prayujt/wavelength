package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestNativeBindingRequiresTokenOutsideLoopback(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:8080", ":8080", "[::]:8080"} {
		if validateNativeAddress(addr, "") == nil {
			t.Fatalf("unprotected public bind: %s", addr)
		}
		if err := validateNativeAddress(addr, "token"); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateNativeAddress("127.0.0.1:8080", ""); err != nil {
		t.Fatal(err)
	}
}

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
	err = serve(context.Background(), []*http.Server{{Addr: addr}, {Addr: occupied.Addr().String()}})
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
	go func() { done <- serve(ctx, []*http.Server{{Addr: "127.0.0.1:0"}, {Addr: "127.0.0.1:0"}}) }()
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
