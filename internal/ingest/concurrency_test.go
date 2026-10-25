package ingest_test

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prayujt/wavelength/internal/discovery"
	"github.com/prayujt/wavelength/internal/ingest"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/telemetry"
)

func parallel(t *testing.T, download downloader, concurrency int) *fixture {
	t.Helper()
	f := setup(t, download, metadata{}, nil)
	f.s.Close()
	f.s = nil
	f.deps.Concurrency = concurrency
	f.deps.Metrics = telemetry.New(concurrency)
	f.start(t)
	return f
}
func started(t *testing.T, ch <-chan discovery.Ref) discovery.Ref {
	t.Helper()
	select {
	case ref := <-ch:
		return ref
	case <-time.After(3 * time.Second):
		t.Fatal("parallel item never started")
		return discovery.Ref{}
	}
}
func selection(id string) ingest.Selection {
	return ingest.Selection{MetadataRef: discovery.Ref{Provider: "itunes", ID: id}, SourceRef: recording}
}

func TestParallelItemsRespectLimitAndPlaylistOrder(t *testing.T) {
	begin := make(chan discovery.Ref, 3)
	release := make(chan struct{})
	var active, peak atomic.Int32
	f := parallel(t, func(ctx context.Context, ref discovery.Ref, path string) error {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		begin <- ref
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return writeAudio(ctx, ref, path)
		}
	}, 2)
	p, err := f.data.CreatePlaylist(context.Background(), "alice", "ordered", nil)
	if err != nil {
		t.Fatal(err)
	}
	j, err := f.s.Create(context.Background(), "alice", ingest.Request{RequestID: "parallel-items", Items: []ingest.Selection{selection("123"), selection("456"), selection("789")}, PlaylistID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	started(t, begin)
	started(t, begin)
	if active.Load() != 2 {
		t.Fatal("items were not concurrent")
	}
	select {
	case <-begin:
		t.Fatal("concurrency limit exceeded")
	default:
	}
	close(release)
	j = wait(t, f.s, "alice", j.ID, ingest.Completed)
	if peak.Load() != 2 {
		t.Fatal("unexpected peak concurrency", peak.Load())
	}
	p, err = f.data.Playlist(context.Background(), "alice", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{j.Items[0].TrackID, j.Items[1].TrackID, j.Items[2].TrackID}
	if !reflect.DeepEqual(p.IDs, want) {
		t.Fatal("parallel completion reordered playlist", p.IDs, want)
	}
	stats, err := f.store.QueueStats(context.Background(), f.root.ID)
	if err != nil || stats.Jobs[ingest.Completed] != 1 || stats.Items[ingest.Completed] != 3 || stats.LibraryTracks != 3 || stats.LibraryBytes <= 0 {
		t.Fatal("committed queue metrics disagree with catalog", stats, err)
	}
}

func TestParallelJobsAndCancellationAreIndependent(t *testing.T) {
	begin := make(chan discovery.Ref, 2)
	release := make(chan struct{})
	f := parallel(t, func(ctx context.Context, ref discovery.Ref, path string) error {
		begin <- ref
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return writeAudio(ctx, ref, path)
		}
	}, 2)
	a, err := f.s.Create(context.Background(), "alice", ingest.Request{RequestID: "job-a", Items: []ingest.Selection{selection("123")}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.s.Create(context.Background(), "alice", ingest.Request{RequestID: "job-b", Items: []ingest.Selection{selection("456")}})
	if err != nil {
		t.Fatal(err)
	}
	started(t, begin)
	started(t, begin)
	if _, err = f.s.Cancel(context.Background(), "alice", a.ID); err != nil {
		t.Fatal(err)
	}
	if b, err = f.s.Get(context.Background(), "alice", b.ID); err != nil || b.State != ingest.Running {
		t.Fatal("cancellation affected another job", b.State, err)
	}
	close(release)
	wait(t, f.s, "alice", b.ID, ingest.Completed)
	a, err = f.s.Get(context.Background(), "alice", a.ID)
	if err != nil || a.State != ingest.Cancelled {
		t.Fatal(a.State, err)
	}
}

func TestConcurrentDuplicateReferencesDownloadOnce(t *testing.T) {
	begin := make(chan discovery.Ref, 2)
	release := make(chan struct{})
	var downloads atomic.Int32
	f := parallel(t, func(ctx context.Context, ref discovery.Ref, path string) error {
		downloads.Add(1)
		begin <- ref
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return writeAudio(ctx, ref, path)
		}
	}, 2)
	a, err := f.s.Create(context.Background(), "alice", request("duplicate-a"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.s.Create(context.Background(), "bob", request("duplicate-b"))
	if err != nil {
		t.Fatal(err)
	}
	started(t, begin)
	wait(t, f.s, "bob", b.ID, ingest.Running)
	close(release)
	a = wait(t, f.s, "alice", a.ID, ingest.Completed)
	b = wait(t, f.s, "bob", b.ID, ingest.Completed)
	if downloads.Load() != 1 || a.Items[0].TrackID != b.Items[0].TrackID {
		t.Fatal("duplicate network work or catalog identity", downloads.Load())
	}
	page, err := f.lib.List(context.Background(), library.Query{})
	if err != nil || page.Total != 1 {
		t.Fatal("duplicate catalog rows", page.Total, err)
	}
}

func TestShutdownResumesMultipleActiveItems(t *testing.T) {
	begin := make(chan discovery.Ref, 2)
	f := parallel(t, func(ctx context.Context, ref discovery.Ref, path string) error {
		begin <- ref
		<-ctx.Done()
		return ctx.Err()
	}, 2)
	j, err := f.s.Create(context.Background(), "alice", ingest.Request{RequestID: "parallel-restart", Items: []ingest.Selection{selection("123"), selection("456")}})
	if err != nil {
		t.Fatal(err)
	}
	started(t, begin)
	started(t, begin)
	f.s.Close()
	f.s = nil
	f.deps.Downloader = downloader(writeAudio)
	f.start(t)
	j = wait(t, f.s, "alice", j.ID, ingest.Completed)
	if j.Items[0].TrackID == "" || j.Items[1].TrackID == "" {
		t.Fatal("interrupted work was not resumed")
	}
	if _, err = f.s.Get(context.Background(), "bob", j.ID); !errors.Is(err, ingest.ErrNotFound) {
		t.Fatal("parallelism changed ownership", err)
	}
}
