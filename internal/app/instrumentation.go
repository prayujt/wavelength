package app

import (
	"context"

	"github.com/prayujt/wavelength/internal/discovery"
	"github.com/prayujt/wavelength/internal/ingest"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/telemetry"
)

type measuredMetadata struct {
	discovery.Metadata
	metrics *telemetry.Metrics
}

func (m measuredMetadata) Search(ctx context.Context, q string, limit int) (result discovery.Results, err error) {
	done := m.metrics.Stage("metadata_search")
	defer func() { done(err) }()
	return m.Metadata.Search(ctx, q, limit)
}
func (m measuredMetadata) Track(ctx context.Context, ref discovery.Ref) (result discovery.Track, err error) {
	done := m.metrics.Stage("track_metadata")
	defer func() { done(err) }()
	return m.Metadata.Track(ctx, ref)
}
func (m measuredMetadata) Release(ctx context.Context, ref discovery.Ref) (result discovery.Release, err error) {
	done := m.metrics.Stage("release_metadata")
	defer func() { done(err) }()
	return m.Metadata.Release(ctx, ref)
}
func (m measuredMetadata) Releases(ctx context.Context, refs []discovery.Ref) (result []discovery.Release, err error) {
	done := m.metrics.Stage("release_metadata")
	defer func() { done(err) }()
	return m.Metadata.Releases(ctx, refs)
}
func (m measuredMetadata) Artist(ctx context.Context, ref discovery.Ref) (result discovery.Artist, err error) {
	done := m.metrics.Stage("artist_metadata")
	defer func() { done(err) }()
	return m.Metadata.Artist(ctx, ref)
}

type measuredSources struct {
	discovery.Sources
	metrics *telemetry.Metrics
}

func (m measuredSources) Candidates(ctx context.Context, t discovery.Track) (result []discovery.Candidate, err error) {
	done := m.metrics.Stage("source_search")
	defer func() { done(err) }()
	result, err = m.Sources.Candidates(ctx, t)
	if err == nil {
		m.metrics.Candidates(len(result))
	}
	return result, err
}

type measuredRanker struct {
	discovery.Ranker
	metrics *telemetry.Metrics
}

func (m measuredRanker) Rank(ctx context.Context, t discovery.Track, c []discovery.Candidate) (result []discovery.Candidate, err error) {
	done := m.metrics.Stage("ranking")
	defer func() { done(err) }()
	return m.Ranker.Rank(ctx, t, c)
}

type measuredDownloader struct {
	ingest.Downloader
	metrics *telemetry.Metrics
}

func (m measuredDownloader) Download(ctx context.Context, r discovery.Ref, path string) (err error) {
	done := m.metrics.Stage("download")
	defer func() { done(err) }()
	return m.Downloader.Download(ctx, r, path)
}

type measuredMedia struct {
	ingest.Media
	metrics *telemetry.Metrics
}

func (m measuredMedia) Prepare(ctx context.Context, path string, t discovery.Track) (result library.Track, warnings []string, err error) {
	done := m.metrics.Stage("tagging")
	defer func() { done(err) }()
	return m.Media.Prepare(ctx, path, t)
}
