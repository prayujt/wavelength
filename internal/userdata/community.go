package userdata

import (
	"context"
	"crypto/rand"
	"slices"
	"strings"
	"time"

	"github.com/prayujt/wavelength/internal/state"
)

type Station struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	StreamURL   string `json:"streamUrl"`
	HomepageURL string `json:"homePageUrl"`
}

type Message struct {
	Username string `json:"username"`
	Time     int64  `json:"time"`
	Message  string `json:"message"`
}

func (s *Service) Stations(ctx context.Context) ([]Station, error) {
	records, err := state.ReadAll[Station](ctx, s.store, "radio")
	if err != nil {
		return nil, err
	}
	items := []Station{}
	for _, station := range records {
		items = append(items, station)
	}
	slices.SortFunc(items, func(a, b Station) int { return strings.Compare(a.ID, b.ID) })
	return items, nil
}

func (s *Service) SaveStation(ctx context.Context, station Station) error {
	if station.Name == "" || station.StreamURL == "" {
		return ErrInvalid
	}
	if station.ID == "" {
		station.ID = rand.Text()
	} else {
		if _, err := state.Read[Station](ctx, s.store, "radio", station.ID); err != nil {
			return err
		}
	}
	return state.Write(ctx, s.store, "radio", station.ID, station)
}

func (s *Service) DeleteStation(ctx context.Context, id string) error {
	if _, err := state.Read[Station](ctx, s.store, "radio", id); err != nil {
		return err
	}
	return state.Delete(ctx, s.store, "radio", id)
}

func (s *Service) AddMessage(ctx context.Context, user, message string) error {
	if message == "" || len(message) > 8192 {
		return ErrInvalid
	}
	return state.Write(ctx, s.store, "chat", rand.Text(), Message{Username: user, Time: time.Now().UnixMilli(), Message: message})
}

func (s *Service) Messages(ctx context.Context, since int64) ([]Message, error) {
	if since < 0 {
		return nil, ErrInvalid
	}
	records, err := state.ReadAll[Message](ctx, s.store, "chat")
	if err != nil {
		return nil, err
	}
	items := []Message{}
	for _, message := range records {
		if message.Time > since {
			items = append(items, message)
		}
	}
	slices.SortFunc(items, func(a, b Message) int {
		if a.Time < b.Time {
			return -1
		}
		if a.Time > b.Time {
			return 1
		}
		return 0
	})
	return items, nil
}
