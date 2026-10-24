// Package transcode provides encoded streams and managed HLS sessions.
package transcode

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/prayujt/wavelength/internal/library"
)

var ErrInvalid = errors.New("invalid encoding options")

type source interface {
	Get(context.Context, string) (library.Track, error)
	OpenTrack(context.Context, string) (*os.File, library.Track, error)
	MediaStreams(context.Context, string) ([]library.MediaStream, error)
	CaptionSidecar(context.Context, string) ([]byte, string, error)
}

type Service struct {
	lib       source
	directory string
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	closed    bool
	jobs      map[string]*hlsJob
	closeOnce sync.Once
	done      chan struct{}
}

func New(ctx context.Context, lib source, directory string) (*Service, error) {
	if directory != "" {
		if err := os.MkdirAll(directory, 0700); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	service := &Service{lib: lib, directory: directory, ctx: ctx, cancel: cancel, jobs: map[string]*hlsJob{}, done: make(chan struct{})}
	go func() { <-ctx.Done(); service.Close() }()
	return service, nil
}

func (s *Service) Close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.cancel()
		jobs := s.jobs
		s.jobs = map[string]*hlsJob{}
		s.mu.Unlock()
		for _, job := range jobs {
			job.timer.Stop()
			job.cancel()
			<-job.done
			_ = os.RemoveAll(job.dir)
		}
		close(s.done)
	})
	<-s.done
}

// Stream owns a running encoder. Close always reaps the child process.
type Stream struct {
	io.ReadCloser
	ContentType string
	command     *exec.Cmd
	cancel      context.CancelFunc
	once        sync.Once
	err         error
}

func (s *Stream) Wait() error  { s.once.Do(func() { s.err = s.command.Wait() }); return s.err }
func (s *Stream) Close() error { s.cancel(); _ = s.ReadCloser.Close(); return s.Wait() }

func start(ctx context.Context, file *os.File, args []string, kind string) (*Stream, error) {
	defer file.Close()
	ctx, cancel := context.WithCancel(ctx)
	command := exec.CommandContext(ctx, "ffmpeg", args...)
	command.ExtraFiles = []*os.File{file}
	output, err := command.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := command.Start(); err != nil {
		cancel()
		_ = output.Close()
		return nil, err
	}
	return &Stream{ReadCloser: output, ContentType: kind, command: command, cancel: cancel}, nil
}
