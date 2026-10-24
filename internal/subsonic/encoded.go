package subsonic

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
)

// serveEncoded preserves incremental delivery normally. A client that explicitly
// requests Content-Length gets disk-backed completion before response headers;
// the resulting exact length is also a valid estimate, without truncating media.
func serveEncoded(w http.ResponseWriter, r *http.Request, command *exec.Cmd, cancel context.CancelFunc, contentType string) (bool, error) {
	estimate := false
	if r.Form.Has("estimateContentLength") {
		var err error
		estimate, err = strconv.ParseBool(r.Form.Get("estimateContentLength"))
		if err != nil {
			return false, errors.New("invalid estimateContentLength")
		}
	}
	output, err := command.StdoutPipe()
	if err != nil {
		return false, err
	}
	if err = command.Start(); err != nil {
		return false, errors.New("could not start transcoder")
	}
	buffer := make([]byte, 32<<10)
	n, readErr := output.Read(buffer)
	if n == 0 {
		cancel()
		_ = command.Wait()
		return false, errors.New("could not decode source media")
	}
	var file *os.File
	if estimate {
		file, err = os.CreateTemp("", "wavelength-encoded-")
		if err != nil {
			cancel()
			_ = command.Wait()
			return false, err
		}
		defer os.Remove(file.Name())
		defer file.Close()
		if _, err = file.Write(buffer[:n]); err == nil {
			_, err = io.Copy(file, output)
		}
		if err != nil {
			cancel()
		}
		waitErr := command.Wait()
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
		cancel()
		_ = command.Wait()
		return true, nil
	}
	if _, err = w.Write(buffer[:n]); err == nil {
		_, err = io.Copy(w, output)
	}
	if err != nil {
		cancel()
	}
	waitErr := command.Wait()
	if err != nil {
		return true, err
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return true, readErr
	}
	return true, waitErr
}
