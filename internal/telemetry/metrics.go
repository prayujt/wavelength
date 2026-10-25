// Package telemetry owns bounded, anonymous Prometheus instrumentation.
package telemetry

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Snapshot struct {
	Jobs, Items                                  map[string]float64
	LibraryTracks, LibraryBytes, LibraryDuration float64
	OldestQueuedAt                               time.Time
}

type Metrics struct {
	itemQueue                                                            prometheus.Histogram
	workerWait                                                           *prometheus.HistogramVec
	selections                                                           *prometheus.CounterVec
	candidates                                                           prometheus.Histogram
	operations, downloads, publications, providerRequests, cache, tokens *prometheus.CounterVec
	providerDuration, waits                                              *prometheus.HistogramVec
	audioBytes                                                           *prometheus.CounterVec
	fileSize                                                             prometheus.Histogram
	httpRequests                                                         *prometheus.CounterVec
	httpDuration, httpHeaders, httpRequestSize, httpResponseSize         *prometheus.HistogramVec
	httpActive                                                           *prometheus.GaugeVec
	Registry                                                             *prometheus.Registry
	stages                                                               *prometheus.HistogramVec
	stageActive                                                          *prometheus.GaugeVec
	items                                                                *prometheus.CounterVec
	itemDuration                                                         *prometheus.HistogramVec
	jobs                                                                 *prometheus.HistogramVec
	queue                                                                prometheus.Histogram
	workers, activeJobs                                                  prometheus.Gauge
	mu                                                                   sync.RWMutex
	snapshot                                                             func(context.Context) (Snapshot, error)
}

func New(concurrency int) *Metrics {
	m := &Metrics{Registry: prometheus.NewRegistry()}
	m.itemQueue = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "wavelength_ingest_item_queue_wait_seconds", Help: "Submission/retry-to-item-start latency, including time behind earlier items in a batch.", Buckets: prometheus.ExponentialBuckets(.1, 2, 20)})
	m.workerWait = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "wavelength_ingest_worker_wait_duration_seconds", Help: "Time waiting for the global item slot or duplicate-reference lock.", Buckets: []float64{.001, .01, .1, .5, 1, 5, 10, 30, 60, 300, 900}}, []string{"kind"})
	m.Registry.MustRegister(m.itemQueue, m.workerWait)
	buckets := []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 120, 300, 900}
	m.stages = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "wavelength_ingest_stage_duration_seconds", Help: "Latency of acquisition stages, including provider waits; download includes yt-dlp conversion.", Buckets: buckets}, []string{"stage", "outcome"})
	m.stageActive = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "wavelength_ingest_stage_active", Help: "Operations currently executing in each acquisition stage."}, []string{"stage"})
	m.items = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "wavelength_ingest_item_attempts_total", Help: "Item attempts by outcome; retries are separate attempts."}, []string{"outcome"})
	m.itemDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "wavelength_ingest_item_duration_seconds", Help: "Active item attempt duration, excluding queue and duplicate-reference lock waits.", Buckets: buckets}, []string{"outcome"})
	m.jobs = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "wavelength_ingest_job_duration_seconds", Help: "Job execution duration per attempt, excluding queue and manual selection waits.", Buckets: prometheus.ExponentialBuckets(1, 2, 18)}, []string{"outcome"})
	m.queue = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "wavelength_ingest_queue_wait_seconds", Help: "Time from submission or retry until a job starts; recovered older jobs fall back to creation time.", Buckets: prometheus.ExponentialBuckets(.1, 2, 20)})
	m.workers = prometheus.NewGauge(prometheus.GaugeOpts{Name: "wavelength_ingest_items_active", Help: "Item attempts currently holding a worker slot, including duplicate-reference lock waits."})
	m.activeJobs = prometheus.NewGauge(prometheus.GaugeOpts{Name: "wavelength_ingest_jobs_active", Help: "Jobs currently executing an attempt."})
	limit := prometheus.NewGauge(prometheus.GaugeOpts{Name: "wavelength_ingest_concurrency_limit", Help: "Maximum concurrent item attempts across all jobs."})
	limit.Set(float64(concurrency))
	m.Registry.MustRegister(m.stages, m.stageActive, m.items, m.itemDuration, m.jobs, m.queue, m.workers, m.activeJobs, limit, prometheus.NewGoCollector(), prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	m.Registry.MustRegister(newQueueCollector(m))
	m.initOperations()
	return m
}

func (m *Metrics) ItemQueue(queued time.Time) {
	if m != nil && !queued.IsZero() {
		m.itemQueue.Observe(max(0, time.Since(queued).Seconds()))
	}
}
func (m *Metrics) WorkerWait(kind string, duration time.Duration) {
	if m != nil {
		m.workerWait.WithLabelValues(kind).Observe(duration.Seconds())
	}
}

