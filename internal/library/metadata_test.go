package library_test

import (
	"context"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/prayujt/wavelength/internal/database"
	"github.com/prayujt/wavelength/internal/library"
)

func TestMetadataAcrossContainers(t *testing.T) {
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("WAVELENGTH_REQUIRE_MEDIA_TOOLS") == "1" {
				t.Fatal(err)
			}
			t.Skip(tool + " is not installed")
		}
	}
	ctx := context.Background()
	root := t.TempDir()
	for _, fixture := range []struct {
		name, codec string
		extra       []string
	}{
		{"tagged.flac", "flac", nil}, {"tagged.ogg", "libvorbis", nil},
		{"tagged.opus", "libopus", nil}, {"tagged.wav", "pcm_s16le", nil},
		{"tagged.aiff", "pcm_s16be", []string{"-write_id3v2", "1"}},
		{"tagged.wma", "wmav2", nil}, {"audio-only.mp4", "aac", nil},
	} {
		args := []string{"-v", "error", "-f", "lavfi", "-i", "sine=duration=0.3", "-c:a", fixture.codec,
			"-metadata", "title=Tagged title", "-metadata", "artist=BJÖRK", "-metadata", "album=Tagged album",
			"-metadata", "date=2021-07-15T00:00:00Z", "-metadata", "track=3/12", "-metadata", "disc=2/2",
			"-metadata", "lyrics=Embedded words"}
		args = append(args, fixture.extra...)
		if output, err := exec.Command("ffmpeg", append(args, filepath.Join(root, fixture.name))...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", fixture.name, err, output)
		}
	}
	// Containers with a real video stream stay outside the music catalog.
	if output, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=s=16x16:d=0.1", "-c:v", "mpeg4", filepath.Join(root, "video.mp4")).CombinedOutput(); err != nil {
		t.Fatalf("video fixture: %v %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(root, "video.mkv"), []byte("not an audio extension"), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	lib, err := library.Open(root, database.Catalog{DB: db})
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	if _, err := lib.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := lib.List(ctx, library.Query{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 7 {
		t.Fatalf("indexed %d files; want 7", page.Total)
	}
	for _, track := range page.Tracks {
		t.Run(track.Path, func(t *testing.T) {
			if track.Title != "Tagged title" || track.Artist != "BJÖRK" || track.Album != "Tagged album" || track.Year != 2021 {
				t.Fatalf("metadata lost: %+v", track)
			}
			if track.Path != "tagged.wav" && (track.TrackNumber != 3 || track.DiscNumber != 2) {
				t.Fatalf("fractional track/disc tags lost: %+v", track)
			}
			if track.Duration <= 0 || track.BitRate <= 0 {
				t.Fatalf("missing audio information: %+v", track)
			}
			if track.Path == "tagged.flac" {
				lyrics, err := lib.Lyrics(ctx, track.ID)
				if err != nil || lyrics != "Embedded words" {
					t.Fatalf("embedded lyrics = %q, %v", lyrics, err)
				}
			}
		})
	}
	page, err = lib.List(ctx, library.Query{Search: "björk"})
	if err != nil || page.Total != 7 {
		t.Fatalf("Unicode search: %d, %v", page.Total, err)
	}
	file, err := os.Create(filepath.Join(root, "COVER.JPG"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, kind, err := lib.Artwork(ctx, page.Tracks[0].ID); err != nil || kind != "image/png" {
		t.Fatalf("case-insensitive artwork: %q, %v", kind, err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := lib.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	page, err = lib.List(ctx, library.Query{Search: "Tagged title"})
	if err != nil {
		t.Fatal(err)
	}
	for _, track := range page.Tracks {
		if filepath.Ext(track.Path) == ".flac" || filepath.Ext(track.Path) == ".ogg" || filepath.Ext(track.Path) == ".opus" {
			if track.Year != 2021 || track.TrackNumber != 3 || track.DiscNumber != 2 {
				t.Fatalf("tag fallback requires ffprobe: %+v", track)
			}
		}
	}
}
