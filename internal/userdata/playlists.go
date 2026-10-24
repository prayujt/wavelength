package userdata

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/prayujt/wavelength/internal/state"
)

type Playlist struct {
	ID, Name, Comment, Owner, Created, Changed string
	Public                                     bool
	IDs                                        []string
}

type PlaylistChanges struct {
	Name, Comment *string
	Public        *bool
	Replace       *[]string
	Add           []string
	Remove        []int
}

func (s *Service) Playlists(ctx context.Context, user, owner string, admin bool) ([]Playlist, error) {
	if owner != "" && !admin {
		return nil, ErrForbidden
	}
	records, err := state.ReadAll[Playlist](ctx, s.store, "playlists")
	if err != nil {
		return nil, err
	}
	items := []Playlist{}
	for _, p := range records {
		if owner != "" && p.Owner != owner {
			continue
		}
		if p.Owner != user && !p.Public && owner == "" {
			continue
		}
		items = append(items, p)
	}
	slices.SortFunc(items, func(a, b Playlist) int { return strings.Compare(a.Name, b.Name) })
	return items, nil
}

func (s *Service) Playlist(ctx context.Context, user, id string) (Playlist, error) {
	p, err := state.Read[Playlist](ctx, s.store, "playlists", id)
	if err != nil {
		return p, err
	}
	if p.Owner != user && !p.Public {
		return Playlist{}, ErrForbidden
	}
	return p, nil
}

func (s *Service) CreatePlaylist(ctx context.Context, user, name string, ids []string) (Playlist, error) {
	if name == "" {
		return Playlist{}, ErrInvalid
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	p := Playlist{ID: rand.Text(), Name: name, Owner: user, Created: now, Changed: now, IDs: []string{}}
	for _, id := range ids {
		if _, err := s.tracks.Get(ctx, id); err != nil {
			return Playlist{}, err
		}
		p.IDs = append(p.IDs, id)
	}
	return p, state.Write(ctx, s.store, "playlists", p.ID, p)
}

func (s *Service) UpdatePlaylist(ctx context.Context, user, id string, change PlaylistChanges) (Playlist, error) {
	ids := change.Add
	if change.Replace != nil {
		ids = *change.Replace
	}
	for _, id := range ids {
		if _, err := s.tracks.Get(ctx, id); err != nil {
			return Playlist{}, err
		}
	}
	var result Playlist
	err := state.Modify(ctx, s.store, "playlists", id, func(p *Playlist, exists bool) error {
		if !exists {
			return state.ErrNotFound
		}
		if p.Owner != user {
			return ErrForbidden
		}
		if change.Name != nil {
			if *change.Name == "" {
				return ErrInvalid
			}
			p.Name = *change.Name
		}
		if change.Comment != nil {
			p.Comment = *change.Comment
		}
		if change.Public != nil {
			p.Public = *change.Public
		}
		if change.Replace != nil {
			p.IDs = append([]string{}, (*change.Replace)...)
		} else {
			for _, index := range change.Remove {
				if index < 0 || index >= len(p.IDs) {
					return ErrInvalid
				}
			}
			kept := []string{}
			for index, id := range p.IDs {
				if !slices.Contains(change.Remove, index) {
					kept = append(kept, id)
				}
			}
			p.IDs = append(kept, change.Add...)
		}
		p.Changed = time.Now().UTC().Format(time.RFC3339Nano)
		result = *p
		return nil
	})
	return result, err
}

func (s *Service) DeletePlaylist(ctx context.Context, user, id string) error {
	return s.store.Update(ctx, "playlists", id, func(raw json.RawMessage) (json.RawMessage, error) {
		if len(raw) == 0 {
			return nil, state.ErrNotFound
		}
		var p Playlist
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		if p.Owner != user {
			return nil, ErrForbidden
		}
		return nil, nil
	})
}