func outcome(err error) string {
	if err == nil {
		return "ok"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return "error"
}

func (m *Metrics) Stage(stage string) func(error) {
	if m == nil {
		return func(error) {}
	}
	start := time.Now()
	m.stageActive.WithLabelValues(stage).Inc()
	return func(err error) {
		m.stageActive.WithLabelValues(stage).Dec()
		m.stages.WithLabelValues(stage, outcome(err)).Observe(time.Since(start).Seconds())
		if stage == "download" {
			m.downloads.WithLabelValues(outcome(err)).Inc()
		}
	}
}
func (m *Metrics) Worker() func() {
	if m == nil {
		return func() {}
	}
	m.workers.Inc()
	return m.workers.Dec
}
func (m *Metrics) Job(queued time.Time) func(string) {
	if m == nil {
		return func(string) {}
	}
	start := time.Now()
	m.activeJobs.Inc()
	if !queued.IsZero() {
		m.queue.Observe(max(0, time.Since(queued).Seconds()))
	}
	return func(result string) {
		m.activeJobs.Dec()
		m.jobs.WithLabelValues(result).Observe(time.Since(start).Seconds())
	}
}
func (m *Metrics) Item(start time.Time, result string) {
	if m != nil {
		m.items.WithLabelValues(result).Inc()
		m.itemDuration.WithLabelValues(result).Observe(time.Since(start).Seconds())
	}
}
func (m *Metrics) SetSnapshot(fn func(context.Context) (Snapshot, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshot = fn
}

type queueCollector struct {
	metrics                                              *Metrics
	jobs, items, failed, tracks, bytes, duration, oldest *prometheus.Desc
}

func newQueueCollector(m *Metrics) *queueCollector {
	return &queueCollector{metrics: m,
		jobs:     prometheus.NewDesc("wavelength_ingest_jobs", "Persisted jobs by state.", []string{"state"}, nil),
		items:    prometheus.NewDesc("wavelength_ingest_items", "Persisted items by state.", []string{"state"}, nil),
		failed:   prometheus.NewDesc("wavelength_ingest_queue_scrape_error", "Whether the queue snapshot could not be read.", nil, nil),
		tracks:   prometheus.NewDesc("wavelength_library_tracks", "Tracks currently in the shared catalog.", nil, nil),
		bytes:    prometheus.NewDesc("wavelength_library_audio_bytes", "Tagged audio bytes referenced by the shared catalog.", nil, nil),
		duration: prometheus.NewDesc("wavelength_library_audio_duration_seconds", "Audio duration referenced by the shared catalog.", nil, nil),
		oldest:   prometheus.NewDesc("wavelength_ingest_oldest_queued_job_age_seconds", "Age of the oldest queued job since its most recent submission or retry.", nil, nil),
	}
}
func (c *queueCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{c.jobs, c.items, c.failed, c.tracks, c.bytes, c.duration, c.oldest} {
		ch <- desc
	}
}
func (c *queueCollector) Collect(ch chan<- prometheus.Metric) {
	c.metrics.mu.RLock()
	fn := c.metrics.snapshot
	c.metrics.mu.RUnlock()
	snapshot := Snapshot{Jobs: map[string]float64{}, Items: map[string]float64{}}
	var err error
	if fn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		snapshot, err = fn(ctx)
		cancel()
	}
	failed := 0.
	if err != nil {
		failed = 1
	}
	ch <- prometheus.MustNewConstMetric(c.failed, prometheus.GaugeValue, failed)
	if err != nil {
		return
	}
	ch <- prometheus.MustNewConstMetric(c.tracks, prometheus.GaugeValue, snapshot.LibraryTracks)
	ch <- prometheus.MustNewConstMetric(c.bytes, prometheus.GaugeValue, snapshot.LibraryBytes)
	ch <- prometheus.MustNewConstMetric(c.duration, prometheus.GaugeValue, snapshot.LibraryDuration)
	age := 0.
	if !snapshot.OldestQueuedAt.IsZero() {
		age = max(0, time.Since(snapshot.OldestQueuedAt).Seconds())
	}
	ch <- prometheus.MustNewConstMetric(c.oldest, prometheus.GaugeValue, age)
	// Fixed enumerations prevent persisted input from creating arbitrary labels.
	for _, state := range []string{"queued", "running", "completed", "failed", "partial", "cancelled", "needs_selection"} {
		ch <- prometheus.MustNewConstMetric(c.jobs, prometheus.GaugeValue, snapshot.Jobs[state], state)
	}
	for _, state := range []string{"queued", "resolving", "downloading", "tagging", "publishing", "completed", "deleted", "failed", "cancelled", "needs_selection"} {
		ch <- prometheus.MustNewConstMetric(c.items, prometheus.GaugeValue, snapshot.Items[state], state)
	}
}
