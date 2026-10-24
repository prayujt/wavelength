package subsonic

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

// WithVideoDirectory configures optional preconverted MP4 files named <videoID>.mp4.
// Conversion files are app-owned; the source library remains read-only.
func WithVideoDirectory(dir string) Option {
	return func(a *api) error {
		if dir != "" {
			if err := os.MkdirAll(dir, 0700); err != nil {
				return err
			}
		}
		a.videoDir = dir
		return nil
	}
}
func (a *api) converted(w http.ResponseWriter, r *http.Request, result *response, id string) bool {
	requested := false
	if r.Form.Has("converted") {
		var err error
		requested, err = strconv.ParseBool(r.Form.Get("converted"))
		if err != nil {
			failure(result, 10, "Invalid converted")
			return true
		}
	}
	if !requested {
		return false
	}
	file, err := a.transcoder.OpenConverted(id)
	if errors.Is(err, os.ErrNotExist) {
		return false
	} // Contract says to fall back when no conversion exists.
	if err != nil {
		failure(result, 0, "Converted video unavailable")
		return true
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		failure(result, 0, "Converted video unavailable")
		return true
	}
	w.Header().Set("Content-Type", "video/mp4")
	http.ServeContent(w, r, filepath.Base(info.Name()), info.ModTime(), file)
	result.Status = "streamed"
	return true
}
