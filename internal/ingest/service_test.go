package ingest_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prayujt/wavelength/internal/database"
	"github.com/prayujt/wavelength/internal/discovery"
	"github.com/prayujt/wavelength/internal/ingest"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/userdata"
)

var metadataRef = discovery.Ref{Provider: "itunes", ID: "123"}
var recording = discovery.Ref{Provider: "youtube", ID: "BaW_jenozKc"}

type metadata struct{ release []discovery.Track }

func (metadata) Search(context.Context, string, int) (discovery.Results, error) {
	return discovery.Results{}, nil
}
func (metadata) Track(_ context.Context, r discovery.Ref) (discovery.Track, error) {
	return discovery.Track{Ref: r, Title: "Track " + r.ID, Artist: "Artist", Album: "Album", AlbumArtist: "Artist", Duration: 1}, nil
}
func (m metadata) Release(context.Context, discovery.Ref) (discovery.Release, error) {
	return discovery.Release{Ref: metadataRef, Tracks: m.release}, nil
}
func (metadata) Artist(context.Context, discovery.Ref) (discovery.Artist, error) {
	return discovery.Artist{}, nil
}

type sources struct{ candidates []discovery.Candidate }

func (s sources) Candidates(context.Context, discovery.Track) ([]discovery.Candidate, error) {
	return s.candidates, nil
}

type downloader func(context.Context, discovery.Ref, string) error

func (f downloader) Download(ctx context.Context, r discovery.Ref, path string) error {
	return f(ctx, r, path)
}

type media struct{ calls *atomic.Int32 }

func (m media) Prepare(_ context.Context, path string, t discovery.Track) (library.Track, []string, error) {
	if m.calls != nil {
		m.calls.Add(1)
	}
	info, err := os.Stat(path)
	return library.Track{Title: t.Title, Artist: t.Artist, Album: t.Album, AlbumArtist: t.AlbumArtist, ContentType: "audio/mpeg", Duration: 1, Size: info.Size(), ModifiedAt: info.ModTime()}, []string{}, err
}

type fixture struct {
	s       *ingest.Service
	store   database.IngestStore
	lib     *library.Library
	data    *userdata.Service
	root    ingest.Root
	staging string
	deps    ingest.Dependencies
}

