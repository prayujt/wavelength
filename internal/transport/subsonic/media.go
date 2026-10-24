package subsonic

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"log/slog"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/transcode"
	"github.com/prayujt/wavelength/internal/transport/media"
)

func (a *api) handle(w http.ResponseWriter, r *http.Request, endpoint string, result *response) bool {
	if endpoint == "jukeboxControl" {
		failure(result, 0, "Jukebox playback is not supported")
		return true
	}
	if a.video(w, r, endpoint, result) {
		return true
	}
	if a.podcastMedia(w, r, endpoint, result) {
		return true
	}
	if a.podcasts(r, endpoint, result) {
		return true
	}
	if a.hls(w, r, endpoint, result) {
		return true
	}
	if a.shares(r, endpoint, result) {
		return true
	}
	if a.users(r, endpoint, result) {
		return true
	}
	if a.browse(w, r, endpoint, result) {
		return true
	}
	if a.userData(r, endpoint, result) {
		return true
	}
	switch endpoint {
	case "stream", "download":
		id := required(r, result, "id")
		if result.Error != nil {
			return true
		}
		if endpoint == "download" {
			track, err := a.lib.Get(r.Context(), id)
			if err != nil {
				legacyError(result, err)
				return true
			}
			w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(track.Path)}))
			if err = media.Stream(w, r, a.lib, id); err != nil {
				w.Header().Del("Content-Disposition")
				legacyError(result, err)
				return true
			}
			result.Status = "streamed"
			return true
		}
		bitrate, err := number(r, "maxBitRate", 0, 100000)
		if err != nil {
			failure(result, 10, err.Error())
			return true
		}
		current, err := a.account(r.Context(), r.Form.Get("u"))
		if err != nil {
			legacyError(result, err)
			return true
		}
		if current.MaxBitRate > 0 && (bitrate == 0 || bitrate > current.MaxBitRate) {
			bitrate = current.MaxBitRate
		}
		format := r.Form.Get("format")
		track, err := a.lib.Get(r.Context(), id)
		if err != nil {
			legacyError(result, err)
			return true
		}
		if strings.HasPrefix(track.ContentType, "video/") && a.converted(w, r, result, id) {
			return true
		}
		if a.maybeVideoTranscode(w, r, result, track.ContentType, id, format, bitrate) {
			return true
		}
		transcode := format != "raw" && ((format != "" && format != strings.TrimPrefix(filepath.Ext(track.Path), ".")) || (bitrate > 0 && (track.BitRate == 0 || track.BitRate > bitrate)))
		if !transcode {
			if err = media.Stream(w, r, a.lib, id); err != nil {
				legacyError(result, err)
				return true
			}
			result.Status = "streamed"
			return true
		}
		if format == "" {
			format = "mp3"
		}
		if bitrate == 0 {
			bitrate = 192
		}
		if format == "mp3" && bitrate > 320 {
			bitrate = 320
		}
		started, err := a.transcode(w, r, id, format, bitrate)
		if started {
			result.Status = "streamed"
			if err != nil {
				slog.Error("transcoded stream failed", "error", err)
			}
		} else if err != nil {
			failure(result, 0, err.Error())
		}
		return true
	case "getCoverArt":
		id := required(r, result, "id")
		if result.Error != nil {
			return true
		}
		size, err := number(r, "size", 0, 4096)
		if err != nil {
			failure(result, 10, err.Error())
			return true
		}
		trackID := id
		if _, err = a.lib.Get(r.Context(), id); errors.Is(err, library.ErrNotFound) {
			c, e := a.collection(r.Context(), r.Form.Get("u"))
			if e != nil {
				legacyError(result, e)
				return true
			}
			if entries := c.directoryTracks(id); len(entries) > 0 {
				trackID = entries[0].ID
			}
			for _, alb := range c.albums {
				if alb.ID == id && len(alb.Songs) > 0 {
					trackID = alb.Songs[0].ID
				}
			}
			for _, art := range c.artists {
				if art.ID == id && art.CoverArt != "" {
					trackID = art.CoverArt
				}
			}
		}
		body, contentType, err := a.lib.Artwork(r.Context(), trackID)
		if err != nil {
			legacyError(result, err)
			return true
		}
		if size > 0 {
			configuration, _, err := image.DecodeConfig(bytes.NewReader(body))
			if err != nil || configuration.Width > 20000 || configuration.Height > 20000 || int64(configuration.Width)*int64(configuration.Height) > 50_000_000 {
				failure(result, 0, "Invalid or oversized artwork")
				return true
			}
			source, _, err := image.Decode(bytes.NewReader(body))
			if err != nil {
				failure(result, 0, "Invalid artwork")
				return true
			}
			bounds := source.Bounds()
			width, height := bounds.Dx(), bounds.Dy()
			if width > size || height > size {
				if width >= height {
					height = height * size / width
					width = size
				} else {
					width = width * size / height
					height = size
				}
				if width < 1 {
					width = 1
				}
				if height < 1 {
					height = 1
				}
				scaled := image.NewRGBA(image.Rect(0, 0, width, height))
				for y := 0; y < height; y++ {
					for x := 0; x < width; x++ {
						scaled.Set(x, y, source.At(bounds.Min.X+x*bounds.Dx()/width, bounds.Min.Y+y*bounds.Dy()/height))
					}
				}
				var out bytes.Buffer
				if err = jpeg.Encode(&out, scaled, &jpeg.Options{Quality: 85}); err != nil {
					failure(result, 0, "Artwork conversion failed")
					return true
				}
				body = out.Bytes()
				contentType = "image/jpeg"
			}
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method != "HEAD" {
			_, _ = w.Write(body)
		}
		result.Status = "streamed"
		return true
	case "getLyrics":
		page, err := a.lib.List(r.Context(), library.Query{Search: r.Form.Get("title"), Limit: 500})
		if err != nil {
			legacyError(result, err)
			return true
		}
		lyrics := ""
		for _, track := range page.Tracks {
			if artist := r.Form.Get("artist"); artist != "" && !strings.EqualFold(artist, track.Artist) {
				continue
			}
			value, e := a.lib.Lyrics(r.Context(), track.ID)
			if e == nil {
				lyrics = value
				break
			}
			if !errors.Is(e, library.ErrNotFound) {
				legacyError(result, e)
				return true
			}
		}
		payload(result, "lyrics", map[string]any{"artist": r.Form.Get("artist"), "title": r.Form.Get("title"), "value": lyrics})
		return true
	case "getAvatar":
		a.avatar(w, r, result)
		return true
	case "getScanStatus", "startScan":
		if endpoint == "startScan" && a.scanQueued.CompareAndSwap(false, true) {
			go func() {
				defer a.scanQueued.Store(false)
				if _, err := a.lib.Scan(a.ctx); err != nil && !errors.Is(err, library.ErrScanRunning) {
					slog.Error("legacy scan failed", "error", err)
				}
			}()
		}
		scanning, count := a.lib.ScanStatus()
		payload(result, "scanStatus", map[string]any{"scanning": scanning || a.scanQueued.Load(), "count": count})
		return true
	case "getVideos":
		payload(result, "videos", map[string]any{"video": []any{}})
		return true
	}
	return false
}
func (a *api) transcode(w http.ResponseWriter, r *http.Request, id, format string, bitrate int) (bool, error) {
	stream, err := a.transcoder.Audio(r.Context(), id, transcode.AudioOptions{Format: format, BitRate: bitrate})
	if err != nil {
		return false, err
	}
	return serveEncoded(w, r, stream)
}
