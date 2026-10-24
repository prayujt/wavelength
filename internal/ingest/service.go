package ingest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/prayujt/wavelength/internal/discovery"
)

type Dependencies struct {
	Store      Store
	Discovery  *discovery.Service
	Downloader Downloader
	Media      Media
	Playlists  Playlists
}
type Service struct {
	deps         Dependencies
	root         Root
	files        *os.Root
	staging      string
	ctx          context.Context
	cancel       context.CancelFunc
	done         chan struct{}
	wake         chan struct{}
	control      sync.Mutex
	activeMu     sync.Mutex
	activeID     string
	activeCancel context.CancelFunc
}

// New resumes only recorded jobs. Neither startup nor recovery walks the source library.
func New(ctx context.Context, root Root, staging string, deps Dependencies) (*Service, error) {
	if err := deps.Store.EnsureRoot(ctx, root); err != nil {
		return nil, err
	}
	files, err := os.OpenRoot(root.Path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(staging, 0700); err != nil {
		files.Close()
		return nil, err
	}
	staging, err = filepath.Abs(staging)
	if err != nil {
		files.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Service{deps: deps, root: root, files: files, staging: staging, ctx: ctx, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1)}
	go s.run()
	return s, nil
}
func (s *Service) Close() { s.cancel(); <-s.done; s.files.Close() }
func (s *Service) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func validate(r Request) error {
	if strings.TrimSpace(r.RequestID) == "" || len(r.RequestID) > 128 || len(r.Items) > MaxItems {
		return ErrInvalid
	}
	if r.ReleaseRef.Empty() == (len(r.Items) == 0) {
		return ErrInvalid
	}
	if !r.ReleaseRef.Empty() && !r.ReleaseRef.ValidMetadata() {
		return ErrInvalid
	}
	for _, i := range r.Items {
		if !i.MetadataRef.ValidMetadata() || (!i.SourceRef.Empty() && !i.SourceRef.ValidSource()) {
			return ErrInvalid
		}
	}
	return nil
}
func (s *Service) Create(ctx context.Context, owner string, r Request) (Job, error) {
	if owner == "" {
		return Job{}, ErrInvalid
	}
	if err := validate(r); err != nil {
		return Job{}, err
	}
	if r.PlaylistID != "" {
		if err := s.deps.Playlists.CheckImport(ctx, owner, r.PlaylistID); err != nil {
			return Job{}, err
		}
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return Job{}, err
	}
	hash := sha256.Sum256(raw)
	now := time.Now().UTC()
	j := Job{ID: rand.Text(), RootID: s.root.ID, Owner: owner, Request: r, RequestHash: hex.EncodeToString(hash[:]), State: Queued, Items: []Item{}, CreatedAt: now, UpdatedAt: now, Expanded: r.ReleaseRef.Empty()}
	for _, i := range r.Items {
		j.Items = append(j.Items, Item{Selection: i, State: Queued, Warnings: []string{}})
	}
	j, err = s.deps.Store.Create(ctx, j)
	if err == nil {
		s.notify()
	}
	return j, err
}
func (s *Service) Get(ctx context.Context, owner, id string) (Job, error) {
	return s.deps.Store.Get(ctx, s.root.ID, owner, id)
}
func (s *Service) List(ctx context.Context, owner string, limit, offset int) ([]Job, int, error) {
	if owner == "" || limit < 0 || limit > 100 || offset < 0 {
		return nil, 0, ErrInvalid
	}
	if limit == 0 {
		limit = 20
	}
	return s.deps.Store.List(ctx, s.root.ID, owner, limit, offset)
}
func (s *Service) owned(j *Job, owner string) error {
	if j.RootID != s.root.ID || j.Owner != owner {
		return ErrNotFound
	}
	return nil
}
func (s *Service) Cancel(ctx context.Context, owner, id string) (Job, error) {
	s.control.Lock()
	defer s.control.Unlock()
	j, err := s.deps.Store.Update(ctx, id, func(j *Job) error {
		if err := s.owned(j, owner); err != nil {
			return err
		}
		if j.State == Completed {
			return ErrConflict
		}
		j.State = Cancelled
		j.Cleaned = false
		j.Error = ""
		for n := range j.Items {
			if j.Items[n].State != Completed {
				j.Items[n].State = Cancelled
			}
		}
		return nil
	})
	if err != nil {
		return j, err
	}
	s.activeMu.Lock()
	active := s.activeID == id
	if active {
		s.activeCancel()
	}
	s.activeMu.Unlock()
	if !active {
		if err := s.cleanCancelled(j); err != nil {
			return j, err
		}
	}
	s.notify()
	return j, nil
}
func (s *Service) Retry(ctx context.Context, owner, id string, overrides []Override) (Job, error) {
	s.control.Lock()
	defer s.control.Unlock()
	old, err := s.Get(ctx, owner, id)
	if err != nil {
		return old, err
	}
	s.activeMu.Lock()
	active := s.activeID == id
	s.activeMu.Unlock()
	if active {
		return old, ErrConflict
	}
	if !old.Terminal() || old.State == Completed {
		return old, ErrConflict
	}
	seen := map[int]bool{}
	for _, o := range overrides {
		if o.Index < 0 || o.Index >= len(old.Items) || seen[o.Index] || old.Items[o.Index].State == Completed || !o.SourceRef.ValidSource() {
			return old, ErrInvalid
		}
		seen[o.Index] = true
		if err := s.cleanItem(old, o.Index); err != nil {
			return old, err
		}
	}
	j, err := s.deps.Store.Update(ctx, id, func(j *Job) error {
		if err := s.owned(j, owner); err != nil {
			return err
		}
		if !j.Terminal() || j.State == Completed {
			return ErrConflict
		}
		for _, o := range overrides {
			j.Items[o.Index].SourceRef = o.SourceRef
			j.Items[o.Index].Prepared = nil
			j.Items[o.Index].Automatic = false
		}
		for n := range j.Items {
			if j.Items[n].State != Completed {
				j.Items[n].State = Queued
				j.Items[n].Error = ""
			}
		}
		j.State = Queued
		j.Cleaned = false
		j.Error = ""
		return nil
	})
	if err == nil {
		s.notify()
	}
	return j, err
}
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
	j, err := s.deps.Store.Update(ctx, id, func(j *Job) error {
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
		j, err = s.deps.Store.Update(ctx, id, func(j *Job) error {
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
	_, err := s.deps.Store.Update(ctx, id, func(j *Job) error {
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
	_, err := s.deps.Store.Update(ctx, id, func(j *Job) error {
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
	if existing, err := s.deps.Store.Lookup(ctx, s.root.ID, item.MetadataRef); err == nil {
		file, err := s.files.Open(existing.Path)
		if err != nil {
			return s.itemFailure(ctx, j, n, "existing catalog track has no readable source file", err)
		}
		file.Close()
		_, err = s.deps.Store.Publish(ctx, s.root, j.ID, n, Prepared{})
		return err
	} else if !errors.Is(err, ErrNotFound) {
		return err
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
		chosen, ok := discovery.Select(candidates)
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
		if item.Automatic && !durationMatches(t.Duration, item.Metadata.Duration) {
			return s.itemFailure(ctx, j, n, "downloaded duration differs from the automatic source match", ErrInvalid)
		}
		t.ID = rand.Text()
		artist := t.AlbumArtist
		if artist == "" {
			artist = t.Artist
		}
		t.Path = filepath.Join(component(artist, "Unknown Artist"), component(t.Album, "Unknown Album"), t.ID+".mp3")
		t.CreatedAt = time.Now().UTC()
		checksum, err := checksum(path)
		if err != nil {
			return s.itemFailure(ctx, j, n, "prepared audio could not be read", err)
		}
		item.Prepared = &Prepared{Track: t, SHA256: checksum}
		if err = s.changeItem(ctx, j.ID, n, func(i *Item) { i.Prepared = item.Prepared; i.Warnings = warnings; i.State = "publishing" }); err != nil {
			return err
		}
	}
	if err := s.publishFile(ctx, j.ID, n, *item.Prepared); err != nil {
		return s.itemFailure(ctx, j, n, "prepared audio could not be published", err)
	}
	t, err := s.deps.Store.Publish(ctx, s.root, j.ID, n, *item.Prepared)
	if err != nil {
		return err
	}
	if t.ID != item.Prepared.Track.ID {
		_ = s.files.Remove(item.Prepared.Track.Path)
	}
	_ = os.RemoveAll(filepath.Dir(s.stagePath(j.ID, n)))
	return nil
}
func (s *Service) finish(ctx context.Context, id string) error {
	s.control.Lock()
	defer s.control.Unlock()
	// The worker owns the ID; read the current record inside Update after any cancellation.
	var final Job
	_, err := s.deps.Store.Update(ctx, id, func(j *Job) error { final = *j; return nil })
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
	_, err = s.deps.Store.Update(ctx, id, func(j *Job) error {
		if j.State == Cancelled {
			return nil
		}
		j.State = state
		j.Error = message
		return nil
	})
	return err
}
func (s *Service) stagePath(id string, index int) string {
	return filepath.Join(s.staging, id, fmt.Sprint(index), "audio.mp3")
}
func checksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (s *Service) publishFile(ctx context.Context, id string, index int, p Prepared) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if existing, err := s.files.Open(p.Track.Path); err == nil {
		h := sha256.New()
		_, readErr := io.Copy(h, existing)
		existing.Close()
		if readErr == nil && hex.EncodeToString(h.Sum(nil)) == p.SHA256 {
			return nil
		}
		return fmt.Errorf("destination exists with different content")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := s.files.MkdirAll(filepath.Dir(p.Track.Path), 0755); err != nil {
		return err
	}
	source, err := os.Open(s.stagePath(id, index))
	if err != nil {
		return err
	}
	defer source.Close()
	pending := pendingPath(p)
	_ = s.files.Remove(pending)
	out, err := s.files.OpenFile(pending, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(out, h), source)
	if copyErr == nil {
		copyErr = out.Sync()
	}
	closeErr := out.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr == nil && hex.EncodeToString(h.Sum(nil)) != p.SHA256 {
		copyErr = errors.New("staging checksum changed")
	}
	if copyErr == nil {
		copyErr = ctx.Err()
	}
	if copyErr != nil {
		s.files.Remove(pending)
		return copyErr
	}
	if err = s.files.Rename(pending, p.Track.Path); err != nil {
		return err
	}
	directory, err := s.files.Open(filepath.Dir(p.Track.Path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
func (s *Service) cleanItem(j Job, n int) error {
	item := j.Items[n]
	var result error
	if item.Prepared != nil && item.State != Completed {
		existing, err := s.deps.Store.Lookup(s.ctx, s.root.ID, item.MetadataRef)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if errors.Is(err, ErrNotFound) || (err == nil && existing.ID != item.Prepared.Track.ID) {
			for _, path := range []string{item.Prepared.Track.Path, pendingPath(*item.Prepared)} {
				if err := s.files.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
					result = errors.Join(result, err)
				}
			}
		}
	}
	return errors.Join(result, os.RemoveAll(filepath.Dir(s.stagePath(j.ID, n))))
}
func (s *Service) cleanCancelled(j Job) error {
	if j.State != Cancelled {
		return nil
	}
	for n := range j.Items {
		if j.Items[n].State != Completed {
			if err := s.cleanItem(j, n); err != nil {
				return err
			}
		}
	}
	_, err := s.deps.Store.Update(s.ctx, j.ID, func(j *Job) error {
		if j.State == Cancelled {
			j.Cleaned = true
		}
		return nil
	})
	return err
}

func pendingPath(p Prepared) string {
	return filepath.Join(filepath.Dir(p.Track.Path), ".pending-"+p.Track.ID)
}

// Provider text may name a folder, but never supplies filesystem syntax.
func component(value, fallback string) string {
	if value == "" {
		return fallback
	}
	safe := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || unicode.IsControl(r) {
			return '_'
		}
		return r
	}, value)
	safe = strings.Trim(strings.TrimSpace(safe), ". ")
	runes := []rune(safe)
	if len(runes) > 80 {
		safe = string(runes[:80])
	}
	if safe == "" {
		safe = fallback
	}
	if safe != value {
		sum := sha256.Sum256([]byte(value))
		safe += "-" + hex.EncodeToString(sum[:4])
	}
	return safe
}
