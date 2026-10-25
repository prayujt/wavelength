package library_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/prayujt/wavelength/internal/database"
	"github.com/prayujt/wavelength/internal/discovery"
	"github.com/prayujt/wavelength/internal/ingest"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/listening"
	"github.com/prayujt/wavelength/internal/sharing"
	"github.com/prayujt/wavelength/internal/state"
	"github.com/prayujt/wavelength/internal/testutil"
	"github.com/prayujt/wavelength/internal/userdata"
)

type deletionFixture struct {
	ctx         context.Context
	db          *sql.DB
	lib         *library.Library
	music, root string
	tracks      map[string]library.Track
}

func deletionLibrary(t *testing.T) deletionFixture {
	t.Helper()
	ctx := context.Background()
	music := t.TempDir()
	for _, path := range []string{"Artist/Album/one.mp3", "Artist/Album/two.mp3", "Other/Record/keep.mp3"} {
		full := filepath.Join(music, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("saved audio"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	testutil.SeedCatalog(t, db, music)
	lib, err := library.Open(music, database.Catalog{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lib.Close() })
	page, err := lib.List(ctx, library.Query{})
	if err != nil {
		t.Fatal(err)
	}
	tracks := map[string]library.Track{}
	for _, track := range page.Tracks {
		tracks[filepath.Base(track.Path)] = track
	}
	root, _ := lib.SourceRoot()
	return deletionFixture{ctx, db, lib, music, root, tracks}
}
func (f deletionFixture) exists(t *testing.T, path string, want bool) {
	t.Helper()
	_, err := os.Lstat(filepath.Join(f.music, path))
	if (err == nil) != want {
		t.Fatalf("file %s existence=%v, want %v", path, err, want)
	}
}

func TestDeleteRemovesFilesOriginsAndEveryListenersReferences(t *testing.T) {
	f := deletionLibrary(t)
	one, two := f.tracks["one.mp3"], f.tracks["two.mp3"]
	for _, path := range []string{"Artist/Album/one.lrc", "Artist/Album/one.txt", "Artist/Album/cover.jpg", "Artist/Album/readme.txt"} {
		if err := os.WriteFile(filepath.Join(f.music, path), []byte("sidecar"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	_, err := f.db.Exec("INSERT INTO track_origins VALUES(?,?,?,?,?,?,?)", f.root, "itunes", "123", one.ID, "youtube", "source", "checksum")
	if err != nil {
		t.Fatal(err)
	}
	data := userdata.New(database.StateStore{DB: f.db}, f.lib)
	for _, owner := range []string{"oidc:alice", "oidc:bob"} {
		if _, err := data.CreatePlaylist(f.ctx, owner, owner, []string{one.ID, two.ID, one.ID}); err != nil {
			t.Fatal(err)
		}
		if err := data.SaveQueue(f.ctx, owner, userdata.Queue{IDs: []string{one.ID, two.ID, one.ID}, Current: one.ID, Position: 123}); err != nil {
			t.Fatal(err)
		}
		if err := data.SaveBookmark(f.ctx, owner, userdata.Bookmark{ID: one.ID}); err != nil {
			t.Fatal(err)
		}
		if err := data.RecordPlay(f.ctx, owner, "web", one.ID, time.Now(), true); err != nil {
			t.Fatal(err)
		}
	}
	grants := database.GrantStore{DB: f.db}
	for _, g := range []sharing.Grant{{ID: "parent", TokenHash: "parent-hash", RootID: f.root, Owner: "alice", Kind: "share", TrackIDs: []string{one.ID, two.ID}}, {ID: "child", TokenHash: "child-hash", RootID: f.root, Owner: "alice", Kind: "playback", ParentID: "parent", TrackIDs: []string{two.ID}}} {
		if err := grants.Create(f.ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	_, err = (database.ListeningStore{DB: f.db}).Create(f.ctx, listening.Session{ID: "listen", RootID: f.root, Owner: "alice", Track: one})
	if err != nil {
		t.Fatal(err)
	}
	jobs := database.IngestStore{DB: f.db}
	job := ingest.Job{ID: "historical", RootID: f.root, Owner: "alice", State: ingest.Completed, Expanded: true, Request: ingest.Request{RequestID: "history"}, Items: []ingest.Item{{State: ingest.Completed, TrackID: one.ID, Prepared: &ingest.Prepared{Track: one}}}}
	if _, err := jobs.Create(f.ctx, job); err != nil {
		t.Fatal(err)
	}
	revision, _ := f.lib.Revision(f.ctx)
	if err := f.lib.Delete(f.ctx, one.ID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{one.Path, "Artist/Album/one.lrc", "Artist/Album/one.txt"} {
		f.exists(t, path, false)
	}
	for _, path := range []string{two.Path, "Artist/Album/cover.jpg", "Artist/Album/readme.txt", "Other/Record/keep.mp3"} {
		f.exists(t, path, true)
	}
	if _, err := f.lib.Get(f.ctx, one.ID); !errors.Is(err, library.ErrNotFound) {
		t.Fatal(err)
	}
	after, _ := f.lib.Revision(f.ctx)
	if after <= revision {
		t.Fatal("catalog revision did not advance")
	}
	for _, owner := range []string{"oidc:alice", "oidc:bob"} {
		ps, err := data.Playlists(f.ctx, owner, "", false)
		if err != nil || len(ps) != 1 || !reflect.DeepEqual(ps[0].IDs, []string{two.ID}) {
			t.Fatal(ps, err)
		}
		q, err := data.Queue(f.ctx, owner)
		if err != nil || !reflect.DeepEqual(q.IDs, []string{two.ID}) || q.Current != "" || q.Position != 0 {
			t.Fatal(q, err)
		}
		b, err := data.Bookmarks(f.ctx, owner)
		if err != nil || len(b) != 0 {
			t.Fatal(b, err)
		}
		h, err := data.History(f.ctx, owner)
		if err != nil || len(h) != 0 {
			t.Fatal(h, err)
		}
	}
	playing, err := data.NowPlaying(f.ctx)
	if err != nil || len(playing) != 0 {
		t.Fatal(playing, err)
	}
	for _, table := range []string{"track_origins", "media_grants", "listening_sessions"} {
		var n int
		if err := f.db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatal(table, n, err)
		}
	}
	job, err = jobs.Get(f.ctx, f.root, "alice", job.ID)
	if err != nil || job.Items[0].TrackID != "" || job.Items[0].Prepared != nil || job.Items[0].State != "deleted" {
		t.Fatal(job, err)
	}
	if _, err := jobs.Lookup(f.ctx, f.root, discovery.Ref{Provider: "itunes", ID: "123"}); !errors.Is(err, ingest.ErrNotFound) {
		t.Fatal(err)
	}
	// Stale requests cannot restore references or access links after committing deletion.
	raw := userdata.Playlist{ID: "stale", IDs: []string{one.ID}}
	if err := state.Write(f.ctx, database.StateStore{DB: f.db}, "playlists", "stale", raw); !errors.Is(err, library.ErrNotFound) {
		t.Fatal("stale write accepted", err)
	}
	if err := grants.Create(f.ctx, sharing.Grant{ID: "stale", RootID: f.root, Kind: "playback", TrackIDs: []string{one.ID}}); !errors.Is(err, library.ErrNotFound) {
		t.Fatal("stale link accepted", err)
	}
	// An idempotent retry must not unlink a newly created unrelated file at the old path.
	if err := os.WriteFile(filepath.Join(f.music, one.Path), []byte("unmanaged"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := f.lib.Delete(f.ctx, one.ID); err != nil {
		t.Fatal(err)
	}
	f.exists(t, one.Path, true)
	if err := f.lib.Delete(f.ctx, two.ID); err != nil {
		t.Fatal(err)
	}
	f.exists(t, "Artist/Album/cover.jpg", false)
	f.exists(t, "Artist/Album/readme.txt", true)
}

func TestDeleteBusyImportsAndAlreadyMissingFiles(t *testing.T) {
	f := deletionLibrary(t)
	one := f.tracks["one.mp3"]
	jobs := database.IngestStore{DB: f.db}
	job := ingest.Job{ID: "active", RootID: f.root, Owner: "alice", State: ingest.Running, Expanded: true, Request: ingest.Request{RequestID: "active"}, Items: []ingest.Item{{State: ingest.Running, Selection: ingest.Selection{ReplaceTrackID: one.ID}}}}
	if _, err := jobs.Create(f.ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := f.lib.Delete(f.ctx, one.ID); !errors.Is(err, library.ErrBusy) {
		t.Fatal(err)
	}
	f.exists(t, one.Path, true)
	if _, err := jobs.Update(f.ctx, job.ID, func(j *ingest.Job) error { j.State = ingest.Cancelled; j.Cleaned = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.music, one.Path)); err != nil {
		t.Fatal(err)
	}
	if err := f.lib.Delete(f.ctx, one.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.lib.Delete(f.ctx, "does-not-exist"); !errors.Is(err, library.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestDeleteRejectsTraversalDirectoriesAndSymlinkParents(t *testing.T) {
	for _, path := range []string{"../outside.mp3", ".", "/tmp/outside.mp3", "linked/target.mp3", "Artist/Album"} {
		t.Run(path, func(t *testing.T) {
			f := deletionLibrary(t)
			one := f.tracks["one.mp3"]
			outside := t.TempDir()
			if err := os.WriteFile(filepath.Join(outside, "target.mp3"), []byte("protected"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(f.music, "linked")); err != nil {
				t.Fatal(err)
			}
			if _, err := f.db.Exec("UPDATE tracks SET path=? WHERE id=?", path, one.ID); err != nil {
				t.Fatal(err)
			}
			if err := f.lib.Delete(f.ctx, one.ID); err == nil {
				t.Fatal("unsafe deletion accepted")
			}
			if _, err := f.lib.Get(f.ctx, one.ID); err != nil {
				t.Fatal("catalog changed on validation failure", err)
			}
			if b, err := os.ReadFile(filepath.Join(outside, "target.mp3")); err != nil || string(b) != "protected" {
				t.Fatal("outside asset affected", err)
			}
		})
	}
}

type interruptedDelete struct{ database.Catalog }

func (interruptedDelete) FinishDelete(context.Context, string, string) error {
	return errors.New("receipt write interrupted")
}

func TestDeletionRecoveryAfterCommitAndAfterUnlink(t *testing.T) {
	for _, unlink := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-unlink", true: "after-unlink"}[unlink], func(t *testing.T) {
			f := deletionLibrary(t)
			one := f.tracks["one.mp3"]
			store := database.Catalog{DB: f.db}
			if unlink {
				lib, err := library.Open(f.music, interruptedDelete{store})
				if err != nil {
					t.Fatal(err)
				}
				err = lib.Delete(f.ctx, one.ID)
				lib.Close()
				if !errors.Is(err, library.ErrCleanupPending) {
					t.Fatal(err)
				}
				f.exists(t, one.Path, false)
			} else {
				if _, err := store.BeginDelete(f.ctx, f.root, one.ID, func([]string) error { return nil }); err != nil {
					t.Fatal(err)
				}
				f.exists(t, one.Path, true)
			}
			if _, err := f.lib.Get(f.ctx, one.ID); !errors.Is(err, library.ErrNotFound) {
				t.Fatal(err)
			}
			if err := f.lib.RecoverDeletions(f.ctx); err != nil {
				t.Fatal(err)
			}
			f.exists(t, one.Path, false)
			pending, err := store.PendingDeletions(f.ctx, f.root)
			if err != nil || len(pending) != 0 {
				t.Fatal(pending, err)
			}
		})
	}
}

func TestDeletionPermissionFailureCanBeRetried(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	f := deletionLibrary(t)
	one := f.tracks["one.mp3"]
	dir := filepath.Join(f.music, "Artist/Album")
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0755)
	if err := f.lib.Delete(f.ctx, one.ID); !errors.Is(err, library.ErrCleanupPending) {
		t.Fatal(err)
	}
	f.exists(t, one.Path, true)
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := f.lib.Delete(f.ctx, one.ID); err != nil {
		t.Fatal(err)
	}
	f.exists(t, one.Path, false)
}
