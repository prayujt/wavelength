package httpserver

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prayujt/wavelength/internal/telemetry"
	"golang.org/x/net/http2"
)

func TestMetricsAreOnlyOnTheirOperationalListener(t *testing.T) {
	m := telemetry.New(2)
	w := httptest.NewRecorder()
	Metrics("127.0.0.1:9090", m.Registry).Handler.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "wavelength_ingest_concurrency_limit 2") {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	New("127.0.0.1:8080", func(context.Context) error { return nil }).Handler.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != 404 {
		t.Fatal("metrics exposed on native listener", w.Code)
	}
	w = httptest.NewRecorder()
	Metrics("127.0.0.1:9090", m.Registry).Handler.ServeHTTP(w, httptest.NewRequest("GET", "/auth/login", nil))
	if w.Code != 404 {
		t.Fatal("operational listener exposes application routes")
	}
}

func TestHTTP2RequestsAreInstrumentedInsideH2C(t *testing.T) {
	m := telemetry.New(2)
	server := httptest.NewServer(NewWithMetrics("127.0.0.1:0", func(context.Context) error { return nil }, m).Handler)
	defer server.Close()
	transport := &http2.Transport{AllowHTTP: true, DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport}).Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.ProtoMajor != 2 || response.StatusCode != 200 {
		t.Fatal("not an HTTP/2 request", response.Proto)
	}
	w := httptest.NewRecorder()
	Metrics("", m.Registry).Handler.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(w.Body.String(), `wavelength_http_requests_total{code="200",listener="native",method="get"} 1`) {
		t.Fatal("HTTP/2 request bypassed metrics")
	}
}