func setup(t *testing.T, download downloader, m metadata, candidates []discovery.Candidate) *fixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	lib, err := library.Open(root, database.Catalog{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lib.Close() })
	id, path := lib.SourceRoot()
	f := &fixture{store: database.IngestStore{DB: db}, lib: lib, root: ingest.Root{ID: id, Path: path}, staging: filepath.Join(dir, "staging")}
	f.data = userdata.New(database.StateStore{DB: db}, lib)
	f.deps = ingest.Dependencies{Store: f.store, Discovery: discovery.New(m, sources{candidates}), Downloader: download, Media: media{}, Playlists: f.data}
	f.start(t)
	return f
}
func (f *fixture) start(t *testing.T) {
	t.Helper()
	var err error
	f.s, err = ingest.New(context.Background(), f.root, f.staging, f.deps)
	if err != nil {
		t.Fatal(err)
	}
	s := f.s
	t.Cleanup(func() {
		if f.s == s {
			f.s.Close()
			f.s = nil
		}
	})
}
func request(id string) ingest.Request {
	return ingest.Request{RequestID: id, Items: []ingest.Selection{{MetadataRef: metadataRef, SourceRef: recording}}}
}
func writeAudio(_ context.Context, _ discovery.Ref, path string) error {
	return os.WriteFile(path, []byte("prepared audio"), 0600)
}
func wait(t *testing.T, s *ingest.Service, owner, id, state string) ingest.Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		j, err := s.Get(context.Background(), owner, id)
		if err != nil {
			t.Fatal(err)
		}
		if j.State == state {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	j, _ := s.Get(context.Background(), owner, id)
	t.Fatalf("wanted %s, got %+v", state, j)
	return j
}
func create(t *testing.T, s *ingest.Service, owner string, r ingest.Request) ingest.Job {
	t.Helper()
	j, err := s.Create(context.Background(), owner, r)
	if err != nil {
		t.Fatal(err)
	}
	return j
}
func TestPublicationIdempotencyDeduplicationAndPrivacy(t *testing.T) {
	var calls atomic.Int32
	f := setup(t, func(ctx context.Context, r discovery.Ref, path string) error {
		calls.Add(1)
		return writeAudio(ctx, r, path)
	}, metadata{}, nil)
	j := create(t, f.s, "alice", request("first"))
	j = wait(t, f.s, "alice", j.ID, ingest.Completed)
	if len(j.Items) != 1 || j.Items[0].TrackID == "" {
		t.Fatal(j)
	}
	t1, err := f.lib.Get(context.Background(), j.Items[0].TrackID)
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(filepath.Join(f.root.Path, t1.Path))
	if err != nil || string(bytes) != "prepared audio" {
		t.Fatal(string(bytes), err)
	}
	replay := create(t, f.s, "alice", request("first"))
	if replay.ID != j.ID {
		t.Fatal("idempotent request created a second job")
	}
	changed := request("first")
	changed.Items[0].MetadataRef.ID = "456"
	if _, err = f.s.Create(context.Background(), "alice", changed); !errors.Is(err, ingest.ErrConflict) {
		t.Fatal(err)
	}
	if _, err = f.s.Get(context.Background(), "bob", j.ID); !errors.Is(err, ingest.ErrNotFound) {
		t.Fatal("job leaked", err)
	}
	if _, err = f.s.Cancel(context.Background(), "bob", j.ID); !errors.Is(err, ingest.ErrNotFound) {
		t.Fatal(err)
	}
	reused := create(t, f.s, "bob", request("different-user"))
	reused = wait(t, f.s, "bob", reused.ID, ingest.Completed)
	if !reused.Items[0].Reused || reused.Items[0].TrackID != t1.ID || calls.Load() != 1 {
		t.Fatal(reused, calls.Load())
	}
	page, err := f.lib.List(context.Background(), library.Query{})
	if err != nil || page.Total != 1 {
		t.Fatal(page, err)
	}
	rev, err := f.lib.Revision(context.Background())
	if err != nil || rev <= 0 {
		t.Fatal(rev, err)
	}
	jobs, total, err := f.s.List(context.Background(), "alice", 20, 0)
	if err != nil || total != 1 || len(jobs) != 1 {
		t.Fatal(jobs, total, err)
	}
}
func TestAmbiguousMatchRequiresSelectionAndRetry(t *testing.T) {
	candidates := []discovery.Candidate{{Ref: recording, Score: .95, Suitable: true}, {Ref: discovery.Ref{Provider: "youtube", ID: "abcdefghijk"}, Score: .94, Suitable: true}}
	f := setup(t, writeAudio, metadata{}, candidates)
	r := request("ambiguous")
	r.Items[0].SourceRef = discovery.Ref{}
	j := create(t, f.s, "alice", r)
	j = wait(t, f.s, "alice", j.ID, ingest.NeedsSelection)
	if j.Items[0].TrackID != "" {
		t.Fatal(j)
	}
	if _, err := f.s.Retry(context.Background(), "alice", j.ID, []ingest.Override{{Index: 0, SourceRef: recording}}); err != nil {
		t.Fatal(err)
	}
	wait(t, f.s, "alice", j.ID, ingest.Completed)
}

type unavailableRanker struct{ calls *atomic.Int32 }

func (r unavailableRanker) Rank(context.Context, discovery.Track, []discovery.Candidate) ([]discovery.Candidate, error) {
	r.calls.Add(1)
	return nil, errors.New("ranking provider unavailable")
}

