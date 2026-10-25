package auth_test

import (
	"bytes"
	"context"
	"database/sql"
	"image"
	"image/png"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/bogem/id3v2/v2"
	"github.com/prayujt/wavelength/internal/database"
	"github.com/prayujt/wavelength/internal/discovery"
	"github.com/prayujt/wavelength/internal/ingest"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/transport/httpserver"
	transportmcp "github.com/prayujt/wavelength/internal/transport/mcp"
	"github.com/prayujt/wavelength/internal/userdata"
)

func TestOIDCWebReturnDestination(t *testing.T) {
	issuer := provider(t, "")
	s, _ := app(t, issuer.URL, t.TempDir(), t.TempDir())
	b := browser(t)
	for _, destination := range []string{"https://other.example", "//other.example", "/oauth/authorize", "/auth/me"} {
		r := do(t, b, "GET", s.URL+"/auth/login?return_to="+destination, nil)
		if r.StatusCode != http.StatusBadRequest {
			t.Fatalf("accepted return destination %q: %d", destination, r.StatusCode)
		}
	}
	r := do(t, b, "GET", s.URL+"/auth/login?return_to=%2F", nil)
	r = do(t, b, "GET", r.Header.Get("Location"), nil)
	r = do(t, b, "GET", r.Header.Get("Location"), nil)
	if r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != "/" {
		t.Fatalf("web callback: %d %s", r.StatusCode, r.Header.Get("Location"))
	}
	me(t, b, s.URL)
}

// The real client runs against real OIDC, cookie/CSRF middleware and Connect
// services. Only external music providers and downloaded audio are fixtures.
func TestWebBrowser(t *testing.T) {
	if os.Getenv("WAVELENGTH_WEB_TESTS") == "" {
		t.Skip("set WAVELENGTH_WEB_TESTS=1 after building web/ and installing Playwright")
	}
	web, err := filepath.Abs("../../web")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(web, "dist/index.html")); err != nil {
		t.Fatal("build web app before running browser tests:", err)
	}
	music := t.TempDir()
	file := filepath.Join(music, "first-light.mp3")
	if output, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=duration=3", "-metadata", "title=First Light", "-metadata", "artist=The Morning", "-metadata", "album=Small Hours", file).CombinedOutput(); err != nil {
		t.Fatalf("create audio fixture: %v\n%s", err, output)
	}
	var cover bytes.Buffer
	if err := png.Encode(&cover, image.NewRGBA(image.Rect(0, 0, 16, 16))); err != nil {
		t.Fatal(err)
	}
	tags, err := id3v2.Open(file, id3v2.Options{Parse: true})
	if err != nil {
		t.Fatal(err)
	}
	tags.AddAttachedPicture(id3v2.PictureFrame{Encoding: id3v2.EncodingUTF8, MimeType: "image/png", PictureType: id3v2.PTFrontCover, Picture: cover.Bytes()})
	tags.AddUnsynchronisedLyricsFrame(id3v2.UnsynchronisedLyricsFrame{Encoding: id3v2.EncodingUTF8, Language: "eng", Lyrics: "[00:00.00]First line\n[00:02.00]Second line"})
	if err := tags.Save(); err != nil {
		t.Fatal(err)
	}
	tags.Close()
	audio, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	issuer := provider(t, "")
	source := &ingestSource{audio: audio}
	s, _ := appWithOptions(t, issuer.URL, t.TempDir(), music, []httpserver.Option{httpserver.WithWeb(os.DirFS(filepath.Join(web, "dist")))}, func(db *sql.DB, lib *library.Library, data *userdata.Service) transportmcp.Features {
		d := discovery.New(ingestMetadata{}, source)
		root, path := lib.SourceRoot()
		service, err := ingest.New(context.Background(), ingest.Root{ID: root, Path: path}, t.TempDir(), ingest.Dependencies{Store: database.IngestStore{DB: db}, Discovery: d, Downloader: source, Media: ingestMedia{}, Playlists: data})
		if err != nil {
			t.Fatal(err)
		}
		return transportmcp.Features{Discovery: d, Ingest: service}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", "node_modules/@playwright/test/cli.js", "test", "--config", "playwright.integration.config.ts")
	command.Dir = web
	command.Env = append(os.Environ(), "WAVELENGTH_WEB_URL="+s.URL)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("web browser checks: %v\n%s", err, output)
	} else {
		t.Log(string(output))
	}
}
