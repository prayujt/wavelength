// Package listening records reported listening activity independently of audio delivery.
package listening

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/prayujt/wavelength/internal/library"
)

var (
	ErrInvalid  = errors.New("invalid listening event")
	ErrNotFound = errors.New("listening session not found")
	ErrConflict = errors.New("listening session conflicts with an existing event")
	requestID   = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)
)

type Session struct {
	ID, Owner, RootID             string
	Track                         library.Track
	ArtistID, AlbumID             string
	Origin, Outcome               string
	StartedAt, UpdatedAt, EndedAt time.Time
	ListenedMS, PositionMS        int64
	Playing                       bool
	CountPlay                     bool
}

type Query struct {
	TrackID, ArtistID, AlbumID string
	Origin, Outcome            string
	Since, Until               time.Time
	Limit, Offset              int
}

type Page struct {
	Sessions []Session
	Total    int
}

type Progress struct {
	ListenedMS, PositionMS int64
	Playing                bool
	At                     time.Time
}

type StartOptions struct {
	At        time.Time
	CountPlay bool
}

func eventTime(at time.Time) (time.Time, error) {
	if at.IsZero() {
		return time.Now().UTC(), nil
	}
	if at.Before(time.Unix(0, 0)) || at.After(time.Now().Add(5*time.Minute)) {
		return time.Time{}, ErrInvalid
	}
	return at.UTC(), nil
}

// Update runs its callback in the same transaction that persists the session.
type Store interface {
	Create(context.Context, Session) (Session, error)
	Update(context.Context, string, string, string, func(*Session) error) (Session, error)
	List(context.Context, string, string, Query) (Page, error)
}

type Service struct {
	store   Store
	library *library.Library
	root    string
}

func New(store Store, lib *library.Library) *Service {
	root, _ := lib.SourceRoot()
	return &Service{store: store, library: lib, root: root}
}

func validOrigin(v string) bool  { return v == "direct" || v == "queue" || v == "restored" }
func validOutcome(v string) bool { return v == "completed" || v == "skipped" || v == "stopped" }

// Start uses the client's request ID as the session ID, so retries cannot double count a listen.
func (s *Service) Start(ctx context.Context, owner, trackID, origin, id string, options ...StartOptions) (Session, error) {
	if owner == "" || !requestID.MatchString(id) || !validOrigin(origin) {
		return Session{}, ErrInvalid
	}
	track, err := s.library.Get(ctx, trackID)
	if err != nil {
		return Session{}, err
	}
	group := track.AlbumIdentity()
	track.Path = ""
	var option StartOptions
	if len(options) > 0 {
		option = options[0]
	}
	now, err := eventTime(option.At)
	if err != nil {
		return Session{}, err
	}
	session, err := s.store.Create(ctx, Session{ID: id, Owner: owner, RootID: s.root, Track: track, ArtistID: group.ArtistID, AlbumID: group.AlbumID, Origin: origin, StartedAt: now, UpdatedAt: now, Playing: true, CountPlay: option.CountPlay})
	if err == nil && (session.Track.ID != trackID || session.Origin != origin) {
		return Session{}, ErrConflict
	}
	return session, err
}

func (s *Service) report(ctx context.Context, owner, id string, p Progress, outcome string) (Session, error) {
	at, err := eventTime(p.At)
	if err != nil {
		return Session{}, err
	}
	// Keep counters safely representable by JavaScript clients as well as SQLite.
	const maxMS = int64(1<<53 - 1)
	if owner == "" || !requestID.MatchString(id) || p.ListenedMS < 0 || p.PositionMS < 0 || p.ListenedMS > maxMS || p.PositionMS > maxMS {
		return Session{}, ErrInvalid
	}
	return s.store.Update(ctx, owner, s.root, id, func(session *Session) error {
		if at.Before(session.StartedAt) {
			return ErrInvalid
		}
		if session.Outcome != "" {
			if outcome != "" && outcome != session.Outcome {
				return ErrConflict
			}
			return nil
		}
		// Delayed progress must not move cumulative listening backwards.
		if p.ListenedMS < session.ListenedMS {
			if outcome == "" {
				return nil
			}
		} else {
			session.ListenedMS, session.PositionMS = p.ListenedMS, p.PositionMS
		}
		if at.After(session.UpdatedAt) {
			session.UpdatedAt = at
		}
		session.Playing = p.Playing
		if outcome != "" {
			session.Outcome, session.EndedAt, session.Playing = outcome, session.UpdatedAt, false
		}
		return nil
	})
}

func (s *Service) Report(ctx context.Context, owner, id string, p Progress) (Session, error) {
	return s.report(ctx, owner, id, p, "")
}
func (s *Service) Finish(ctx context.Context, owner, id, outcome string, p Progress) (Session, error) {
	if !validOutcome(outcome) {
		return Session{}, ErrInvalid
	}
	return s.report(ctx, owner, id, p, outcome)
}
func (s *Service) List(ctx context.Context, owner string, q Query) (Page, error) {
	if owner == "" || q.Limit < 0 || q.Limit > 500 || q.Offset < 0 || (!q.Since.IsZero() && !q.Until.IsZero() && !q.Since.Before(q.Until)) || (q.Origin != "" && !validOrigin(q.Origin)) || (q.Outcome != "" && !validOutcome(q.Outcome)) {
		return Page{}, ErrInvalid
	}
	if q.Limit == 0 {
		q.Limit = 100
	}
	return s.store.List(ctx, owner, s.root, q)
}
