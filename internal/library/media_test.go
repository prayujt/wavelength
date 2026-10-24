package library_test

import (
	"context"
	"github.com/prayujt/wavelength/internal/database"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/testutil"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestEmbeddedLyricsAndAdjacentArtwork(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		if os.Getenv("WAVELENGTH_REQUIRE_MEDIA_TOOLS") == "1" {
			t.Fatal(err)
		}
		t.Skip("FFmpeg is not installed")
	}
	music := t.TempDir()
	if output, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=duration=0.3", "-metadata", "title=Tagged title", "-metadata", "artist=BJÖRK", "-metadata", "lyrics=Embedded words", filepath.Join(music, "tone.flac")).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, output)
	}
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	testutil.SeedCatalog(t, db, music)
	lib, err := library.Open(music, database.Catalog{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	page, err := lib.List(ctx, library.Query{Search: "björk"})
	if err != nil || page.Total != 1 {
		t.Fatal(page, err)
	}
	id := page.Tracks[0].ID
	if lyrics, err := lib.Lyrics(ctx, id); err != nil || lyrics != "Embedded words" {
		t.Fatal(lyrics, err)
	}
	file, err := os.Create(filepath.Join(music, "COVER.JPG"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, kind, err := lib.Artwork(ctx, id); err != nil || kind != "image/png" {
		t.Fatal(kind, err)
	}
}
