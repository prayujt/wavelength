package subsonic

import (
	"github.com/prayujt/wavelength/internal/transcode"
	"net/http"
	"strings"
)

func scaleArgument(value string) (string, error) { return transcode.ScaleArgument(value) }
func (a *api) videoTranscode(w http.ResponseWriter, r *http.Request, id, format string, bitrate int) (bool, error) {
	if format == "" {
		format = "flv"
	}
	offset, err := int64Parameter(r, "timeOffset", 0)
	if err != nil {
		return false, err
	}
	stream, err := a.transcoder.Video(r.Context(), id, transcode.VideoOptions{Format: format, BitRate: bitrate, Size: r.Form.Get("size"), OffsetSeconds: offset})
	if err != nil {
		return false, err
	}
	return serveEncoded(w, r, stream)
}
func (a *api) maybeVideoTranscode(w http.ResponseWriter, r *http.Request, result *response, contentType, id, format string, bitrate int) bool {
	if !strings.HasPrefix(contentType, "video/") || format == "raw" {
		return false
	}
	if format == "" && bitrate == 0 && r.Form.Get("timeOffset") == "" && r.Form.Get("size") == "" {
		return false
	}
	started, err := a.videoTranscode(w, r, id, format, bitrate)
	if started {
		result.Status = "streamed"
	} else if err != nil {
		failure(result, 0, err.Error())
	}
	return true
}
