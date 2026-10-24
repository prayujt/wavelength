package ingest

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dhowden/tag"
	"github.com/prayujt/wavelength/internal/discovery"
)

type assetTransport func(*http.Request) (*http.Response, error)

func (f assetTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestPreparedMP3HasMatchingMetadataArtworkAndLyrics(t *testing.T) {
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("WAVELENGTH_REQUIRE_MEDIA_TOOLS") != "" {
				t.Fatal(err)
			}
			t.Skip(err)
		}
	}
	path := filepath.Join(t.TempDir(), "audio.mp3")
	if raw, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=duration=0.25", path).CombinedOutput(); err != nil {
		t.Fatal(err, string(raw))
	}
	m := NewAudioMedia()
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+j4K0AAAAASUVORK5CYII=")
	m.http = &http.Client{Transport: assetTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"syncedLyrics":"[00:00.00]Fixture lyrics"}`
		if strings.HasSuffix(r.URL.Hostname(), "mzstatic.com") {
			body = string(png)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	metadata := discovery.Track{Title: "Fixture", Artist: "Track Artist", Album: "Album", AlbumArtist: "Various Artists", Genre: "Country", ReleaseDate: "2024-01-01", TrackNumber: 2, DiscNumber: 1, Duration: .25, ArtworkURL: "https://is1-ssl.mzstatic.com/cover.png"}
	actual, warnings, err := m.Prepare(context.Background(), path, metadata)
	if err != nil || len(warnings) != 0 || actual.AlbumArtist != "Various Artists" || actual.Size <= 0 || actual.Duration <= 0 || actual.Year != 2024 {
		t.Fatal(actual, warnings, err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tags, err := tag.ReadFrom(f)
	if err != nil {
		t.Fatal(err)
	}
	if tags.Title() != "Fixture" || tags.Artist() != "Track Artist" || tags.AlbumArtist() != "Various Artists" || tags.Picture() == nil || !strings.Contains(tags.Lyrics(), "Fixture lyrics") {
		t.Fatal("embedded metadata is incomplete", tags.Raw())
	}
	// A retry may encounter a tagged file with an attached cover image.
	if _, err := probeAudio(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(t.TempDir(), "bad.mp3")
	os.WriteFile(bad, []byte("not audio"), 0600)
	if _, err = probeAudio(context.Background(), bad); err == nil {
		t.Fatal("invalid media accepted")
	}
}
func TestArtworkRequestsRejectUntrustedLocations(t *testing.T) {
	m := NewAudioMedia()
	for _, url := range []string{"http://127.0.0.1/private", "https://mzstatic.com.evil.example/cover", "https://user:password@is1.mzstatic.com/cover", "https://is1.mzstatic.com:8443/cover"} {
		if _, _, err := m.cover(context.Background(), url); err == nil {
			t.Fatal("untrusted artwork URL accepted", url)
		}
	}
}
