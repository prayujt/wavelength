package subsonic

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type hlsJob struct {
	ID, Owner, Dir, Key string
	Expires             time.Time
	Cancel              context.CancelFunc
	Done                chan struct{}
	Err                 error
}
type hlsState struct {
	mu   sync.Mutex
	jobs map[string]*hlsJob
}

var segmentName = regexp.MustCompile(`^segment[0-9]+\.ts$`)

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
			rate, err := strconv.Atoi(value)
			if err != nil || rate < 16 || rate > 100000 {
				failure(result, 10, "Invalid HLS bitrate")
				return true
			}
			query := url.Values{}
			for key, values := range r.Form {
				query[key] = append([]string{}, values...)
			}
			query.Set("bitRate", value)
			_, _ = fmt.Fprintf(&master, "#EXT-X-STREAM-INF:BANDWIDTH=%d\n/rest/hls.m3u8?%s\n", rate*1000, query.Encode())
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		if r.Method != "HEAD" {
			_, _ = fmt.Fprint(w, master.String())
		}
		result.Status = "streamed"
		return true
	}

	if endpoint == "hlsSegment" {
		key := required(r, result, "id")
		name := r.Form.Get("name")
		if result.Error != nil {
			return true
		}
		if !segmentName.MatchString(name) {
			failure(result, 10, "Invalid HLS segment")
			return true
		}
		a.hlsState.mu.Lock()
		job, ok := a.hlsState.jobs[key]
		a.hlsState.mu.Unlock()
		if !ok || job.Owner != user || time.Now().After(job.Expires) {
			failure(result, 70, "HLS session not found")
			return true
		}
		file, err := os.Open(filepath.Join(job.Dir, name))
		if err != nil {
			failure(result, 70, "HLS segment not found")
			return true
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			legacyError(result, err)
			return true
		}
		w.Header().Set("Content-Type", "video/mp2t")
		http.ServeContent(w, r, name, info.ModTime(), file)
		result.Status = "streamed"
		return true
	}
	trackID := required(r, result, "id")
	if result.Error != nil {
		return true
	}
	bitrate, err := number(r, "bitRate", 192, 100000)
	if err != nil || bitrate < 16 {
		failure(result, 10, "Invalid HLS bitrate")
		return true
	}
	if _, err = exec.LookPath("ffmpeg"); err != nil {
		failure(result, 0, "HLS requires ffmpeg")
		return true
	}
	key := user + "/" + trackID + "/" + strconv.Itoa(bitrate) + "/" + r.Form.Get("size") + "/" + r.Form.Get("audioTrack")
	a.hlsState.mu.Lock()
	if a.hlsState.jobs == nil {
		a.hlsState.jobs = map[string]*hlsJob{}
	}
	var job *hlsJob
	for id, candidate := range a.hlsState.jobs {
		if time.Now().After(candidate.Expires) {
			candidate.Cancel()
			<-candidate.Done
			os.RemoveAll(candidate.Dir)
			delete(a.hlsState.jobs, id)
			continue
		}
		if candidate.Key == key {
			job = candidate
		}
	}
	if job == nil {
		if len(a.hlsState.jobs) >= 8 {
			a.hlsState.mu.Unlock()
			failure(result, 0, "HLS session capacity reached; retry later")
			return true
		}
		source, _, err := a.lib.OpenTrack(r.Context(), trackID)
		if err != nil {
			a.hlsState.mu.Unlock()
			legacyError(result, err)
			return true
		}
		dir, err := os.MkdirTemp("", "wavelength-hls-")
		if err != nil {
			source.Close()
			a.hlsState.mu.Unlock()
			legacyError(result, err)
			return true
		}
		ctx, cancel := context.WithTimeout(a.ctx, 2*time.Hour)
		job = &hlsJob{ID: rand.Text(), Owner: user, Dir: dir, Key: key, Expires: time.Now().Add(2 * time.Hour), Cancel: cancel, Done: make(chan struct{})}
		args := []string{"-v", "error", "-nostdin", "-i", "/dev/fd/3"}
		track, err := a.lib.Get(r.Context(), trackID)
		if err != nil {
			source.Close()
			cancel()
			os.RemoveAll(dir)
			a.hlsState.mu.Unlock()
			legacyError(result, err)
			return true
		}
		if strings.HasPrefix(track.ContentType, "video/") {
			size, err := scaleArgument(r.Form.Get("size"))
			if err != nil {
				source.Close()
				cancel()
				os.RemoveAll(dir)
				a.hlsState.mu.Unlock()
				failure(result, 10, err.Error())
				return true
			}
			audioMap := "0:a:0?"
			if requested := r.Form.Get("audioTrack"); requested != "" {
				index, err := strconv.Atoi(requested)
				if err != nil || index < 0 {
					source.Close()
					cancel()
					os.RemoveAll(dir)
					a.hlsState.mu.Unlock()
					failure(result, 10, "Invalid audioTrack")
					return true
				}
				streams, err := a.lib.MediaStreams(r.Context(), trackID)
				if err != nil {
					source.Close()
					cancel()
					os.RemoveAll(dir)
					a.hlsState.mu.Unlock()
					failure(result, 0, "Cannot inspect audio tracks")
					return true
				}
				found := false
				for _, stream := range streams {
					found = found || (stream.Type == "audio" && stream.Index == index)
				}
				if !found {
					source.Close()
					cancel()
					os.RemoveAll(dir)
					a.hlsState.mu.Unlock()
					failure(result, 70, "Audio track not found")
					return true
				}
				audioMap = "0:" + strconv.Itoa(index)
			}
			args = append(args, "-map", "0:v:0", "-map", audioMap, "-c:v", "libx264", "-preset", "veryfast", "-pix_fmt", "yuv420p", "-b:v", strconv.Itoa(bitrate)+"k", "-c:a", "aac", "-b:a", "128k")
			if size != "" {
				args = append(args, "-vf", "scale="+size)
			}
		} else {
			args = append(args, "-map", "0:a:0", "-vn", "-c:a", "aac", "-b:a", strconv.Itoa(bitrate)+"k")
		}
		args = append(args, "-f", "hls", "-hls_time", "6", "-hls_playlist_type", "vod", "-hls_segment_filename", filepath.Join(dir, "segment%06d.ts"), filepath.Join(dir, "index.m3u8"))
		command := exec.CommandContext(ctx, "ffmpeg", args...)
		command.ExtraFiles = []*os.File{source}
		if err = command.Start(); err != nil {
			cancel()
			source.Close()
			os.RemoveAll(dir)
			a.hlsState.mu.Unlock()
			failure(result, 0, "Could not start HLS transcoder")
			return true
		}
		source.Close()
		a.hlsState.jobs[job.ID] = job
		go func(j *hlsJob) {
			j.Err = command.Wait()
			close(j.Done)
			select {
			case <-a.ctx.Done():
				j.Cancel()
				os.RemoveAll(j.Dir)
			case <-ctx.Done():
				os.RemoveAll(j.Dir)
			}
		}(job)
	}
	a.hlsState.mu.Unlock()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	var playlist []byte
	for {
		playlist, err = os.ReadFile(filepath.Join(job.Dir, "index.m3u8"))
		if err == nil && len(playlist) > 0 {
			break
		}
		select {
		case <-job.Done:
			if job.Err != nil {
				failure(result, 0, "HLS conversion failed")
				return true
			}
			// The previous ReadFile may have raced the final playlist rename.
			playlist, err = os.ReadFile(filepath.Join(job.Dir, "index.m3u8"))
			if err != nil || len(playlist) == 0 {
				failure(result, 0, "HLS playlist unavailable")
				return true
			}
			goto playlistReady
		case <-r.Context().Done():
			failure(result, 0, "HLS request cancelled")
			return true
		case <-deadline.C:
			failure(result, 0, "HLS playlist is not ready")
			return true
		case <-ticker.C:
		}
	}
playlistReady:
	lines := strings.Split(string(playlist), "\n")
	for i, line := range lines {
		if segmentName.MatchString(line) {
			query := url.Values{"id": {job.ID}, "name": {line}, "u": {user}, "v": {r.Form.Get("v")}, "c": {r.Form.Get("c")}}
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
		_, err = fmt.Fprint(w, strings.Join(lines, "\n"))
		if err != nil && !errors.Is(err, context.Canceled) {
		}
	}
	result.Status = "streamed"
	return true
}
