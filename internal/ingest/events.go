package ingest

import (
	"context"

	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/realtime"
)

func (s *Service) Events() *realtime.Hub { return s.deps.Events }

func (s *Service) changed(j Job) {
	s.deps.Events.Publish(realtime.Event{Owner: j.Owner, Change: realtime.ImportChanged{ID: j.ID}})
}

func (s *Service) updateJob(ctx context.Context, id string, change func(*Job) error) (Job, error) {
	j, err := s.deps.Store.Update(ctx, id, change)
	if err == nil {
		s.changed(j)
	}
	return j, err
}

func (s *Service) publish(ctx context.Context, job Job, index int, prepared Prepared) (library.Track, error) {
	t, err := s.deps.Store.Publish(ctx, s.root, job.ID, index, prepared)
	if err == nil {
		if prepared.Track.Path != "" && t.Path == prepared.Track.Path {
			kind := "import"
			if job.Items[index].ReplaceTrackID != "" {
				kind = "replacement"
			}
			s.deps.Metrics.Publication(kind, t.Size)
		}
		s.changed(job)
		s.deps.Events.Publish(realtime.Event{Change: realtime.TrackPublished{Ref: job.Items[index].MetadataRef, TrackID: t.ID}})
	}
	return t, err
}
