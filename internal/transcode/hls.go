package transcode

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/prayujt/wavelength/internal/library"
)

const hlsLifetime = 2 * time.Hour

var segmentName = regexp.MustCompile(`^segment[0-9]+\.ts$`)

type HLSOptions struct{ BitRate int }
type Playlist struct {
	SessionID string
	Body      []byte
}
type jobKey struct {
	owner, track string
	source       string
	bitrate      int
}
type hlsJob struct {
	id, owner, dir string
	key            jobKey
	expires        time.Time
	cancel         context.CancelFunc
	timer          *time.Timer
	done           chan struct{}
	err            error
}

func (s *Service) expire(id string) {
	s.mu.Lock()
	job := s.jobs[id]
	delete(s.jobs, id)
	s.mu.Unlock()
	if job != nil {
		job.cancel()
		<-job.done
		_ = os.RemoveAll(job.dir)
	}
}

func (s *Service) HLS(ctx context.Context, owner, id string, options HLSOptions) (Playlist, error) {
	if options.BitRate < 16 || options.BitRate > 100000 {
		return Playlist{}, ErrInvalid
	}
	track, err := s.lib.Get(ctx, id)
	if err != nil {
		return Playlist{}, err
	}
	key := jobKey{owner: owner, track: id, source: track.Path, bitrate: options.BitRate}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Playlist{}, context.Canceled
	}
	var job *hlsJob
	for _, candidate := range s.jobs {
		if candidate.key == key && time.Now().Before(candidate.expires) {
			job = candidate
			break
		}
	}
	if job == nil {
		if len(s.jobs) >= 8 {
			s.mu.Unlock()
			return Playlist{}, errors.New("HLS session capacity reached")
		}
		job, err = s.startHLS(ctx, key)
		if err != nil {
			s.mu.Unlock()
			return Playlist{}, err
		}
		s.jobs[job.id] = job
		job.timer = time.AfterFunc(hlsLifetime, func() { s.expire(job.id) })
	}
	s.mu.Unlock()
	return waitPlaylist(ctx, job)
}

func (s *Service) startHLS(request context.Context, key jobKey) (*hlsJob, error) {
	source, _, err := s.lib.OpenTrack(request, key.track)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	directory, err := os.MkdirTemp("", "wavelength-hls-")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(s.ctx, hlsLifetime)
	args := []string{"-v", "error", "-nostdin", "-i", "/dev/fd/3"}
	args = append(args, "-map", "0:a:0", "-vn", "-c:a", "aac", "-b:a", strconv.Itoa(key.bitrate)+"k")
	args = append(args, "-f", "hls", "-hls_time", "6", "-hls_playlist_type", "vod", "-hls_segment_filename", filepath.Join(directory, "segment%06d.ts"), filepath.Join(directory, "index.m3u8"))
	command := exec.CommandContext(ctx, "ffmpeg", args...)
	command.ExtraFiles = []*os.File{source}
	if err := command.Start(); err != nil {
		cancel()
		_ = os.RemoveAll(directory)
		return nil, err
	}
	job := &hlsJob{
		id:      rand.Text(),
		owner:   key.owner,
		key:     key,
		dir:     directory,
		expires: time.Now().Add(hlsLifetime),
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	go func() {
		job.err = command.Wait()
		close(job.done)
	}()
	return job, nil
}

func waitPlaylist(ctx context.Context, job *hlsJob) (Playlist, error) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(30 * time.Second)
	defer timeout.Stop()
	read := func() (Playlist, error) {
		body, err := os.ReadFile(filepath.Join(job.dir, "index.m3u8"))
		if err != nil {
			return Playlist{}, err
		}
		if len(body) == 0 {
			return Playlist{}, errors.New("empty HLS playlist")
		}
		return Playlist{SessionID: job.id, Body: body}, nil
	}
	for {
		if playlist, err := read(); err == nil {
			return playlist, nil
		}
		select {
		case <-job.done:
			if job.err != nil {
				return Playlist{}, fmt.Errorf("HLS conversion: %w", job.err)
			}
			return read()
		case <-ctx.Done():
			return Playlist{}, ctx.Err()
		case <-timeout.C:
			return Playlist{}, errors.New("HLS playlist is not ready")
		case <-ticker.C:
		}
	}
}

func (s *Service) Segment(owner, id, name string) (*os.File, error) {
	if !segmentName.MatchString(name) {
		return nil, ErrInvalid
	}
	s.mu.Lock()
	job := s.jobs[id]
	if job == nil || job.owner != owner || time.Now().After(job.expires) {
		s.mu.Unlock()
		return nil, library.ErrNotFound
	}
	file, err := os.Open(filepath.Join(job.dir, name))
	s.mu.Unlock()
	return file, err
}

// IsSegment identifies the filenames emitted by an HLS session.
func IsSegment(name string) bool { return segmentName.MatchString(name) }
