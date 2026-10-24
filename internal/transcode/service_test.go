package transcode_test

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prayujt/wavelength/internal/database"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/testutil"
	"github.com/prayujt/wavelength/internal/transcode"
)

func TestStreamsAndHLSSessionsWithoutHTTP(t *testing.T) {
	for _, tool := range []string{"ffmpeg"} {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("WAVELENGTH_REQUIRE_MEDIA_TOOLS") == "1" {
				t.Fatal(err)
			}
			t.Skip(tool + " is not installed")
		}
	}
	ctx := context.Background()
	root := t.TempDir()
	if output, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=duration=0.3", filepath.Join(root, "tone.flac")).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, output)
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
	testutil.SeedCatalog(t, db, root)
	page, err := lib.List(ctx, library.Query{})
	if err != nil {
		t.Fatal(err)
	}
	id := page.Tracks[0].ID
	service := transcode.New(ctx, lib)
	defer service.Close()
	stream, err := service.Audio(ctx, id, transcode.AudioOptions{Format: "mp3", BitRate: 64})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(stream)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Wait(); err != nil {
		t.Fatal(err)
	}
	stream.Close()
	if stream.ContentType != "audio/mpeg" || len(body) == 0 {
		t.Fatal("empty encoded stream")
	}
	encoded := filepath.Join(t.TempDir(), "encoded.mp3")
	if err := os.WriteFile(encoded, body, 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("ffmpeg", "-v", "error", "-i", encoded, "-f", "null", "-").CombinedOutput(); err != nil {
		t.Fatalf("decode: %v %s", err, output)
	}
	playlist, err := service.HLS(ctx, "alice", id, transcode.HLSOptions{BitRate: 64})
	if err != nil {
		t.Fatal(err)
	}
	var segment string
	for _, line := range strings.Split(string(playlist.Body), "\n") {
		if transcode.IsSegment(line) {
			segment = line
			break
		}
	}
	if segment == "" {
		t.Fatal("playlist has no media segment")
	}
	if _, err := service.Segment("bob", playlist.SessionID, segment); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("foreign HLS access: %v", err)
	}
	if _, err := service.Segment("alice", playlist.SessionID, "../index.m3u8"); !errors.Is(err, transcode.ErrInvalid) {
		t.Fatalf("segment traversal: %v", err)
	}
	file, err := service.Segment("alice", playlist.SessionID, segment)
	if err != nil {
		t.Fatal(err)
	}
	name := file.Name()
	file.Close()
	service.Close()
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatalf("HLS files survived shutdown: %v", err)
	}
	if _, err := service.HLS(ctx, "alice", id, transcode.HLSOptions{BitRate: 64}); !errors.Is(err, context.Canceled) {
		t.Fatalf("closed service accepted job: %v", err)
	}
}
