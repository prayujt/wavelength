package subsonic

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/prayujt/wavelength/internal/transcode"
	"github.com/prayujt/wavelength/internal/transport/media"
)

func serveEncoded(w http.ResponseWriter, r *http.Request, stream *transcode.Stream) (bool, error) {
	exact := false
	if r.Form.Has("estimateContentLength") {
		var err error
		exact, err = strconv.ParseBool(r.Form.Get("estimateContentLength"))
		if err != nil {
			stream.Close()
			return false, fmt.Errorf("%w: invalid estimateContentLength", transcode.ErrInvalid)
		}
	}
	return media.ServeEncoded(w, r, stream, exact)
}
