// Package sharing owns expiring, revocable grants to selected library tracks.
package sharing

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/state"
)

var ErrForbidden = errors.New("share operation is not permitted")
var ErrInvalid = errors.New("invalid share settings")

type Share struct {
	ID, Owner, Description, Created, LastVisited string
	Expires                                      int64
	VisitCount                                   int64
	IDs                                          []string
}

type Changes struct {
	Description *string
	Expires     *int64
}
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

func (s *Service) List(ctx context.Context, user string, admin bool) ([]Share, error) {
	records, err := state.ReadAll[Share](ctx, s.store, "shares")
	if err != nil {
		return nil, err
	}
	items := []Share{}
	for _, share := range records {
		if share.Owner == user || admin {
			items = append(items, share)
		}
	}
	slices.SortFunc(items, func(a, b Share) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return items, nil
}

func (s *Service) Create(ctx context.Context, user, description string, expires int64, ids []string) (Share, error) {
	if len(ids) == 0 || expires < 0 {
		return Share{}, ErrInvalid
	}
	for _, id := range ids {
		if _, err := s.tracks.Get(ctx, id); err != nil {
			return Share{}, err
		}
	}
	share := Share{
		ID:          rand.Text(),
		Owner:       user,
		Description: description,
		Expires:     expires,
		Created:     time.Now().UTC().Format(time.RFC3339),
		IDs:         slices.Clone(ids),
	}
	return share, state.Write(ctx, s.store, "shares", share.ID, share)
}

func (s *Service) Update(ctx context.Context, user, id string, admin bool, changes Changes) error {
	if changes.Expires != nil && *changes.Expires < 0 {
		return ErrInvalid
	}
	return state.Modify(ctx, s.store, "shares", id, func(share *Share, exists bool) error {
		if !exists {
			return state.ErrNotFound
		}
		if share.Owner != user && !admin {
			return ErrForbidden
		}
		if changes.Description != nil {
			share.Description = *changes.Description
		}
		if changes.Expires != nil {
			share.Expires = *changes.Expires
		}
		return nil
	})
}

func (s *Service) Delete(ctx context.Context, user, id string, admin bool) error {
	return s.store.Update(ctx, "shares", id, func(raw json.RawMessage) (json.RawMessage, error) {
		if len(raw) == 0 {
			return nil, state.ErrNotFound
		}
		var share Share
		if err := json.Unmarshal(raw, &share); err != nil {
			return nil, err
		}
		if share.Owner != user && !admin {
			return nil, ErrForbidden
		}
		return nil, nil
	})
}

func (s *Service) Resolve(ctx context.Context, id string) (Share, error) {
	share, err := state.Read[Share](ctx, s.store, "shares", id)
	if err != nil {
		return Share{}, err
	}
	if share.Expires > 0 && time.Now().UnixMilli() >= share.Expires {
		return Share{}, state.ErrNotFound
	}
	return share, nil
}

func (s *Service) Visit(ctx context.Context, id string) error {
	return state.Modify(ctx, s.store, "shares", id, func(share *Share, exists bool) error {
		if !exists || share.Expires > 0 && time.Now().UnixMilli() >= share.Expires {
			return state.ErrNotFound
		}
		share.VisitCount++
		share.LastVisited = time.Now().UTC().Format(time.RFC3339)
		return nil
	})
}

func (s *Service) Authorize(ctx context.Context, shareID, trackID string) error {
	share, err := s.Resolve(ctx, shareID)
	if err != nil {
		return err
	}
	if !slices.Contains(share.IDs, trackID) {
		return ErrForbidden
	}
	return nil
}
