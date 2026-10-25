package telemetry

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func (m *Metrics) initOperations() {
	m.selections = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "wavelength_source_selections_total", Help: "Recording selection attempts: automatic, explicit or manual_required."}, []string{"decision"})
	m.candidates = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "wavelength_source_candidates", Help: "Number of YouTube candidates returned by a successful source search.", Buckets: []float64{0, 1, 2, 5, 10, 15, 20, 50}})
	m.Registry.MustRegister(m.selections, m.candidates)
	m.operations = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "wavelength_ingest_operations_total", Help: "Submit, retry and cancel calls by result."}, []string{"operation", "outcome"})
	m.downloads = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "wavelength_downloads_total", Help: "Audio download attempts by outcome, including repeated attempts."}, []string{"outcome"})
	m.publications = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "wavelength_tracks_published_total", Help: "Newly published audio assets by kind: import or replacement."}, []string{"kind"})
	m.audioBytes = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "wavelength_audio_published_bytes_total", Help: "Bytes in successfully published tagged MP3 assets; not wire traffic."}, []string{"kind"})
	m.fileSize = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "wavelength_audio_file_size_bytes", Help: "Size of newly published tagged MP3 files.", Buckets: prometheus.ExponentialBuckets(64<<10, 2, 14)})
	m.providerRequests = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "wavelength_provider_requests_total", Help: "Provider HTTP or command attempts by bounded status category."}, []string{"provider", "operation", "status"})
	m.providerDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "wavelength_provider_request_duration_seconds", Help: "Provider request or command duration, excluding limiter and command-slot waits.", Buckets: []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 120, 300, 900}}, []string{"provider", "operation", "status"})
	m.waits = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "wavelength_provider_wait_duration_seconds", Help: "Provider rate-limit, retry-backoff, or command-slot wait duration.", Buckets: []float64{.001, .01, .1, .5, 1, 2.5, 5, 10, 30, 60}}, []string{"provider", "kind"})
	m.cache = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "wavelength_provider_cache_lookups_total", Help: "Cache lookups by provider and hit/miss result."}, []string{"provider", "result"})
	m.tokens = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "wavelength_jev_tokens_total", Help: "Tokens reported by successful Jev API responses."}, []string{"direction"})
	m.httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "wavelength_http_requests_total", Help: "HTTP requests by listener, method and status code; paths and identities are excluded."}, []string{"listener", "method", "code"})
	labels := []string{"listener", "method", "code"}
	m.httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "wavelength_http_request_duration_seconds", Help: "Full HTTP request duration, including stream lifetime.", Buckets: []float64{.001, .01, .05, .1, .5, 1, 5, 10, 30, 60, 300, 900, 3600}}, labels)
	m.httpHeaders = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "wavelength_http_time_to_headers_seconds", Help: "Time until response headers are written.", Buckets: []float64{.001, .01, .05, .1, .5, 1, 5, 10, 30, 60}}, labels)
	m.httpRequestSize = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "wavelength_http_request_size_bytes", Help: "Estimated HTTP request size.", Buckets: prometheus.ExponentialBuckets(256, 4, 8)}, labels)
	m.httpResponseSize = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "wavelength_http_response_size_bytes", Help: "HTTP response body bytes, including streamed audio.", Buckets: prometheus.ExponentialBuckets(1024, 4, 10)}, labels)
	m.httpActive = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "wavelength_http_requests_active", Help: "Active HTTP requests, including streams."}, []string{"listener"})
	m.Registry.MustRegister(m.operations, m.downloads, m.publications, m.audioBytes, m.fileSize, m.providerRequests, m.providerDuration, m.waits, m.cache, m.tokens, m.httpRequests, m.httpDuration, m.httpHeaders, m.httpRequestSize, m.httpResponseSize, m.httpActive)
}

func (m *Metrics) Selection(decision string) {
	if m != nil {
		m.selections.WithLabelValues(decision).Inc()
	}
}
func (m *Metrics) Candidates(count int) {
	if m != nil {
		m.candidates.Observe(float64(count))
	}
}

func (m *Metrics) Operation(operation string, err error) {
	if m != nil {
		m.operations.WithLabelValues(operation, outcome(err)).Inc()
	}
}
func (m *Metrics) Publication(kind string, bytes int64) {
	if m != nil {
		m.publications.WithLabelValues(kind).Inc()
		m.audioBytes.WithLabelValues(kind).Add(float64(max(0, bytes)))
		m.fileSize.Observe(float64(max(0, bytes)))
	}
}
func (m *Metrics) Provider(provider, operation string, code int, err error, duration time.Duration) {
	if m == nil {
		return
	}
	status := outcome(err)
	if code > 0 {
		status = fmt.Sprintf("%dxx", code/100)
	}
	m.providerRequests.WithLabelValues(provider, operation, status).Inc()
	m.providerDuration.WithLabelValues(provider, operation, status).Observe(duration.Seconds())
}
func (m *Metrics) Wait(provider, kind string, duration time.Duration) {
	if m != nil {
		m.waits.WithLabelValues(provider, kind).Observe(duration.Seconds())
	}
}
func (m *Metrics) Cache(provider string, hit bool) {
	if m != nil {
		result := "miss"
		if hit {
			result = "hit"
		}
		m.cache.WithLabelValues(provider, result).Inc()
	}
}
func (m *Metrics) Tokens(input, output int) {
	if m != nil {
		m.tokens.WithLabelValues("input").Add(float64(max(0, input)))
		m.tokens.WithLabelValues("output").Add(float64(max(0, output)))
	}
}
func (m *Metrics) Database(db *sql.DB) {
	m.Registry.MustRegister(collectors.NewDBStatsCollector(db, "wavelength"))
}

func (m *Metrics) HTTP(listener string, next http.Handler) http.Handler {
	labels := prometheus.Labels{"listener": listener}
	var h http.Handler = promhttp.InstrumentHandlerCounter(m.httpRequests.MustCurryWith(labels), next)
	h = promhttp.InstrumentHandlerDuration(m.httpDuration.MustCurryWith(labels), h)
	h = promhttp.InstrumentHandlerTimeToWriteHeader(m.httpHeaders.MustCurryWith(labels), h)
	h = promhttp.InstrumentHandlerRequestSize(m.httpRequestSize.MustCurryWith(labels), h)
	h = promhttp.InstrumentHandlerResponseSize(m.httpResponseSize.MustCurryWith(labels), h)
	return promhttp.InstrumentHandlerInFlight(m.httpActive.WithLabelValues(listener), h)
}
