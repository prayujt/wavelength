package media

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/prayujt/wavelength/internal/transcode"
)

// ServeEncoded preserves incremental delivery normally. A client that explicitly
// requests Content-Length gets disk-backed completion before response headers;
// the resulting exact length is also a valid estimate, without truncating media.
func ServeEncoded(w http.ResponseWriter, r *http.Request, stream *transcode.Stream, exactLength bool) (bool, error) {
	defer stream.Close()
	output := stream
	contentType := stream.ContentType
	var err error
	buffer := make([]byte, 32<<10)
	n, readErr := output.Read(buffer)
	if n == 0 {
		_ = stream.Close()
		return false, errors.New("could not decode source media")
	}
	var file *os.File
	if exactLength {
		file, err = os.CreateTemp("", "wavelength-encoded-")
		if err != nil {
			_ = stream.Close()
			return false, err
		}
		defer os.Remove(file.Name())
		defer file.Close()
		if _, err = file.Write(buffer[:n]); err == nil {
			_, err = io.Copy(file, output)
		}
		if err != nil {
			_ = stream.Close()
		}
		waitErr := stream.Wait()
		if err != nil {
			return false, err
		}
		if waitErr != nil {
			return false, waitErr
		}
		info, err := file.Stat()
		if err != nil {
			return false, err
		}
		if _, err = file.Seek(0, 0); err != nil {
			return false, err
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
		if r.Method != "HEAD" {
			_, err = io.Copy(w, file)
		}
		return true, err
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == "HEAD" {
		_ = stream.Close()
		return true, nil
	}
	if _, err = w.Write(buffer[:n]); err == nil {
		_, err = io.Copy(w, output)
	}
	if err != nil {
		_ = stream.Close()
	}
	waitErr := stream.Wait()
	if err != nil {
		return true, err
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return true, readErr
	}
	return true, waitErr
}
