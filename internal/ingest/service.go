package ingest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/prayujt/wavelength/internal/discovery"
	"github.com/prayujt/wavelength/internal/realtime"
	"github.com/prayujt/wavelength/internal/telemetry"
)

type Dependencies struct {
	Events      *realtime.Hub
	Store       Store
	Discovery   *discovery.Service
	Downloader  Downloader
	Media       Media
	Playlists   Playlists
	Concurrency int
	Metrics     *telemetry.Metrics
}
type Service struct {
	deps     Dependencies
	root     Root
	files    *os.Root
	staging  string
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	wake     chan struct{}
	control  sync.Mutex
	activeMu sync.Mutex
	active   map[string]context.CancelFunc
	slots    chan struct{}
	keysMu   sync.Mutex
	keys     map[discovery.Ref]*itemLock
}

// New resumes only recorded jobs. Neither startup nor recovery walks the source library.
func New(ctx context.Context, root Root, staging string, deps Dependencies) (*Service, error) {
	if deps.Concurrency == 0 {
		deps.Concurrency = 1
	}
	if deps.Concurrency < 1 || deps.Concurrency > 8 {
		return nil, ErrInvalid
	}
	if deps.Events == nil {
		deps.Events = realtime.NewHub()
	}
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
	s := &Service{deps: deps, root: root, files: files, staging: staging, ctx: ctx, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1), active: make(map[string]context.CancelFunc), slots: make(chan struct{}, deps.Concurrency), keys: make(map[discovery.Ref]*itemLock)}
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
		if i.ReplaceTrackID != "" && (!i.SourceRef.ValidSource() || len(r.Items) != 1 || !r.ReleaseRef.Empty() || r.PlaylistID != "") {
			return ErrInvalid
		}
	}
	return nil
}
func (s *Service) Create(ctx context.Context, owner string, r Request) (result Job, err error) {
	defer func() { s.deps.Metrics.Operation("create", err) }()
	if owner == "" {
		return Job{}, ErrInvalid
	}
	if err := validate(r); err != nil {
		return Job{}, err
	}
	for _, item := range r.Items {
		if item.ReplaceTrackID == "" {
			continue
		}
		track, err := s.deps.Store.Lookup(ctx, s.root.ID, item.MetadataRef)
		if err != nil {
			return Job{}, err
		}
		if track.ID != item.ReplaceTrackID {
			return Job{}, ErrConflict
		}
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
	j := Job{ID: rand.Text(), RootID: s.root.ID, Owner: owner, Request: r, RequestHash: hex.EncodeToString(hash[:]), State: Queued, Items: []Item{}, CreatedAt: now, UpdatedAt: now, QueuedAt: now, Expanded: r.ReleaseRef.Empty()}
	for _, i := range r.Items {
		j.Items = append(j.Items, Item{Selection: i, Preview: s.deps.Discovery.Preview(i.MetadataRef), State: Queued, Warnings: []string{}})
	}
	j, err = s.deps.Store.Create(ctx, j)
	if err == nil {
		s.changed(j)
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
func (s *Service) Cancel(ctx context.Context, owner, id string) (result Job, err error) {
	defer func() { s.deps.Metrics.Operation("cancel", err) }()
	s.control.Lock()
	defer s.control.Unlock()
	j, err := s.updateJob(ctx, id, func(j *Job) error {
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
	cancel, active := s.active[id]
	if active {
		cancel()
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
func (s *Service) Retry(ctx context.Context, owner, id string, overrides []Override) (result Job, err error) {
	defer func() { s.deps.Metrics.Operation("retry", err) }()
	s.control.Lock()
	defer s.control.Unlock()
	old, err := s.Get(ctx, owner, id)
	if err != nil {
		return old, err
	}
	s.activeMu.Lock()
	_, active := s.active[id]
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
	}
	for _, o := range overrides {
		if err := s.cleanItem(old, o.Index); err != nil {
			return old, err
		}
	}
	j, err := s.updateJob(ctx, id, func(j *Job) error {
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
		j.QueuedAt = time.Now().UTC()
		j.Cleaned = false
		j.Error = ""
		return nil
	})
	if err == nil {
		s.notify()
	}
	return j, err
}
