package subsonic

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/prayujt/wavelength/internal/transcode"
)

func (a *api) hls(w http.ResponseWriter, r *http.Request, endpoint string, result *response) bool {
	if endpoint != "hls" && endpoint != "hlsSegment" {
		return false
	}
	user := r.Form.Get("u")
	if endpoint == "hls" && len(r.Form["bitRate"]) > 1 {
		trackID := required(r, result, "id")
		if result.Error != nil {
			return true
		}
		if _, err := a.lib.Get(r.Context(), trackID); err != nil {
			legacyError(result, err)
			return true
		}
		if len(r.Form["bitRate"]) > 8 {
			failure(result, 10, "At most eight HLS variants are allowed")
			return true
		}
		var master strings.Builder
		master.WriteString("#EXTM3U\n")
		for _, value := range r.Form["bitRate"] {
			rate, size, err := parseHLSBitRate(value)
			if err != nil {
				failure(result, 10, "Invalid HLS bitrate")
				return true
			}
			query := url.Values{}
			for key, values := range r.Form {
				query[key] = append([]string{}, values...)
			}
			query.Set("bitRate", value)
			resolution := ""
			if size != "" {
				resolution = ",RESOLUTION=" + size
			}
			_, _ = fmt.Fprintf(&master, "#EXT-X-STREAM-INF:BANDWIDTH=%d%s\n/rest/hls.m3u8?%s\n", rate*1000, resolution, query.Encode())
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		if r.Method != "HEAD" {
			_, _ = fmt.Fprint(w, master.String())
		}
		result.Status = "streamed"
		return true
	}

	if endpoint == "hlsSegment" {
		file, err := a.transcoder.Segment(user, r.Form.Get("id"), r.Form.Get("name"))
		if err != nil {
			legacyError(result, err)
			return true
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			legacyError(result, err)
			return true
		}
		w.Header().Set("Content-Type", "video/mp2t")
		http.ServeContent(w, r, file.Name(), info.ModTime(), file)
		result.Status = "streamed"
		return true
	}
	value := r.Form.Get("bitRate")
	if value == "" {
		value = "192"
	}
	bitrate, size, err := parseHLSBitRate(value)
	if err != nil {
		failure(result, 10, "Invalid HLS bitrate")
		return true
	}
	if size == "" {
		size = r.Form.Get("size")
	}
	account, err := a.account(r.Context(), user)
	if err != nil {
		legacyError(result, err)
		return true
	}
	if account.MaxBitRate > 0 && bitrate > account.MaxBitRate {
		bitrate = account.MaxBitRate
	}
	options := transcode.HLSOptions{BitRate: bitrate, Size: size}
	if r.Form.Has("audioTrack") {
		index, err := strconv.Atoi(r.Form.Get("audioTrack"))
		if err != nil {
			failure(result, 10, "Invalid audioTrack")
			return true
		}
		options.AudioTrack = &index
	}
	playlist, err := a.transcoder.HLS(r.Context(), user, r.Form.Get("id"), options)
	if err != nil {
		legacyError(result, err)
		return true
	}
	lines := strings.Split(string(playlist.Body), "\n")
	for i, line := range lines {
		if transcode.IsSegment(line) {
			query := url.Values{"id": {playlist.SessionID}, "name": {line}, "u": {user}, "v": {r.Form.Get("v")}, "c": {r.Form.Get("c")}}
			if token := r.Form.Get("t"); token != "" {
				query.Set("t", token)
				query.Set("s", r.Form.Get("s"))
			} else {
				query.Set("p", r.Form.Get("p"))
			}
			lines[i] = "/rest/hlsSegment.view?" + query.Encode()
		}
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	if r.Method != "HEAD" {
		_, _ = fmt.Fprint(w, strings.Join(lines, "\n"))
	}
	result.Status = "streamed"
	return true
}

// Subsonic encodes optional video dimensions inside each bitrate value.
func parseHLSBitRate(value string) (int, string, error) {
	parts := strings.Split(value, "@")
	if len(parts) > 2 {
		return 0, "", errors.New("invalid HLS bitrate")
	}
	rate, err := strconv.Atoi(parts[0])
	if err != nil || rate < 16 || rate > 100000 {
		return 0, "", errors.New("invalid HLS bitrate")
	}
	size := ""
	if len(parts) == 2 {
		size = parts[1]
		if size == "" {
			return 0, "", errors.New("invalid HLS size")
		}
		if _, err = scaleArgument(size); err != nil {
			return 0, "", err
		}
	}
	return rate, size, nil
}
