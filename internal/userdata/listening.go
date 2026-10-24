package userdata

import (
	"context"
	"slices"
	"strings"
	"time"
)

type HistoryEntry struct {
	ID string
	History
}

// ListeningHistory has a deterministic latest-first order for every native transport.
func (s *Service) ListeningHistory(ctx context.Context, user string) ([]HistoryEntry, error) {
	records, err := s.History(ctx, user)
	if err != nil {
		return nil, err
	}
	items := []HistoryEntry{}
	for id, history := range records {
		items = append(items, HistoryEntry{ID: id, History: history})
	}
	slices.SortFunc(items, func(a, b HistoryEntry) int {
		x, _ := time.Parse(time.RFC3339Nano, a.LastPlayed)
		y, _ := time.Parse(time.RFC3339Nano, b.LastPlayed)
		if n := y.Compare(x); n != 0 {
			return n
		}
		return strings.Compare(a.ID, b.ID)
	})
	return items, nil
}

// PersonalNowPlaying never exposes another listener's activity or player identity.
func (s *Service) PersonalNowPlaying(ctx context.Context, user string) ([]Playing, error) {
	all, err := s.NowPlaying(ctx)
	if err != nil {
		return nil, err
	}
	items := []Playing{}
	for _, p := range all {
		if p.Username == user {
			items = append(items, p)
		}
	}
	return items, nil
}

func ValidatePlayTime(stamp time.Time) error {
	if stamp.Before(time.Unix(0, 0)) || stamp.After(time.Now().Add(5*time.Minute)) {
		return ErrInvalid
	}
	return nil
}