func TestRankingOutagePausesAndExplicitRetryBypassesRanking(t *testing.T) {
	var downloads, rankings atomic.Int32
	candidates := []discovery.Candidate{{Ref: recording, Score: 1, Suitable: true}}
	f := setup(t, func(ctx context.Context, ref discovery.Ref, path string) error {
		downloads.Add(1)
		if ref != recording {
			t.Error("explicit source was not honored")
		}
		return writeAudio(ctx, ref, path)
	}, metadata{}, candidates)
	f.s.Close()
	f.s = nil
	f.deps.Discovery = discovery.New(metadata{}, sources{candidates}, discovery.WithRanking(unavailableRanker{&rankings}, discovery.SelectionPolicy{MinimumScore: .9, MinimumMargin: .03}))
	f.start(t)
	r := request("ranking-outage")
	r.Items[0].SourceRef = discovery.Ref{}
	j, err := f.s.Create(context.Background(), "alice", r)
	if err != nil {
		t.Fatal(err)
	}
	wait(t, f.s, "alice", j.ID, ingest.NeedsSelection)
	if downloads.Load() != 0 || rankings.Load() != 1 {
		t.Fatal("outage did not pause automatic import")
	}
	if _, err := f.s.Retry(context.Background(), "alice", j.ID, []ingest.Override{{Index: 0, SourceRef: recording}}); err != nil {
		t.Fatal(err)
	}
	wait(t, f.s, "alice", j.ID, ingest.Completed)
	if downloads.Load() != 1 || rankings.Load() != 1 {
		t.Fatal("explicit retry contacted ranking provider")
	}
}
func TestCancelStopsWorkAndKeepsCompletedItems(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	var calls atomic.Int32
	f := setup(t, func(ctx context.Context, r discovery.Ref, path string) error {
		if calls.Add(1) == 1 {
			return writeAudio(ctx, r, path)
		}
		close(started)
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	}, metadata{}, nil)
	r := request("cancel")
	r.Items = append(r.Items, ingest.Selection{MetadataRef: discovery.Ref{Provider: "itunes", ID: "456"}, SourceRef: recording})
	j := create(t, f.s, "alice", r)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("download not started")
	}
	cancelled, err := f.s.Cancel(context.Background(), "alice", j.ID)
	if err != nil || cancelled.State != ingest.Cancelled {
		t.Fatal(cancelled, err)
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("download did not stop")
	}
	page, err := f.lib.List(context.Background(), library.Query{})
	if err != nil || page.Total != 1 {
		t.Fatal(page, err)
	}
	if _, err = f.s.Cancel(context.Background(), "bob", j.ID); !errors.Is(err, ingest.ErrNotFound) {
		t.Fatal(err)
	}
}
func TestRestartResumesAnInterruptedDownloadWithoutScanning(t *testing.T) {
	started := make(chan struct{})
	f := setup(t, func(ctx context.Context, _ discovery.Ref, path string) error {
		os.WriteFile(path, []byte("partial"), 0600)
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}, metadata{}, nil)
	if err := os.WriteFile(filepath.Join(f.root.Path, "unmanaged.mp3"), []byte("do not ingest"), 0600); err != nil {
		t.Fatal(err)
	}
	j := create(t, f.s, "alice", request("restart"))
	<-started
	f.s.Close()
	f.s = nil
	f.deps.Downloader = downloader(writeAudio)
	f.start(t)
	j = wait(t, f.s, "alice", j.ID, ingest.Completed)
	page, err := f.lib.List(context.Background(), library.Query{})
	if err != nil || page.Total != 1 || page.Tracks[0].ID != j.Items[0].TrackID {
		t.Fatal(page, err)
	}
}

type interruptedPublication struct {
	ingest.Store
	reached chan struct{}
}

func (s interruptedPublication) Publish(ctx context.Context, _ ingest.Root, _ string, _ int, _ ingest.Prepared) (library.Track, error) {
	close(s.reached)
	<-ctx.Done()
	return library.Track{}, ctx.Err()
}
func TestRestartFinalizesAnAlreadyPublishedFile(t *testing.T) {
	var downloads, preparations atomic.Int32
	f := setup(t, func(ctx context.Context, r discovery.Ref, path string) error {
		downloads.Add(1)
		return writeAudio(ctx, r, path)
	}, metadata{}, nil)
	f.s.Close()
	f.s = nil
	reached := make(chan struct{})
	f.deps.Store = interruptedPublication{Store: f.store, reached: reached}
	f.deps.Media = media{calls: &preparations}
	f.start(t)
	j := create(t, f.s, "alice", request("finalize"))
	<-reached
	f.s.Close()
	f.s = nil
	f.deps.Store = f.store
	f.start(t)
	wait(t, f.s, "alice", j.ID, ingest.Completed)
	if downloads.Load() != 1 || preparations.Load() != 1 {
		t.Fatal("recovery repeated acquisition", downloads.Load(), preparations.Load())
	}
}
func TestPublicationCannotEscapeTheRoot(t *testing.T) {
	f := setup(t, writeAudio, metadata{}, nil)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(f.root.Path, "Artist")); err != nil {
		t.Fatal(err)
	}
	j := create(t, f.s, "alice", request("escape"))
	wait(t, f.s, "alice", j.ID, ingest.Failed)
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
	page, err := f.lib.List(context.Background(), library.Query{})
	if err != nil || page.Total != 0 {
		t.Fatal(page, err)
	}
}

