package subsonic_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
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
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
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
func TestRealTranscodingHLSVideoAndCaptions(t *testing.T) {
	mediaTools(t)
	music := t.TempDir()
	encode(t, "-f", "lavfi", "-i", "sine=frequency=440:duration=1.2", "-metadata", "title=Tone", "-metadata", "artist=Fixture", "-metadata", "album=Fixtures", filepath.Join(music, "tone.flac"))
	encode(t, "-f", "lavfi", "-i", "color=c=blue:s=160x120:d=1.2", "-f", "lavfi", "-i", "sine=frequency=330:duration=1.2", "-c:v", "mpeg4", "-c:a", "aac", "-shortest", filepath.Join(music, "clip.mp4"))
	if err := os.WriteFile(filepath.Join(music, "clip.vtt"), []byte("WEBVTT\n\n00:00.000 --> 00:01.000\nReal caption\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, "", music)
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
	f.ok(t, "createUser", url.Values{"username": {"capped"}, "password": {"app"}, "email": {"a@example.test"}})
	f.ok(t, "updateUser", url.Values{"username": {"capped"}, "maxBitRate": {"64"}})
	capped := f.raw(t, "stream", url.Values{"u": {"capped"}, "p": {"app"}, "id": {track}})
	if capped.Header().Get("Content-Type") != "audio/mpeg" {
		t.Fatalf("account bitrate not applied: %s", capped.Body)
	}
	decode(t, capped.Body.Bytes(), "mp3")

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
	videos := list(f.ok(t, "getVideos", nil), "videos", "video")
	if len(videos) != 1 {
		t.Fatal(videos)
	}
	videoID := videos[0].(map[string]any)["id"].(string)
	f.ok(t, "getSong", url.Values{"id": {videoID}})
	master := f.raw(t, "hls", url.Values{"id": {videoID}, "bitRate": {"256@80x60", "512@160x120"}}).Body.String()
	if !strings.Contains(master, "RESOLUTION=80x60") || strings.Count(master, "#EXT-X-STREAM-INF") != 2 {
		t.Fatal(master)
	}
	variant := f.raw(t, "hls", url.Values{"id": {videoID}, "bitRate": {"256@80x60"}, "audioTrack": {"1"}})
	if !strings.Contains(variant.Body.String(), "#EXTM3U") {
		t.Fatal(variant.Body)
	}

	info := f.ok(t, "getVideoInfo", url.Values{"id": {videoID}})
	if len(list(info, "videoInfo", "audioTrack")) != 1 {
		t.Fatal(info)
	}
	caption := f.raw(t, "getCaptions", url.Values{"id": {videoID}, "format": {"vtt"}})
	if !strings.Contains(caption.Body.String(), "Real caption") {
		t.Fatal(caption.Body)
	}
	conversion := f.raw(t, "stream", url.Values{"id": {videoID}, "format": {"mp4"}, "size": {"80x60"}, "maxBitRate": {"256"}})
	if conversion.Header().Get("Content-Type") != "video/mp4" {
		t.Fatal(conversion.Body)
	}
	decode(t, conversion.Body.Bytes(), "mp4")
	convertedDir := filepath.Join(f.data, "converted")
	if err := os.MkdirAll(convertedDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(convertedDir, videoID+".mp4"), conversion.Body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	// Reopen on the same persistent catalog with a configured converted-file directory.
	withConverted := newFixture(t, f.data, f.music)
	info = withConverted.ok(t, "getVideoInfo", url.Values{"id": {videoID}})
	if len(list(info, "videoInfo", "conversion")) != 1 {
		t.Fatal(info)
	}
	stored := withConverted.raw(t, "stream", url.Values{"id": {videoID}, "converted": {"true"}})
	if !bytes.Equal(stored.Body.Bytes(), conversion.Body.Bytes()) {
		t.Fatal("converted=true did not return stored MP4")
	}
	captionSRT := f.raw(t, "getCaptions", url.Values{"id": {videoID}, "format": {"srt"}})
	if !strings.Contains(captionSRT.Body.String(), "00:00:00,000") {
		t.Fatal(captionSRT.Body)
	}

}
func TestPublicShareExpirationRevocationAndScope(t *testing.T) {
	f := newFixture(t, "", "")
	track, album, _ := f.ids(t)
	f.ok(t, "updateUser", url.Values{"username": {"admin"}, "shareRole": {"true"}})
	node := list(f.ok(t, "createShare", url.Values{"id": {album}, "description": {"A <safe> share"}}), "shares", "share")[0].(map[string]any)
	shareID := node["id"].(string)
	request := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		f.server.Handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	page := request("/share/" + shareID)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "A &lt;safe&gt; share") {
		t.Fatal(page.Body)
	}
	audio := request("/share/" + shareID + "/stream/" + track)
	if audio.Code != 200 || audio.Body.Len() != 256 {
		t.Fatal("shared audio missing")
	}
	if response := request("/share/" + shareID + "/stream/not-in-share"); response.Code != 404 {
		t.Fatal("share scope bypass")
	}
	f.ok(t, "updateShare", url.Values{"id": {shareID}, "expires": {"1"}})
	if request("/share/"+shareID).Code != 404 {
		t.Fatal("expired share accessible")
	}
	f.ok(t, "updateShare", url.Values{"id": {shareID}, "expires": {"0"}})
	f.ok(t, "deleteShare", url.Values{"id": {shareID}})
	if request("/share/"+shareID).Code != 404 {
		t.Fatal("deleted share accessible")
	}
}
func TestPodcastRSSDownloadPlaybackAndDeletion(t *testing.T) {
	mediaTools(t)
	source := filepath.Join(t.TempDir(), "episode.mp3")
	encode(t, "-f", "lavfi", "-i", "sine=frequency=220:duration=0.3", source)
	audio, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	var address string
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/episode.mp3" {
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write(audio)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Local fixture podcast</title><description>Actual local feed</description><item><guid>episode-one</guid><title>Episode one</title><pubDate>Mon, 02 Jan 2006 15:04:05 -0700</pubDate><enclosure url="%s/episode.mp3" type="audio/mpeg" length="%d"/></item></channel></rss>`, address, len(audio))
	}))
	defer feed.Close()
	address = feed.URL
	f := newFixture(t, "", "")
	f.ok(t, "updateUser", url.Values{"username": {"admin"}, "podcastRole": {"true"}})
	f.ok(t, "createPodcastChannel", url.Values{"url": {feed.URL + "/feed"}})
	channel := list(f.ok(t, "getPodcasts", nil), "podcasts", "channel")[0].(map[string]any)
	channelID := channel["id"].(string)
	episode := channel["episode"].([]any)[0].(map[string]any)
	episodeID := episode["id"].(string)
	f.ok(t, "downloadPodcastEpisode", url.Values{"id": {episodeID}})
	deadline := time.Now().Add(5 * time.Second)
	for {
		body := f.ok(t, "getPodcasts", url.Values{"id": {channelID}})
		episode = list(body, "podcasts", "channel")[0].(map[string]any)["episode"].([]any)[0].(map[string]any)
		if episode["status"] == "completed" {
			break
		}
		if episode["status"] == "error" || time.Now().After(deadline) {
			t.Fatal(episode)
		}
		time.Sleep(10 * time.Millisecond)
	}
	stream := f.raw(t, "stream", url.Values{"id": {episode["streamId"].(string)}})
	if !bytes.Equal(stream.Body.Bytes(), audio) {
		t.Fatal("podcast bytes differ")
	}
	if len(list(f.ok(t, "getNewestPodcasts", nil), "newestPodcasts", "episode")) != 1 {
		t.Fatal("downloaded episode missing")
	}
	f.ok(t, "refreshPodcasts", nil)
	f.ok(t, "deletePodcastEpisode", url.Values{"id": {episodeID}})
	f.ok(t, "deletePodcastChannel", url.Values{"id": {channelID}})
	if len(list(f.ok(t, "getPodcasts", nil), "podcasts", "channel")) != 0 {
		t.Fatal("channel deletion failed")
	}
}
