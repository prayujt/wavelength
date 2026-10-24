package subsonic_test

import (
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func mediaTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"ffmpeg"} {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("WAVELENGTH_REQUIRE_MEDIA_TOOLS") == "1" {
				t.Fatal(err)
			}
			t.Skip("media integration requires " + tool)
		}
	}
}
func encode(t *testing.T, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "ffmpeg", append([]string{"-v", "error", "-y"}, args...)...)
	if body, err := command.CombinedOutput(); err != nil {
		t.Fatalf("encode fixture: %v %s", err, body)
	}
}
func decode(t *testing.T, body []byte, suffix string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stream."+suffix)
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	encode(t, "-i", path, "-f", "null", "-")
}
func TestRealAudioTranscodingAndHLS(t *testing.T) {
	mediaTools(t)
	music := t.TempDir()
	encode(t, "-f", "lavfi", "-i", "sine=frequency=440:duration=1.2", "-metadata", "title=Tone", "-metadata", "artist=Fixture", "-metadata", "album=Fixtures", filepath.Join(music, "tone.flac"))
	f := newFixture(t, "", music)
	if _, err := f.db.Exec("UPDATE tracks SET duration=1.2 WHERE path=?", "tone.flac"); err != nil {
		t.Fatal(err)
	}
	song := list(f.ok(t, "search3", url.Values{"query": {"Tone"}}), "searchResult3", "song")[0].(map[string]any)
	track := song["id"].(string)
	if song["duration"].(float64) < 1 {
		t.Fatal("duration was not probed")
	}
	transcoded := f.raw(t, "stream", url.Values{"id": {track}, "format": {"mp3"}, "maxBitRate": {"64"}})
	if transcoded.Header().Get("Content-Type") != "audio/mpeg" {
		t.Fatalf("transcode: %s", transcoded.Body)
	}
	decode(t, transcoded.Body.Bytes(), "mp3")
	for _, method := range []string{"GET", "HEAD"} {
		args := url.Values{"id": {track}, "format": {"mp3"}, "estimateContentLength": {"true"}, "u": {"admin"}, "p": {"secret"}, "v": {"1.16.1"}, "c": {"tests"}}
		recorder := httptest.NewRecorder()
		f.server.Handler.ServeHTTP(recorder, httptest.NewRequest(method, "/rest/stream?"+args.Encode(), nil))
		length, err := strconv.Atoi(recorder.Header().Get("Content-Length"))
		if err != nil || length == 0 {
			t.Fatalf("%s length: %v %s", method, err, recorder.Body)
		}
		if method == "GET" {
			if length != recorder.Body.Len() {
				t.Fatal("wrong encoded length")
			}
			decode(t, recorder.Body.Bytes(), "mp3")
		} else if recorder.Body.Len() != 0 {
			t.Fatal("HEAD wrote a body")
		}
	}

	playlist := f.raw(t, "hls", url.Values{"id": {track}, "bitRate": {"64"}}).Body.String()
	if !strings.Contains(playlist, "#EXTM3U") {
		t.Fatal(playlist)
	}
	var segment string
	for _, line := range strings.Split(playlist, "\n") {
		if strings.HasPrefix(line, "/rest/hlsSegment") {
			segment = line
			break
		}
	}
	if segment == "" {
		t.Fatal("missing HLS segment")
	}
	w := httptest.NewRecorder()
	f.server.Handler.ServeHTTP(w, httptest.NewRequest("GET", segment, nil))
	if w.Header().Get("Content-Type") != "video/mp2t" || w.Body.Len() == 0 {
		t.Fatalf("HLS segment: %s", w.Body)
	}
	decode(t, w.Body.Bytes(), "ts")
}
