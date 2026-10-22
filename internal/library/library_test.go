package library_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/prayujt/wavelength/internal/database"
	"github.com/prayujt/wavelength/internal/library"
)

func TestScanReconcileAndRootIsolation(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	root := t.TempDir()
	if err = os.Mkdir(filepath.Join(root, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(root, "nested", "first.wav")
	if err = os.WriteFile(first, []byte("RIFF not tagged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "ignore.txt"), []byte("ignore"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.mp3")
	if err = os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(root, "escape.mp3")); err != nil {
		t.Fatal(err)
	}
	lib, err := library.Open(root, database.Catalog{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	result, err := lib.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Indexed != 1 || result.MetadataWarnings != 1 {
		t.Fatalf("scan: %+v", result)
	}
	page, err := lib.List(ctx, library.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Tracks[0].Title != "first" || page.Tracks[0].Path != "nested/first.wav" {
		t.Fatalf("catalog: %+v", page)
	}
	id := page.Tracks[0].ID
	if err = os.WriteFile(first, []byte("new content with different size"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = lib.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	track, err := lib.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if track.Size != 31 {
		t.Fatalf("update not reflected: %+v", track)
	}
	// A cancelled scan must not prune the previous snapshot even if the file disappeared.
	if err = os.Remove(first); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = lib.Scan(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation: %v", err)
	}
	if _, err = lib.Get(ctx, id); err != nil {
		t.Fatalf("cancelled scan removed track: %v", err)
	}
	if _, err = lib.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = lib.Get(ctx, id); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("missing track not pruned: %v", err)
	}
	// Separate configured roots never expose each other's tracks.
	other, err := library.Open(t.TempDir(), database.Catalog{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	page, err = other.List(ctx, library.Query{})
	if err != nil || page.Total != 0 {
		t.Fatalf("root isolation: %+v %v", page, err)
	}
}

func TestTagsAndStableIDAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	music := t.TempDir()
	// An ID3v1 footer exercises real tag extraction, independent of an external encoder.
	data := make([]byte, 256)
	copy(data[128:], "TAG")
	copy(data[131:161], "Tagged title")
	copy(data[161:191], "An artist")
	copy(data[191:221], "An album")
	copy(data[221:225], "2026")
	data[254] = 3
	if err := os.WriteFile(filepath.Join(music, "file.mp3"), data, 0600); err != nil {
		t.Fatal(err)
	}
	var firstID string
	for i := 0; i < 2; i++ {
		db, err := database.Open(ctx, dir)
		if err != nil {
			t.Fatal(err)
		}
		lib, err := library.Open(music, database.Catalog{DB: db})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = lib.Scan(ctx); err != nil {
			t.Fatal(err)
		}
		page, err := lib.List(ctx, library.Query{Search: "AN ARTIST"})
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 1 {
			t.Fatalf("search: %+v", page)
		}
		track := page.Tracks[0]
		if track.Title != "Tagged title" || track.Artist != "An artist" || track.Album != "An album" || track.Year != 2026 || track.TrackNumber != 3 {
			t.Fatalf("tags: %+v", track)
		}
		if i == 0 {
			firstID = track.ID
		} else if firstID != track.ID {
			t.Fatal("ID changed on reopen")
		}
		if err = lib.Close(); err != nil {
			t.Fatal(err)
		}
		if err = db.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
