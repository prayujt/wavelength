// Package podcasts manages subscriptions and downloaded episodes independently of clients.
package podcasts

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/prayujt/wavelength/internal/state"
)

type Service struct {
	store     state.Store
	directory string
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	closed    bool
	workers   sync.WaitGroup
}

func New(ctx context.Context, store state.Store, directory string) (*Service, error) {
	if directory != "" {
		if err := os.MkdirAll(directory, 0700); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	service := &Service{store: store, directory: directory, ctx: ctx, cancel: cancel}
	go func() { <-ctx.Done(); service.Close() }()
	return service, nil
}

// Close stops downloads before their store and files are closed by the application.
func (s *Service) Close() { s.mu.Lock(); s.closed = true; s.cancel(); s.mu.Unlock(); s.workers.Wait() }

func id(value string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(value))) }

func (s *Service) Subscribe(ctx context.Context, address string) error {
	if _, err := validSource(address); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	channel := Channel{ID: id("podcast\x00" + address), URL: address, Status: "new", Episodes: []Episode{}}
	if err := s.store.Update(ctx, "podcasts", channel.ID, func(raw json.RawMessage) (json.RawMessage, error) {
		if len(raw) > 0 {
			return raw, nil
		}
		return json.Marshal(channel)
	}); err != nil {
		return err
	}
	return s.refreshChannel(ctx, channel)
}

var ErrInvalid = errors.New("invalid podcast request")

func (s *Service) Refresh(ctx context.Context) error {
	channels, err := s.Channels(ctx)
	if err != nil {
		return err
	}
	for _, channel := range channels {
		if err := s.refreshChannel(ctx, channel); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) DeleteChannel(ctx context.Context, id string) error {
	var removed Channel
	if err := s.store.Update(ctx, "podcasts", id, func(raw json.RawMessage) (json.RawMessage, error) {
		if len(raw) == 0 {
			return nil, state.ErrNotFound
		}
		if err := json.Unmarshal(raw, &removed); err != nil {
			return nil, err
		}
		return nil, nil
	}); err != nil {
		return err
	}
	for _, episode := range removed.Episodes {
		s.removeFile(episode.ID)
	}
	return nil
}

func (s *Service) Episode(ctx context.Context, id string) (string, Episode, error) {
	channels, err := s.Channels(ctx)
	if err != nil {
		return "", Episode{}, err
	}
	for _, channel := range channels {
		for _, episode := range channel.Episodes {
			if episode.ID == id {
				return channel.ID, episode, nil
			}
		}
	}
	return "", Episode{}, state.ErrNotFound
}

func (s *Service) DeleteEpisode(ctx context.Context, id string) error {
	channel, _, err := s.Episode(ctx, id)
	if err != nil {
		return err
	}
	if err := s.updateEpisode(ctx, channel, id, func(e *Episode) { e.Status = "deleted"; e.Error = "" }); err != nil {
		return err
	}
	s.removeFile(id)
	return nil
}

func (s *Service) removeFile(id string) {
	if s.directory != "" {
		_ = os.Remove(filepath.Join(s.directory, id+".media"))
	}
}

func (s *Service) Download(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return context.Canceled
	}
	if s.directory == "" {
		return errors.New("podcast storage is not configured")
	}
	channel, episode, err := s.Episode(ctx, id)
	if err != nil {
		return err
	}
	claimed := false
	if err := s.updateEpisode(ctx, channel, id, func(e *Episode) {
		if e.Status != "completed" && e.Status != "downloading" {
			e.Status = "downloading"
			e.Error = ""
			claimed = true
		}
	}); err != nil {
		return err
	}
	if claimed {
		s.workers.Add(1)
		go func() { defer s.workers.Done(); s.downloadEpisode(channel, episode) }()
	}
	return nil
}

func (s *Service) Open(ctx context.Context, id string) (*os.File, Episode, error) {
	_, episode, err := s.Episode(ctx, id)
	if err != nil {
		return nil, Episode{}, err
	}
	if episode.Status != "completed" {
		return nil, Episode{}, state.ErrNotFound
	}
	root, err := os.OpenRoot(s.directory)
	if err != nil {
		return nil, Episode{}, err
	}
	defer root.Close()
	file, err := root.Open(id + ".media")
	return file, episode, err
}
