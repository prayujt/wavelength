package database_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prayujt/wavelength/internal/database"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/sharing"
	"github.com/prayujt/wavelength/internal/testutil"
)

func TestMediaGrantsSurviveRestartAndStayBoundToTheirLibrary(t *testing.T) {
	ctx := context.Background()
	dir, music := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(music, "one.mp3"), []byte("audio"), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	testutil.SeedCatalog(t, db, music)
	lib, err := library.Open(music, database.Catalog{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	page, err := lib.List(ctx, library.Query{})
	if err != nil {
		t.Fatal(err)
	}
	links := sharing.New(database.GrantStore{DB: db}, lib, "https://music.example")
	share, err := links.CreateShare(ctx, "alice", sharing.ShareRequest{Name: "Saved songs", TrackIDs: []string{page.Tracks[0].ID}})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(share.URL)
	shareToken := strings.TrimPrefix(u.Path, "/s/")
	playback, err := links.PlaybackFromShare(ctx, shareToken, page.Tracks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := url.Parse(playback.URL)
	playbackToken := strings.Split(p.Path, "/")[2]
	if _, err := links.AuthorizePlayback(ctx, shareToken); !errors.Is(err, sharing.ErrNotFound) {
		t.Fatal("share token accepted as playback token", err)
	}
	if _, err := links.ResolveShare(ctx, playbackToken); !errors.Is(err, sharing.ErrNotFound) {
		t.Fatal("playback token accepted as share token", err)
	}
	lib.Close()
	db.Close()
	db, err = database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	lib, err = library.Open(music, database.Catalog{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	links = sharing.New(database.GrantStore{DB: db}, lib, "https://music.example")
	if _, err := links.ResolveShare(ctx, shareToken); err != nil {
		t.Fatal("share lost after restart", err)
	}
	if id, err := links.AuthorizePlayback(ctx, playbackToken); err != nil || id != page.Tracks[0].ID {
		t.Fatal("playback lost after restart", id, err)
	}
	another, err := library.Open(t.TempDir(), database.Catalog{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	defer another.Close()
	wrongRoot := sharing.New(database.GrantStore{DB: db}, another, "https://music.example")
	if _, err := wrongRoot.AuthorizePlayback(ctx, playbackToken); !errors.Is(err, sharing.ErrNotFound) {
		t.Fatal("grant crossed library roots", err)
	}
	if err := wrongRoot.Revoke(ctx, "alice", share.Grant.ID); !errors.Is(err, sharing.ErrNotFound) {
		t.Fatal("revocation crossed library roots", err)
	}
	if err := links.Revoke(ctx, "alice", share.Grant.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := links.AuthorizePlayback(ctx, playbackToken); !errors.Is(err, sharing.ErrNotFound) {
		t.Fatal("parent revocation lost after restart", err)
	}
}
