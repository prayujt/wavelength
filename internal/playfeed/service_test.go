package playfeed_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prayujt/wavelength/internal/database"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/listening"
	"github.com/prayujt/wavelength/internal/playfeed"
	"github.com/prayujt/wavelength/internal/testutil"
)

type ranker struct {
	calls   atomic.Int32
	fail    bool
	invalid bool
	seen    chan playfeed.Context
}

func (r *ranker) Recommend(ctx context.Context, c playfeed.Context, tracks []library.Track) ([]playfeed.Score, error) {
	r.calls.Add(1)
	if r.seen != nil {
		select {
		case r.seen <- c:
		default:
		}
	}
	if r.fail {
		return nil, errors.New("provider offline")
	}
	out := make([]playfeed.Score, len(tracks))
	for i, t := range tracks {
		out[i] = playfeed.Score{TrackID: t.ID, Value: float64(t.TrackNumber) / 100}
	}
	if r.invalid && len(out) > 0 {
		out[0].TrackID = "not-a-candidate"
	}
	return out, nil
}

type fixture struct {
	service *playfeed.Service
	lib     *library.Library
	store   database.PlayFeedStore
	history database.ListeningStore
	tracks  []library.Track
	root    string
}

func setup(t *testing.T, r playfeed.Ranker, n int) fixture {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	music := t.TempDir()
	for i := 0; i < n; i++ {
		name := filepath.Join(music, time.Unix(int64(i), 0).UTC().Format("150405")+".mp3")
		if err := os.WriteFile(name, []byte("fixture audio"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	testutil.SeedCatalog(t, db, music)
	lib, err := library.Open(music, database.Catalog{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	page, err := lib.List(ctx, library.Query{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	for i, track := range page.Tracks {
		if _, err := db.Exec("UPDATE tracks SET artist=?,album=?,genre='Folk',duration=180,track_number=? WHERE id=?", []string{"Artist A", "Artist B", "Artist C"}[i%3], "Album", i+1, track.ID); err != nil {
			t.Fatal(err)
		}
	}
	page, err = lib.List(ctx, library.Query{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	root, _ := lib.SourceRoot()
	store := database.PlayFeedStore{DB: db}
	history := database.ListeningStore{DB: db}
	service := playfeed.New(ctx, lib, store, history, r, nil)
	t.Cleanup(func() { cancel(); lib.Close(); db.Close() })
	return fixture{service, lib, store, history, page.Tracks, root}
}
func get(t *testing.T, f fixture, owner string, in playfeed.Request) playfeed.Page {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		p, err := f.service.Get(context.Background(), owner, in)
		if errors.Is(err, playfeed.ErrCapacity) && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
}
func TestPaginationReplayPersistenceAndOwnerIsolation(t *testing.T) {
	r := &ranker{}
	f := setup(t, r, 38)
	in := playfeed.Request{RequestID: "page-first", PageSize: 10, SeedTrackID: f.tracks[0].ID, QueuedTrackIDs: []string{f.tracks[1].ID}, ExcludedTrackIDs: []string{f.tracks[2].ID}}
	first := get(t, f, "alice", in)
	if len(first.Items) != 3 || first.Exhausted || first.NextCursor == "" {
		t.Fatal(first)
	}
	replay := get(t, f, "alice", in)
	if !reflect.DeepEqual(first, replay) || r.calls.Load() != 1 {
		t.Fatal("retry changed the page or called ranking twice")
	}
	altered := in
	altered.PageSize = 2
	if _, err := f.service.Get(context.Background(), "alice", altered); !errors.Is(err, playfeed.ErrConflict) {
		t.Fatal("request ID did not bind input", err)
	}
	if _, err := f.service.Get(context.Background(), "bob", playfeed.Request{RequestID: "wrong-owner", Cursor: first.NextCursor}); !errors.Is(err, playfeed.ErrNotFound) {
		t.Fatal("cursor crossed owners", err)
	}
	seen := map[string]bool{f.tracks[0].ID: true, f.tracks[1].ID: true, f.tracks[2].ID: true}
	page := first
	for step := 0; ; step++ {
		for _, item := range page.Items {
			if seen[item.Track.ID] || item.ID == "" {
				t.Fatal("duplicate/excluded item", item)
			}
			seen[item.Track.ID] = true
		}
		if page.Exhausted {
			break
		}
		in.Cursor = page.NextCursor
		in.RequestID = "page-next-" + time.Unix(int64(step), 0).UTC().Format("150405")
		page = get(t, f, "alice", in)
		if step == 0 {
			again := in
			again.RequestID = "same-cursor-new-request"
			same := get(t, f, "alice", again)
			if !reflect.DeepEqual(page, same) {
				t.Fatal("an issued cursor page changed")
			}
		}
	}
	if len(seen) != 38 {
		t.Fatalf("pagination omitted library records: %d", len(seen))
	}
	// Request receipts survive a process restart, including their stable item IDs.
	reopened := f
	reopened.service = playfeed.New(context.Background(), f.lib, f.store, f.history, r, nil)
	if got := get(t, reopened, "alice", playfeed.Request{RequestID: "page-first", PageSize: 10, SeedTrackID: f.tracks[0].ID, QueuedTrackIDs: []string{f.tracks[1].ID}, ExcludedTrackIDs: []string{f.tracks[2].ID}}); !reflect.DeepEqual(got, first) {
		t.Fatal("restart lost replay receipt")
	}
}
func TestExplicitEarlySkipChangesUnissuedPagesAndAttributionIsOwned(t *testing.T) {
	r := &ranker{seen: make(chan playfeed.Context, 5)}
	f := setup(t, r, 30)
	first := get(t, f, "alice", playfeed.Request{RequestID: "feedback-first", PageSize: 2})
	selected := first.Items[0]
	// A same-account recommendation is attributed; another account cannot claim it.
	alice, err := f.history.Create(context.Background(), listening.Session{ID: "listen-alice", Owner: "alice", RootID: f.root, Track: selected.Track, PlayFeedItemID: selected.ID, StartedAt: time.Now().Add(-time.Minute), Outcome: "skipped", ChangeReason: "next", ListenedMS: 1000})
	if err != nil || alice.PlayFeedItemID != selected.ID {
		t.Fatal(alice, err)
	}
	bob, err := f.history.Create(context.Background(), listening.Session{ID: "listen-bob", Owner: "bob", RootID: f.root, Track: selected.Track, PlayFeedItemID: selected.ID, StartedAt: time.Now()})
	if err != nil || bob.PlayFeedItemID != "" {
		t.Fatal("cross-account attribution", bob, err)
	}
	fresh := get(t, f, "alice", playfeed.Request{RequestID: "feedback-refresh", PageSize: 25})
	for _, item := range fresh.Items {
		if item.Track.ID == selected.Track.ID {
			t.Fatal("fresh feed repeated an intentional early skip")
		}
	}
	if r.calls.Load() != 2 {
		t.Fatal("history did not invalidate the ranking context", r.calls.Load())
	}
	var latest playfeed.Context
	for len(r.seen) > 0 {
		latest = <-r.seen
	}
	if len(latest.Preferences) != 1 || latest.Preferences[0].EarlySkips != 1 || latest.Preferences[0].Track.Path != "" {
		t.Fatal(latest)
	}
	// An old offline receipt does not attribute forever, even before feed cleanup runs.
	if _, err := f.store.DB.Exec("UPDATE play_feed_items SET created_at=0 WHERE id=?", selected.ID); err != nil {
		t.Fatal(err)
	}
	expired, err := f.history.Create(context.Background(), listening.Session{ID: "expired-attribution", Owner: "alice", RootID: f.root, Track: selected.Track, PlayFeedItemID: selected.ID, StartedAt: time.Now()})
	if err != nil || expired.PlayFeedItemID != "" {
		t.Fatal("expired receipt retained attribution", expired, err)
	}
	// Stopping or abandoning a session is not a negative listening preference.
	if _, err := f.history.Create(context.Background(), listening.Session{ID: "listen-stopped", Owner: "alice", RootID: f.root, Track: f.tracks[1], Outcome: "stopped", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
}
func TestFallbackDeletedTracksExpiryAndInputLimits(t *testing.T) {
	for _, r := range []*ranker{{fail: true}, {invalid: true}} {
		f := setup(t, r, 6)
		p := get(t, f, "alice", playfeed.Request{RequestID: "fallback-first", PageSize: 2})
		if len(p.Items) != 2 {
			t.Fatal("provider failure blocked local recommendations")
		}
		if err := f.lib.Delete(context.Background(), p.Items[0].Track.ID); err != nil {
			t.Fatal(err)
		}
		replay := get(t, f, "alice", playfeed.Request{RequestID: "fallback-first", PageSize: 2})
		if len(replay.Items) != 1 {
			t.Fatal("a replay exposed a deleted catalog track", replay)
		}
		if _, err := f.store.DB.Exec("UPDATE play_feeds SET touched_at=0"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.service.Get(context.Background(), "alice", playfeed.Request{RequestID: "expired-cursor", Cursor: p.NextCursor}); !errors.Is(err, playfeed.ErrNotFound) {
			t.Fatal("expired cursor accepted", err)
		}
		for _, in := range []playfeed.Request{{RequestID: "short"}, {RequestID: "invalid-size", PageSize: 26}, {RequestID: "negative-size", PageSize: -1}, {RequestID: "invalid-exclusion", ExcludedTrackIDs: []string{""}}} {
			if _, err := f.service.Get(context.Background(), "alice", in); !errors.Is(err, playfeed.ErrInvalid) {
				t.Fatal(in, err)
			}
		}
	}
	f := setup(t, nil, 0)
	p := get(t, f, "alice", playfeed.Request{RequestID: "empty-catalog"})
	if !p.Exhausted || p.NextCursor != "" || len(p.Items) != 0 {
		t.Fatal(p)
	}
}
func TestConcurrentRetriesShareOnePageAndRanking(t *testing.T) {
	r := &ranker{}
	f := setup(t, r, 20)
	var workers sync.WaitGroup
	responses := make(chan playfeed.Page, 12)
	failures := make(chan error, 12)
	for range 12 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			p, err := f.service.Get(context.Background(), "alice", playfeed.Request{RequestID: "concurrent-request"})
			responses <- p
			failures <- err
		}()
	}
	workers.Wait()
	close(responses)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first *playfeed.Page
	for response := range responses {
		if first == nil {
			first = &response
		} else if !reflect.DeepEqual(*first, response) {
			t.Fatal("concurrent retry changed response")
		}
	}
	if r.calls.Load() != 1 {
		t.Fatal("duplicate provider requests", r.calls.Load())
	}
}

func TestRollingPagesRerankWithCurrentListeningProgress(t *testing.T) {
	r := &ranker{seen: make(chan playfeed.Context, 4)}
	f := setup(t, r, 15)
	now := time.Now().UTC()
	_, err := f.history.Create(context.Background(), listening.Session{
		ID: "active-listen", Owner: "alice", RootID: f.root, Track: f.tracks[0],
		StartedAt: now.Add(-2 * time.Minute), UpdatedAt: now, Playing: true,
		ListenedMS: 110000, PositionMS: 125000,
	})
	if err != nil {
		t.Fatal(err)
	}
	first := get(t, f, "alice", playfeed.Request{RequestID: "rolling-first", SeedTrackID: f.tracks[0].ID})
	if len(first.Items) != 3 {
		t.Fatalf("expected a short first batch, got %d", len(first.Items))
	}
	_, err = f.history.Update(context.Background(), "alice", f.root, "active-listen", func(s *listening.Session) error {
		s.ListenedMS, s.PositionMS, s.UpdatedAt = 150000, 165000, now.Add(40*time.Second)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	next := get(t, f, "alice", playfeed.Request{RequestID: "rolling-next", Cursor: first.NextCursor, PageSize: 2, SeedTrackID: f.tracks[0].ID})
	if len(next.Items) != 2 || r.calls.Load() != 2 {
		t.Fatal("continuation did not request a fresh short ranking", r.calls.Load())
	}
	<-r.seen
	state := <-r.seen
	if state.AsOf.IsZero() || len(state.Recent) != 1 || state.Recent[0].ListenedMS != 150000 || state.Recent[0].PositionMS != 165000 || !state.Recent[0].Playing || state.Recent[0].Track.Path != "" || !state.Recent[0].StartedAt.Equal(now.Add(-2*time.Minute)) {
		t.Fatal("current listening time, position or timestamps missing from ranking context", state)
	}
	for _, item := range next.Items {
		for _, previous := range first.Items {
			if item.Track.ID == previous.Track.ID {
				t.Fatal("continuation repeated a prior suggestion")
			}
		}
	}
}

func TestCanonicalRetriesAndRequestRateLimit(t *testing.T) {
	f := setup(t, nil, 10)
	first := get(t, f, "alice", playfeed.Request{RequestID: "canonical-request"})
	retry := get(t, f, "alice", playfeed.Request{RequestID: "canonical-request", PageSize: 10, QueuedTrackIDs: []string{}, ExcludedTrackIDs: []string{}})
	if !reflect.DeepEqual(first, retry) {
		t.Fatal("empty/default protobuf inputs changed the receipt")
	}
	for i := 0; i < 3; i++ {
		get(t, f, "alice", playfeed.Request{RequestID: "burst-request-" + string(rune('a'+i))})
	}
	if _, err := f.service.Get(context.Background(), "alice", playfeed.Request{RequestID: "over-capacity"}); !errors.Is(err, playfeed.ErrCapacity) {
		t.Fatal("request limiter did not bound work", err)
	}
	// Saved retries still work without consuming another rank request.
	if _, err := f.service.Get(context.Background(), "alice", playfeed.Request{RequestID: "canonical-request"}); err != nil {
		t.Fatal(err)
	}
}
