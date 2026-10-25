package httpserver

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestShutdownCancelsLongLivedRequests(t *testing.T) {
	started := make(chan struct{})
	finished := make(chan struct{})
	server := New("", func(context.Context) error { return nil }, func(mux *http.ServeMux) {
		mux.HandleFunc("GET /stream", func(w http.ResponseWriter, r *http.Request) {
			close(started)
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			close(finished)
		})
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	go server.Serve(listener)
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://" + listener.Addr().String() + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("stream was not cancelled")
	}
}
