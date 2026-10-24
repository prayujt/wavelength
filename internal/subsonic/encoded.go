package subsonic

import (
	"github.com/prayujt/wavelength/internal/playback"
	"github.com/prayujt/wavelength/internal/transcode"
	"net/http"
	"strconv"
)

func serveEncoded(w http.ResponseWriter, r *http.Request, stream *transcode.Stream) (bool, error) {
	exact := false
	if r.Form.Has("estimateContentLength") {
		var err error
		exact, err = strconv.ParseBool(r.Form.Get("estimateContentLength"))
		if err != nil {
			stream.Close()
			return false, err
		}
	}
	return playback.ServeEncoded(w, r, stream, exact)
}