type lostPlaylistReceipt struct {
	*userdata.Service
	calls atomic.Int32
}

func (p *lostPlaylistReceipt) AppendImport(ctx context.Context, owner, id, job string, ids []string) error {
	if err := p.Service.AppendImport(ctx, owner, id, job, ids); err != nil {
		return err
	}
	if p.calls.Add(1) == 1 {
		return errors.New("lost completion acknowledgement")
	}
	return nil
}
func TestReleaseOrderAndPlaylistReceiptSurviveRetry(t *testing.T) {
	first, _ := (metadata{}).Track(context.Background(), metadataRef)
	second := first
	second.Ref.ID = "456"
	second.Title = "Second"
	f := setup(t, writeAudio, metadata{release: []discovery.Track{first, second}}, []discovery.Candidate{{Ref: recording, Score: .99, Suitable: true}})
	p, err := f.data.CreatePlaylist(context.Background(), "alice", "Album", nil)
	if err != nil {
		t.Fatal(err)
	}
	f.s.Close()
	f.s = nil
	f.deps.Playlists = &lostPlaylistReceipt{Service: f.data}
	f.start(t)
	j := create(t, f.s, "alice", ingest.Request{RequestID: "release", ReleaseRef: metadataRef, PlaylistID: p.ID})
	j = wait(t, f.s, "alice", j.ID, ingest.Partial)
	if _, err = f.s.Retry(context.Background(), "alice", j.ID, nil); err != nil {
		t.Fatal(err)
	}
	j = wait(t, f.s, "alice", j.ID, ingest.Completed)
	p, err = f.data.Playlist(context.Background(), "alice", p.ID)
	if err != nil || len(p.IDs) != 2 || p.IDs[0] != j.Items[0].TrackID || p.IDs[1] != j.Items[1].TrackID {
		t.Fatal(p, err)
	}
	if _, err = f.s.Create(context.Background(), "bob", ingest.Request{RequestID: "forbidden", ReleaseRef: metadataRef, PlaylistID: p.ID}); !errors.Is(err, userdata.ErrForbidden) {
		t.Fatal(err)
	}
}
func TestSubmissionContextDoesNotOwnTheDownload(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan struct{})
	f := setup(t, func(ctx context.Context, r discovery.Ref, path string) error {
		close(started)
		select {
		case <-gate:
			return writeAudio(ctx, r, path)
		case <-ctx.Done():
			return ctx.Err()
		}
	}, metadata{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	j, err := f.s.Create(ctx, "alice", request("detached"))
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	<-started
	close(gate)
	wait(t, f.s, "alice", j.ID, ingest.Completed)
}

func TestRestartCleansCancellationRecordedBeforeShutdown(t *testing.T) {
	f := setup(t, writeAudio, metadata{}, nil)
	f.s.Close()
	f.s = nil
	reached := make(chan struct{})
	f.deps.Store = interruptedPublication{Store: f.store, reached: reached}
	f.start(t)
	j := create(t, f.s, "alice", request("cancel-before-crash"))
	<-reached
	f.s.Close()
	f.s = nil
	j, err := f.store.Update(context.Background(), j.ID, func(j *ingest.Job) error {
		j.State = ingest.Cancelled
		j.Items[0].State = ingest.Cancelled
		j.Cleaned = false
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.root.Path, j.Items[0].Prepared.Track.Path)
	if _, err = os.Stat(path); err != nil {
		t.Fatal("prepared file never reached publication boundary", err)
	}
	f.deps.Store = f.store
	f.start(t)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		current, err := f.s.Get(context.Background(), "alice", j.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Cleaned {
			if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("unpublished cancelled file remains", err)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("cancellation cleanup was not recovered")
}
