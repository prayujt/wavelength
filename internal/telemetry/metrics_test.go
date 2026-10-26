package telemetry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func scrape(t *testing.T, m *Metrics) string {
	t.Helper()
	w := httptest.NewRecorder()
	promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{}).ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	return w.Body.String()
}

func TestMetricsExposeCountsLatencyQueueAndRuntime(t *testing.T) {
	m := New(2)
	m.SetSnapshot(func(context.Context) (Snapshot, error) {
		return Snapshot{Jobs: map[string]float64{"queued": 3}, Items: map[string]float64{"downloading": 2}}, nil
	})
	done := m.Stage("download")
	done(nil)
	done = m.Stage("download")
	done(errors.New("private track and upstream detail"))
	w := m.Worker()
	j := m.Job(time.Now().Add(-time.Second))
	m.Item(time.Now(), "reused")
	w()
	j("completed")
	m.Operation("retry", nil)
	m.Publication("import", 12345)
	m.Provider("itunes", "lookup", 429, nil, time.Millisecond)
	m.Wait("itunes", "rate_limit", time.Second)
	m.Cache("jev", true)
	m.Tokens(200, 20)
	m.PlayFeed("jev", 10, nil, time.Millisecond)
	m.PlayFeed("private-owner", 0, errors.New("private track"), time.Second)
	body := scrape(t, m)
	for _, expected := range []string{
		`wavelength_downloads_total{outcome="ok"} 1`,
		`wavelength_downloads_total{outcome="error"} 1`,
		`wavelength_ingest_stage_duration_seconds_count{outcome="ok",stage="download"} 1`,
		`wavelength_ingest_jobs{state="queued"} 3`,
		`wavelength_ingest_items{state="downloading"} 2`,
		`wavelength_ingest_items_active 0`,
		`wavelength_ingest_jobs_active 0`,
		`wavelength_audio_published_bytes_total{kind="import"} 12345`,
		`wavelength_provider_requests_total{operation="lookup",provider="itunes",status="4xx"} 1`,
		`wavelength_provider_cache_lookups_total{provider="jev",result="hit"} 1`,
		`wavelength_jev_tokens_total{direction="input"} 200`,
		`wavelength_play_feed_requests_total{outcome="ok",source="jev"} 1`,
		`wavelength_play_feed_requests_total{outcome="error",source="error"} 1`,
		`wavelength_play_feed_items_sum 10`,
		`wavelength_play_feed_duration_seconds_count{outcome="ok",source="jev"} 1`,
		`go_goroutines`, `process_cpu_seconds_total`,
	} {
		if !strings.Contains(body, expected) {
			t.Error("missing metric", expected)
		}
	}
	if strings.Contains(body, "private track") || strings.Contains(body, "upstream detail") {
		t.Fatal("error text leaked into labels")
	}
	m.SetSnapshot(func(context.Context) (Snapshot, error) { return Snapshot{}, errors.New("private owner") })
	body = scrape(t, m)
	if !strings.Contains(body, "wavelength_ingest_queue_scrape_error 1") || strings.Contains(body, "private owner") {
		t.Fatal("unsafe scrape failure")
	}
}

func TestHTTPMetricsPreserveStreamingAndBoundLabels(t *testing.T) {
	m := New(2)
	h := m.HTTP("native", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte("audio"))
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/media/tracks/private-id?token=secret", nil))
	if w.Body.String() != "audio" || !w.Flushed {
		t.Fatal("streaming interface changed")
	}
	body := scrape(t, m)
	if !strings.Contains(body, `wavelength_http_requests_total{code="200",listener="native",method="get"} 1`) {
		t.Fatal("request counter missing")
	}
	if strings.Contains(body, "private-id") || strings.Contains(body, "secret") {
		t.Fatal("path or credentials leaked into metrics")
	}
}
