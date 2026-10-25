package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/prayujt/wavelength/internal/library"
)

// Exercise publication independently of FFmpeg, including on developer machines
// where the media integration fixtures are skipped.
func TestParallelPublishCreatesSharedDirectories(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	s := &Service{files: root, staging: t.TempDir()}
	const count = 32
	prepared := make([]Prepared, count)
	contents := make([][]byte, count)
	for i := range prepared {
		contents[i] = []byte(fmt.Sprintf("prepared track %d", i))
		digest := sha256.Sum256(contents[i])
		prepared[i] = Prepared{Track: library.Track{Path: fmt.Sprintf("Artist/Album/%d.mp3", i)}, SHA256: hex.EncodeToString(digest[:])}
		stage := s.stagePath("parallel-publish", i)
		if err := os.MkdirAll(filepath.Dir(stage), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(stage, contents[i], 0600); err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	results := make(chan error, count)
	for i := range prepared {
		go func() {
			<-start
			results <- s.publishFile(context.Background(), "parallel-publish", i, prepared[i])
		}()
	}
	close(start)
	for range prepared {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	for i, item := range prepared {
		file, err := root.ReadFile(item.Track.Path)
		if err != nil || string(file) != string(contents[i]) {
			t.Fatalf("track %d: %q, %v", i, file, err)
		}
	}
}
