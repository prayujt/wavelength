// Package userdata owns personal collections and listening state across transports.
package userdata

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/state"
)

var (
	ErrInvalid   = errors.New("invalid input")
	ErrForbidden = errors.New("operation is not permitted")
)

type trackReader interface {
	Get(context.Context, string) (library.Track, error)
}

type Service struct {
	store  state.Store
	tracks trackReader
}

func New(store state.Store, tracks trackReader) *Service {
	return &Service{store: store, tracks: tracks}
}

type History struct {
	Plays      int    `json:"plays"`
	LastPlayed string `json:"last_played,omitempty"`
}

type Bookmark struct {
	ID, Comment, Created, Changed string
	Position                      int64
}
type Queue struct {
	IDs                         []string
	Current, Changed, ChangedBy string
	Position                    int64
}
type Playing struct {
	ID, Username, Player string
	Time                 time.Time
}

// History reads listening counts from the existing scope; legacy favorites are ignored.
func (s *Service) History(ctx context.Context, user string) (map[string]History, error) {
	return state.ReadAll[History](ctx, s.store, "annotations/"+user)
}

func (s *Service) RecordPlay(ctx context.Context, user, client, id string, stamp time.Time, submitted bool) error {
	if _, err := s.tracks.Get(ctx, id); err != nil {
		return err
	}
	if !submitted {
		return state.Write(ctx, s.store, "now-playing", user+"/"+client, Playing{ID: id, Username: user, Player: client, Time: stamp})
	}
	return state.Modify(ctx, s.store, "annotations/"+user, id, func(a *History, _ bool) error {
		a.Plays++
		value := stamp.UTC().Format(time.RFC3339Nano)
		if value > a.LastPlayed {
			a.LastPlayed = value
		}
		return nil
	})
}

func (s *Service) NowPlaying(ctx context.Context) ([]Playing, error) {
	records, err := state.ReadAll[Playing](ctx, s.store, "now-playing")
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	items := []Playing{}
	for _, key := range keys {
		p := records[key]
		if time.Since(p.Time) <= 10*time.Minute {
			items = append(items, p)
		}
	}
	return items, nil
}

func (s *Service) Bookmarks(ctx context.Context, user string) ([]Bookmark, error) {
	records, err := state.ReadAll[Bookmark](ctx, s.store, "bookmarks/"+user)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	items := []Bookmark{}
	for _, key := range keys {
		items = append(items, records[key])
	}
	return items, nil
}

func (s *Service) SaveBookmark(ctx context.Context, user string, b Bookmark) error {
	if b.Position < 0 {
		return ErrInvalid
	}
	if _, err := s.tracks.Get(ctx, b.ID); err != nil {
		return err
	}
	return state.Modify(ctx, s.store, "bookmarks/"+user, b.ID, func(old *Bookmark, exists bool) error {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		b.Changed = now
		b.Created = now
		if exists {
			b.Created = old.Created
		}
		*old = b
		return nil
	})
}

func (s *Service) DeleteBookmark(ctx context.Context, user, id string) error {
	return state.Delete(ctx, s.store, "bookmarks/"+user, id)
}
func (s *Service) Queue(ctx context.Context, user string) (Queue, error) {
	return state.Read[Queue](ctx, s.store, "queues", user)
}
func (s *Service) SaveQueue(ctx context.Context, user string, q Queue) error {
	if q.Position < 0 || q.Current != "" && !slices.Contains(q.IDs, q.Current) {
		return ErrInvalid
	}
	for _, id := range q.IDs {
		if _, err := s.tracks.Get(ctx, id); err != nil {
			return err
		}
	}
	q.Changed = time.Now().UTC().Format(time.RFC3339Nano)
	return state.Write(ctx, s.store, "queues", user, q)
}
