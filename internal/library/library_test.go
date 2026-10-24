package library_test

import (
	"context"
	"errors"
	"github.com/prayujt/wavelength/internal/database"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/testutil"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPersistedCatalogIgnoresFilesystemChangesAcrossReopen(t *testing.T) {
	ctx := context.Background()
	data, music := t.TempDir(), t.TempDir()
	path := filepath.Join(music, "first.wav")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	testutil.SeedCatalog(t, db, music)
	lib, err := library.Open(music, database.Catalog{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	before, err := lib.List(ctx, library.Query{})
	if err != nil || before.Total != 1 {
		t.Fatal(before, err)
	}
	revision, err := lib.Revision(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lib.Close()
	db.Close()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(music, "new.mp3"), []byte("uncataloged"), 0600); err != nil {
		t.Fatal(err)
	}
	db, err = database.Open(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	lib, err = library.Open(music, database.Catalog{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	after, err := lib.List(ctx, library.Query{})
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("reopen rewrote the catalog", after, err)
	}
	current, err := lib.Revision(ctx)
	if err != nil || current != revision {
		t.Fatal("reopen changed the catalog revision", current, err)
	}
	if _, _, err := lib.OpenTrack(ctx, before.Tracks[0].ID); !errors.Is(err, library.ErrNotFound) {
		t.Fatal("missing source must fail playback without deleting its catalog entry", err)
	}
	other, err := library.Open(t.TempDir(), database.Catalog{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	page, err := other.List(ctx, library.Query{})
	if err != nil || page.Total != 0 {
		t.Fatal("music roots lost isolation", page, err)
	}
}
