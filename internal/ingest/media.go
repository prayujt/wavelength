package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/bogem/id3v2/v2"
	"github.com/prayujt/wavelength/internal/discovery"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/telemetry"
)

// AudioMedia validates only the newly acquired file. It never scans a directory.
type AudioMedia struct {
	http    *http.Client
	metrics *telemetry.Metrics
}

func (m *AudioMedia) SetMetrics(metrics *telemetry.Metrics) { m.metrics = metrics }
func (m *AudioMedia) request(req *http.Request, provider string) (*http.Response, error) {
	start := time.Now()
	response, err := m.http.Do(req)
	code := 0
	if response != nil {
		code = response.StatusCode
	}
	m.metrics.Provider(provider, "fetch", code, err, time.Since(start))
	return response, err
}

func NewAudioMedia() *AudioMedia {
	return &AudioMedia{http: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 3 || !artworkURL(r.URL) {
			return fmt.Errorf("artwork redirect is not allowed")
		}
		return nil
	}}}
}
func artworkURL(u *url.URL) bool {
	return u.Scheme == "https" && u.User == nil && u.Port() == "" && (u.Hostname() == "mzstatic.com" || strings.HasSuffix(u.Hostname(), ".mzstatic.com"))
}
func (m *AudioMedia) cover(ctx context.Context, raw string) ([]byte, string, error) {
	u, err := url.Parse(raw)
	if err != nil || !artworkURL(u) {
		return nil, "", fmt.Errorf("invalid artwork URL")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	response, err := m.request(req, "artwork")
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, "", fmt.Errorf("artwork status %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil {
		return nil, "", err
	}
	kind := http.DetectContentType(data)
	if len(data) > 8<<20 || !strings.HasPrefix(kind, "image/") {
		return nil, "", fmt.Errorf("invalid artwork body")
	}
	return data, kind, nil
}
func (m *AudioMedia) lyrics(ctx context.Context, t discovery.Track) (string, error) {
	q := url.Values{"artist_name": {t.Artist}, "track_name": {t.Title}, "album_name": {t.Album}, "duration": {strconv.Itoa(int(t.Duration))}}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://lrclib.net/api/get?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Wavelength/1.0 (+https://github.com/prayujt/wavelength)")
	response, err := m.request(req, "lrclib")
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode == 404 {
		return "", nil
	}
	if response.StatusCode != 200 {
		return "", fmt.Errorf("lyrics status %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		return "", err
	}
	if len(raw) > 1<<20 {
		return "", fmt.Errorf("lyrics too large")
	}
	var l struct {
		Synced string `json:"syncedLyrics"`
		Plain  string `json:"plainLyrics"`
	}
	if err = json.Unmarshal(raw, &l); err != nil {
		return "", err
	}
	if l.Synced != "" {
		return l.Synced, nil
	}
	return l.Plain, nil
}
func (m *AudioMedia) Prepare(ctx context.Context, path string, t discovery.Track) (library.Track, []string, error) {
	doneProbe := m.metrics.Stage("audio_probe")
	out, err := probeAudio(ctx, path)
	doneProbe(err)
	if err != nil {
		return out, nil, err
	}
	warnings := []string{}
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		return out, nil, err
	}
	defer tag.Close()
	tag.DeleteAllFrames()
	tag.SetDefaultEncoding(id3v2.EncodingUTF8)
	tag.SetTitle(t.Title)
	tag.SetArtist(t.Artist)
	tag.SetAlbum(t.Album)
	tag.SetGenre(t.Genre)
	artist := t.AlbumArtist
	if artist == "" {
		artist = t.Artist
	}
	tag.AddTextFrame("TPE2", id3v2.EncodingUTF8, artist)
	if len(t.ReleaseDate) >= 4 {
		tag.SetYear(t.ReleaseDate[:4])
	}
	if t.TrackNumber > 0 {
		tag.AddTextFrame("TRCK", id3v2.EncodingUTF8, strconv.Itoa(t.TrackNumber))
	}
	if t.DiscNumber > 0 {
		tag.AddTextFrame("TPOS", id3v2.EncodingUTF8, strconv.Itoa(t.DiscNumber))
	}
	if t.ArtworkURL != "" {
		done := m.metrics.Stage("artwork")
		data, kind, err := m.cover(ctx, t.ArtworkURL)
		done(err)
		if err != nil {
			warnings = append(warnings, "cover artwork was unavailable")
		} else {
			tag.AddAttachedPicture(id3v2.PictureFrame{Encoding: id3v2.EncodingUTF8, MimeType: kind, PictureType: id3v2.PTFrontCover, Description: "Cover", Picture: data})
		}
	}
	doneLyrics := m.metrics.Stage("lyrics")
	lyrics, err := m.lyrics(ctx, t)
	doneLyrics(err)
	if err != nil {
		warnings = append(warnings, "lyrics were unavailable")
	} else if lyrics != "" {
		tag.AddUnsynchronisedLyricsFrame(id3v2.UnsynchronisedLyricsFrame{Encoding: id3v2.EncodingUTF8, Language: "eng", Lyrics: lyrics})
	}
	if err = ctx.Err(); err != nil {
		return out, nil, err
	}
	doneTags := m.metrics.Stage("tag_write")
	err = tag.Save()
	doneTags(err)
	if err != nil {
		return out, nil, err
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return out, nil, err
	}
	info, err := file.Stat()
	if err == nil {
		err = file.Sync()
	}
	file.Close()
	if err != nil {
		return out, nil, err
	}
	out.Title = t.Title
	out.Artist = t.Artist
	out.Album = t.Album
	out.AlbumArtist = artist
	out.Genre = t.Genre
	out.TrackNumber = t.TrackNumber
	out.DiscNumber = t.DiscNumber
	if len(t.ReleaseDate) >= 4 {
		out.Year, _ = strconv.Atoi(t.ReleaseDate[:4])
	}
	out.Size = info.Size()
	out.ModifiedAt = info.ModTime().UTC()
	return out, warnings, nil
}
func probeAudio(ctx context.Context, path string) (library.Track, error) {
	var t library.Track
	info, err := os.Stat(path)
	if err != nil {
		return t, err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > 256<<20 {
		return t, fmt.Errorf("download is not a supported audio file")
	}
	raw, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=format_name,duration,bit_rate:stream=codec_type,codec_name:stream_disposition=attached_pic", "-of", "json", path).Output()
	if err != nil {
		return t, fmt.Errorf("audio probe failed: %w", err)
	}
	var response struct {
		Format struct {
			Name     string `json:"format_name"`
			Duration string `json:"duration"`
			BitRate  string `json:"bit_rate"`
		} `json:"format"`
		Streams []struct {
			Type        string `json:"codec_type"`
			Codec       string `json:"codec_name"`
			Disposition struct {
				Attached int `json:"attached_pic"`
			} `json:"disposition"`
		} `json:"streams"`
	}
	if err = json.Unmarshal(raw, &response); err != nil {
		return t, err
	}
	audio := false
	for _, stream := range response.Streams {
		if stream.Type == "audio" && stream.Codec == "mp3" {
			audio = true
		}
		if stream.Type == "video" && stream.Disposition.Attached != 1 {
			return t, fmt.Errorf("download contains video")
		}
	}
	t.Duration, err = strconv.ParseFloat(response.Format.Duration, 64)
	if err != nil || !audio || response.Format.Name != "mp3" || t.Duration <= 0 || t.Duration > 7200 || math.IsNaN(t.Duration) || math.IsInf(t.Duration, 0) {
		return t, fmt.Errorf("download is not a valid MP3 track")
	}
	bitRate, _ := strconv.Atoi(response.Format.BitRate)
	t.BitRate = bitRate / 1000
	t.ContentType = "audio/mpeg"
	return t, nil
}
func durationMatches(actual, expected float64) bool {
	delta := actual - expected
	if delta < 0 {
		delta = -delta
	}
	allowance := expected * .08
	if allowance < 10 {
		allowance = 10
	}
	return delta <= allowance
}

// A different recording should not discard artwork or lyrics already curated
// in the saved file. Copy those frames after preparing the new audio.
func preserveMediaTags(path string, previous io.Reader) error {
	old, err := id3v2.ParseReader(io.LimitReader(previous, 16<<20), id3v2.Options{Parse: true})
	if err != nil {
		return err
	}
	frames := old.AllFrames()
	if len(frames["APIC"])+len(frames["USLT"])+len(frames["SYLT"]) == 0 {
		return nil
	}
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		return err
	}
	defer tag.Close()
	for _, id := range []string{"APIC", "USLT", "SYLT"} {
		if len(frames[id]) == 0 {
			continue
		}
		tag.DeleteFrames(id)
		for _, frame := range frames[id] {
			tag.AddFrame(id, frame)
		}
	}
	return tag.Save()
}
