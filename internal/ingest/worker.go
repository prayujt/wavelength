package ingest

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/prayujt/wavelength/internal/discovery"
)

func (s *Service) run() {
	defer close(s.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for s.ctx.Err() == nil {
		j, err := s.deps.Store.Next(s.ctx, s.root.ID)
		if err == nil {
			err = s.process(j.ID)
			if err == nil {
				continue
			}
		}
		if err != nil && !errors.Is(err, ErrNotFound) && s.ctx.Err() == nil {
			slog.Error("ingestion worker interrupted", "error", err)
		}
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
		case <-ticker.C:
		}
	}
}
func (s *Service) process(id string) error {
	s.control.Lock()
	ctx, cancel := context.WithCancel(s.ctx)
	s.activeMu.Lock()
	s.activeID = id
	s.activeCancel = cancel
	s.activeMu.Unlock()
	defer func() { cancel(); s.activeMu.Lock(); s.activeID = ""; s.activeCancel = nil; s.activeMu.Unlock() }()
	j, err := s.updateJob(ctx, id, func(j *Job) error {
		if j.State == Cancelled {
			return ErrCancelled
		}
		j.State = Running
		return nil
	})
	s.control.Unlock()
	if errors.Is(err, ErrCancelled) {
		return s.cleanCancelled(j)
	}
	if err != nil {
		return err
	}
	if !j.Expanded {
		lookup, cancel := context.WithTimeout(ctx, 45*time.Second)
		release, lookupErr := s.deps.Discovery.Release(lookup, j.Request.ReleaseRef)
		cancel()
		if lookupErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return s.failJob(ctx, id, "release metadata could not be resolved", lookupErr)
		}
		j, err = s.updateJob(ctx, id, func(j *Job) error {
			if j.State == Cancelled {
				return ErrCancelled
			}
			j.Items = []Item{}
			for _, t := range release.Tracks {
				metadata := t
				j.Items = append(j.Items, Item{Selection: Selection{MetadataRef: t.Ref}, Metadata: &metadata, State: Queued, Warnings: []string{}})
			}
			j.Expanded = true
			return nil
		})
		if err != nil {
			return err
		}
	}
	for n := range j.Items {
		if ctx.Err() != nil {
			break
		}
		current, err := s.deps.Store.Get(ctx, s.root.ID, j.Owner, id)
		if err != nil {
			return err
		}
		if current.State == Cancelled {
			break
		}
		if current.Items[n].State == Completed || current.Items[n].State == Failed || current.Items[n].State == NeedsSelection {
			continue
		}
		if err = s.processItem(ctx, current, n); err != nil {
			if ctx.Err() != nil {
				break
			}
			if errors.Is(err, ErrCancelled) {
				break
			}
			return err
		}
	}
	if ctx.Err() != nil {
		if s.ctx.Err() != nil {
			return s.ctx.Err()
		}
		fresh, err := s.deps.Store.Get(s.ctx, s.root.ID, j.Owner, id)
		if err == nil {
			err = s.cleanCancelled(fresh)
		}
		return err
	}
	return s.finish(ctx, id)
}
func (s *Service) failJob(ctx context.Context, id, message string, cause error) error {
	slog.Warn("import failed", "import", id, "error", cause)
	_, err := s.updateJob(ctx, id, func(j *Job) error {
		if j.State == Cancelled {
			return nil
		}
		j.State = Failed
		j.Error = message
		return nil
	})
	return err
}
func (s *Service) changeItem(ctx context.Context, id string, index int, change func(*Item)) error {
	_, err := s.updateJob(ctx, id, func(j *Job) error {
		if j.State == Cancelled {
			return ErrCancelled
		}
		change(&j.Items[index])
		return nil
	})
	return err
}
func (s *Service) itemFailure(ctx context.Context, j Job, n int, message string, cause error) error {
	slog.Warn("import item failed", "import", j.ID, "index", n, "error", cause)
	return s.changeItem(ctx, j.ID, n, func(i *Item) { i.State = Failed; i.Error = message })
}
func (s *Service) processItem(ctx context.Context, j Job, n int) error {
	item := j.Items[n]
	// If an unfinished asset was lost, acquire it again rather than retrying an
	// impossible finalization forever. Published catalog records are handled below.
	if item.Prepared != nil {
		final, finalErr := s.files.Open(item.Prepared.Track.Path)
		if finalErr == nil {
			final.Close()
		}
		_, stageErr := os.Stat(s.stagePath(j.ID, n))
		if errors.Is(finalErr, os.ErrNotExist) && errors.Is(stageErr, os.ErrNotExist) {
			item.Prepared = nil
			if err := s.changeItem(ctx, j.ID, n, func(i *Item) { i.Prepared = nil }); err != nil {
				return err
			}
		}
	}
	previousPath := ""
	if existing, err := s.deps.Store.Lookup(ctx, s.root.ID, item.MetadataRef); err == nil {
		if item.ReplaceTrackID != "" {
			if existing.ID != item.ReplaceTrackID {
				return s.itemFailure(ctx, j, n, "saved track no longer matches this replacement", ErrConflict)
			}
			previousPath = existing.Path
			if item.Metadata == nil {
				item.Metadata = &discovery.Track{Ref: item.MetadataRef, Title: existing.Title, Artist: existing.Artist, Album: existing.Album, AlbumArtist: existing.AlbumArtist, Genre: existing.Genre, ReleaseDate: strconv.Itoa(existing.Year), TrackNumber: existing.TrackNumber, DiscNumber: existing.DiscNumber, Duration: existing.Duration}
				if err := s.changeItem(ctx, j.ID, n, func(i *Item) { i.Metadata = item.Metadata }); err != nil {
					return err
				}
			}
		} else {
			file, err := s.files.Open(existing.Path)
			if err != nil {
				return s.itemFailure(ctx, j, n, "existing catalog track has no readable source file", err)
			}
			file.Close()
			_, err = s.publish(ctx, j, n, Prepared{})
			return err
		}
	} else if !errors.Is(err, ErrNotFound) {
		return err
	} else if item.ReplaceTrackID != "" {
		return s.itemFailure(ctx, j, n, "saved track no longer exists", err)
	}
	if item.Metadata == nil {
		if err := s.changeItem(ctx, j.ID, n, func(i *Item) { i.State = "resolving" }); err != nil {
			return err
		}
		lookup, cancel := context.WithTimeout(ctx, 45*time.Second)
		t, err := s.deps.Discovery.Track(lookup, item.MetadataRef)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return s.itemFailure(ctx, j, n, "track metadata could not be resolved", err)
		}
		item.Metadata = &t
		if err = s.changeItem(ctx, j.ID, n, func(i *Item) { i.Metadata = &t }); err != nil {
			return err
		}
	}
	if item.SourceRef.Empty() {
		lookup, cancel := context.WithTimeout(ctx, 60*time.Second)
		candidates, err := s.deps.Discovery.Candidates(lookup, *item.Metadata)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return s.itemFailure(ctx, j, n, "audio sources could not be resolved", err)
		}
		chosen, ok := s.deps.Discovery.Select(candidates)
		if !ok {
			return s.changeItem(ctx, j.ID, n, func(i *Item) {
				i.State = NeedsSelection
				i.Error = "choose a recording with find_audio_sources, then retry this item with its source reference"
			})
		}
		item.SourceRef = chosen
		item.Automatic = true
		if err = s.changeItem(ctx, j.ID, n, func(i *Item) { i.SourceRef = chosen; i.Automatic = true }); err != nil {
			return err
		}
	}
	if item.Prepared == nil {
		path := s.stagePath(j.ID, n)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return s.itemFailure(ctx, j, n, "cannot create download staging directory", err)
		}
		if err := s.changeItem(ctx, j.ID, n, func(i *Item) { i.State = "downloading" }); err != nil {
			return err
		}
		download, cancel := context.WithTimeout(ctx, 15*time.Minute)
		err := s.deps.Downloader.Download(download, item.SourceRef, path)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return s.itemFailure(ctx, j, n, "audio download failed", err)
		}
		if err = s.changeItem(ctx, j.ID, n, func(i *Item) { i.State = "tagging" }); err != nil {
			return err
		}
		prepare, cancel := context.WithTimeout(ctx, 60*time.Second)
		t, warnings, err := s.deps.Media.Prepare(prepare, path, *item.Metadata)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return s.itemFailure(ctx, j, n, "audio validation or tagging failed", err)
		}
		if previousPath != "" {
			previous, err := s.files.Open(previousPath)
			if err == nil {
				err = preserveMediaTags(path, previous)
				previous.Close()
			}
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return s.itemFailure(ctx, j, n, "saved artwork and lyrics could not be preserved", err)
			}
			file, err := os.OpenFile(path, os.O_RDWR, 0)
			if err != nil {
				return s.itemFailure(ctx, j, n, "replacement could not be read", err)
			}
			info, err := file.Stat()
			if err == nil {
				err = file.Sync()
			}
			file.Close()
			if err != nil {
				return s.itemFailure(ctx, j, n, "replacement could not be saved", err)
			}
			t.Size, t.ModifiedAt = info.Size(), info.ModTime().UTC()
		}
		if item.Automatic && !durationMatches(t.Duration, item.Metadata.Duration) {
			return s.itemFailure(ctx, j, n, "downloaded duration differs from the automatic source match", ErrInvalid)
		}
		assetID := rand.Text()
		t.ID = assetID
		if item.ReplaceTrackID != "" {
			t.ID = item.ReplaceTrackID
		}
		artist := t.AlbumArtist
		if artist == "" {
			artist = t.Artist
		}
		t.Path = filepath.Join(component(artist, "Unknown Artist"), component(t.Album, "Unknown Album"), assetID+".mp3")
		t.CreatedAt = time.Now().UTC()
		checksum, err := checksum(path)
		if err != nil {
			return s.itemFailure(ctx, j, n, "prepared audio could not be read", err)
		}
		item.Prepared = &Prepared{Track: t, SHA256: checksum, PreviousPath: previousPath}
		if err = s.changeItem(ctx, j.ID, n, func(i *Item) { i.Prepared = item.Prepared; i.Warnings = warnings; i.State = "publishing" }); err != nil {
			return err
		}
	}
	if err := s.publishFile(ctx, j.ID, n, *item.Prepared); err != nil {
		return s.itemFailure(ctx, j, n, "prepared audio could not be published", err)
	}
	t, err := s.publish(ctx, j, n, *item.Prepared)
	if err != nil {
		return err
	}
	if t.ID != item.Prepared.Track.ID {
		_ = s.files.Remove(item.Prepared.Track.Path)
	}
	if item.Prepared.PreviousPath != "" && t.Path == item.Prepared.Track.Path && t.Path != item.Prepared.PreviousPath {
		_ = s.files.Remove(item.Prepared.PreviousPath)
	}
	_ = os.RemoveAll(filepath.Dir(s.stagePath(j.ID, n)))
	return nil
}
func (s *Service) finish(ctx context.Context, id string) error {
	s.control.Lock()
	defer s.control.Unlock()
	// The worker owns the ID; read the current record inside Update after any cancellation.
	var final Job
	_, err := s.updateJob(ctx, id, func(j *Job) error { final = *j; return nil })
	if err != nil {
		return err
	}
	if final.State == Cancelled {
		return s.cleanCancelled(final)
	}
	complete, waiting := 0, 0
	ids := []string{}
	for n, i := range final.Items {
		if i.State == Failed && i.Prepared == nil {
			if err := s.cleanItem(final, n); err != nil {
				return err
			}
		}
		if i.State == Completed {
			if i.Prepared != nil && i.Prepared.PreviousPath != "" && i.Prepared.PreviousPath != i.Prepared.Track.Path {
				_ = s.files.Remove(i.Prepared.PreviousPath)
			}
			_ = os.RemoveAll(filepath.Dir(s.stagePath(final.ID, n)))
			complete++
			ids = append(ids, i.TrackID)
		}
		if i.State == NeedsSelection {
			waiting++
		}
	}
	state := Failed
	switch {
	case complete == len(final.Items):
		state = Completed
	case waiting > 0:
		state = NeedsSelection
	case complete > 0:
		state = Partial
	}
	message := ""
	if state == Completed && final.Request.PlaylistID != "" {
		if err = s.deps.Playlists.AppendImport(ctx, final.Owner, final.Request.PlaylistID, final.ID, ids); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			state = Partial
			message = "tracks were imported, but the target playlist could not be updated"
			slog.Warn("import playlist failed", "import", id, "error", err)
		}
	}
	_, err = s.updateJob(ctx, id, func(j *Job) error {
		if j.State == Cancelled {
			return nil
		}
		j.State = state
		j.Error = message
		return nil
	})
	return err
}
