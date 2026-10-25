package database

import (
	"context"
	"testing"
	"time"

	"github.com/prayujt/wavelength/internal/listening"
)

func TestListeningHistoryPersistsAndIsBoundToOwnerAndLibrary(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	store := ListeningStore{DB: db}
	now := time.Now().UTC()
	original := listening.Session{ID: "same-session", Owner: "alice", RootID: "music-a", Origin: "direct", StartedAt: now, UpdatedAt: now, ListenedMS: 42000}
	if _, err := store.Create(ctx, original); err != nil {
		t.Fatal(err)
	}
	second := original
	second.RootID = "music-b"
	if _, err := store.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	second.RootID = "music-a"
	second.Owner = "bob"
	if _, err := store.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store.DB = db
	page, err := store.List(ctx, "alice", "music-a", listening.Query{Limit: 100})
	if err != nil || page.Total != 1 || len(page.Sessions) != 1 || page.Sessions[0].ListenedMS != 42000 || !page.Sessions[0].StartedAt.Equal(now) {
		t.Fatal(page, err)
	}
	_, err = store.Update(ctx, "alice", "other-root", original.ID, func(*listening.Session) error { t.Fatal("cross-root mutation"); return nil })
	if err != listening.ErrNotFound {
		t.Fatal(err)
	}
}
