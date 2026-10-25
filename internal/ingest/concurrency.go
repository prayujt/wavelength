package ingest

import (
	"context"
	"time"

	"github.com/prayujt/wavelength/internal/discovery"
	"golang.org/x/sync/errgroup"
)

type itemLock struct {
	token chan struct{}
	users int
}

// Serialize identical references across jobs. The next attempt rechecks the
// catalog and reuses the first result instead of downloading the same song twice.
func (s *Service) lockItem(ctx context.Context, ref discovery.Ref) (func(), error) {
	started := time.Now()
	defer func() { s.deps.Metrics.WorkerWait("duplicate", time.Since(started)) }()
	s.keysMu.Lock()
	entry := s.keys[ref]
	if entry == nil {
		entry = &itemLock{token: make(chan struct{}, 1)}
		s.keys[ref] = entry
	}
	entry.users++
	s.keysMu.Unlock()
	releaseRef := func() {
		s.keysMu.Lock()
		entry.users--
		if entry.users == 0 {
			delete(s.keys, ref)
		}
		s.keysMu.Unlock()
	}
	select {
	case entry.token <- struct{}{}:
	case <-ctx.Done():
		releaseRef()
		return nil, ctx.Err()
	}
	return func() { <-entry.token; releaseRef() }, nil
}

func (s *Service) processItems(ctx context.Context, j Job, queued time.Time) error {
	group, work := errgroup.WithContext(ctx)
	for index := range j.Items {
		if j.Items[index].State == Completed || j.Items[index].State == Failed || j.Items[index].State == NeedsSelection {
			continue
		}
		waited := time.Now()
		select {
		case s.slots <- struct{}{}:
		case <-work.Done():
			s.deps.Metrics.WorkerWait("slot", time.Since(waited))
			return waitItems(group, work.Err())
		}
		s.deps.Metrics.WorkerWait("slot", time.Since(waited))
		group.Go(func() error {
			defer func() { <-s.slots }()
			done := s.deps.Metrics.Worker()
			defer done()
			unlock, err := s.lockItem(work, j.Items[index].MetadataRef)
			if err != nil {
				return err
			}
			defer unlock()
			current, err := s.deps.Store.Get(work, s.root.ID, j.Owner, j.ID)
			if err != nil {
				return err
			}
			if current.State == Cancelled {
				return ErrCancelled
			}
			item := current.Items[index]
			if item.State == Completed || item.State == Failed || item.State == NeedsSelection {
				return nil
			}
			started := time.Now()
			s.deps.Metrics.ItemQueue(queued)
			err = s.processItem(work, current, index)
			result := "error"
			if work.Err() != nil {
				result = "cancelled"
			} else if fresh, readErr := s.deps.Store.Get(work, s.root.ID, j.Owner, j.ID); readErr == nil {
				result = fresh.Items[index].State
				if result == Completed && fresh.Items[index].Reused {
					result = "reused"
				}
			}
			s.deps.Metrics.Item(started, result)
			return err
		})
	}
	return group.Wait()
}

func waitItems(group *errgroup.Group, fallback error) error {
	if err := group.Wait(); err != nil {
		return err
	}
	return fallback
}
