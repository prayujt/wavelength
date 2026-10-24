package subsonic_test

import (
	"context"
	"github.com/prayujt/wavelength/internal/testutil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestIndexesFollowPersistedCatalogChanges(t *testing.T) {
	f := newFixture(t, "", "")
	before := f.ok(t, "getIndexes", nil)["indexes"].(map[string]any)["lastModified"].(float64)
	writeSong(t, f.music, "New Artist/Album/new.mp3", "New", "New Artist", "Album", 1)
	path := filepath.Join(f.music, "New Artist/Album/new.mp3")
	old := time.Unix(1, 0)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	testutil.SeedCatalog(t, f.db, f.music)
	args := url.Values{"ifModifiedSince": {strconv.FormatInt(int64(before), 10)}}
	updated := f.ok(t, "getIndexes", args)
	if updated["indexes"] == nil {
		t.Fatal("new artist hidden by cached index")
	}
	stamp := updated["indexes"].(map[string]any)["lastModified"].(float64)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("DELETE FROM tracks WHERE path=?", "New Artist/Album/new.mp3"); err != nil {
		t.Fatal(err)
	}
	args.Set("ifModifiedSince", strconv.FormatInt(int64(stamp), 10))
	if f.ok(t, "getIndexes", args)["indexes"] == nil {
		t.Fatal("removed artist hidden by cached index")
	}
}

func TestAlbumOrderingMatchesSharedCatalog(t *testing.T) {
	root := t.TempDir()
	writeSong(t, root, "Artist/Album/01.mp3", "Zulu", "Artist", "Album", 0)
	writeSong(t, root, "Artist/Album/02.mp3", "Alpha", "Artist", "Album", 0)
	f := newFixture(t, "", root)
	catalog, err := f.lib.Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	album := catalog.Albums[0]
	songs := list(f.ok(t, "getAlbum", url.Values{"id": {album.ID}}), "album", "song")
	for i, track := range album.Tracks {
		if songs[i].(map[string]any)["id"] != track.ID {
			t.Fatal("legacy adapter reordered the shared album")
		}
	}
}
