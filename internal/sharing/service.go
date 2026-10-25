// Package sharing owns revocable access to selected audio, independent of transport.
package sharing

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/prayujt/wavelength/internal/library"
)

var (
	ErrInvalid     = errors.New("invalid link parameters")
	ErrNotFound    = errors.New("link not found or no longer available")
	ErrUnavailable = errors.New("a public URL is required for playback and sharing links")
)

const (
	Share              = "share"
	Playback           = "playback"
	DefaultPlaybackTTL = 3 * time.Hour
	DefaultShareTTL    = 30 * 24 * time.Hour
	MaxPlaybackTTL     = 24 * time.Hour
)

type Grant struct {
	ID, TokenHash, Owner, RootID, Kind, Name, ParentID string
	TrackIDs                                           []string
	CreatedAt, ExpiresAt                               time.Time
}

type Store interface {
	Create(context.Context, Grant) error
	Get(context.Context, string) (Grant, error)
	ByTokenHash(context.Context, string) (Grant, error)
	List(context.Context, string, string, string, int, int) ([]Grant, error)
	Revoke(context.Context, string, string, string) error
}

type Service struct {
	store           Store
	library         *library.Library
	rootID, baseURL string
	now             func() time.Time
}

func New(store Store, lib *library.Library, publicURL string) *Service {
	rootID, _ := lib.SourceRoot()
	return &Service{store: store, library: lib, rootID: rootID, baseURL: strings.TrimRight(publicURL, "/"), now: time.Now}
}

type Link struct {
	Grant Grant
	URL   string
}

type ShareRequest struct {
	Name      string
	TrackIDs  []string
	ExpiresAt time.Time
	NoExpiry  bool
}

// CreateShare captures an ordered snapshot; later playlist edits do not widen access.
func (s *Service) CreateShare(ctx context.Context, owner string, request ShareRequest) (Link, error) {
	name, ids, expires := strings.TrimSpace(request.Name), request.TrackIDs, request.ExpiresAt
	if request.NoExpiry && !expires.IsZero() {
		return Link{}, ErrInvalid
	}
	if expires.IsZero() && !request.NoExpiry {
		expires = s.now().Add(DefaultShareTTL)
	}
	if owner == "" || len(name) > 255 || len(ids) == 0 || len(ids) > 1000 || !expires.IsZero() && !expires.After(s.now()) {
		return Link{}, ErrInvalid
	}
	ids = slices.Clone(ids)
	for i, id := range ids {
		t, err := s.library.Get(ctx, id)
		if err != nil {
			return Link{}, err
		}
		if i == 0 && name == "" {
			name = t.Title
		}
	}
	return s.create(ctx, Grant{Owner: owner, Kind: Share, Name: name, TrackIDs: ids, ExpiresAt: expires}, "")
}

func (s *Service) CreatePlayback(ctx context.Context, owner, id string, ttl time.Duration) (Link, error) {
	if owner == "" || id == "" {
		return Link{}, ErrInvalid
	}
	return s.playback(ctx, owner, id, ttl, Grant{})
}
func (s *Service) playback(ctx context.Context, owner, id string, ttl time.Duration, parent Grant) (Link, error) {
	if ttl == 0 {
		ttl = DefaultPlaybackTTL
	}
	if ttl < time.Minute || ttl > MaxPlaybackTTL {
		return Link{}, ErrInvalid
	}
	t, err := s.library.Get(ctx, id)
	if err != nil {
		return Link{}, err
	}
	expires := s.now().Add(ttl)
	if !parent.ExpiresAt.IsZero() && parent.ExpiresAt.Before(expires) {
		expires = parent.ExpiresAt
	}
	return s.create(ctx, Grant{Owner: owner, Kind: Playback, Name: t.Title, TrackIDs: []string{id}, ParentID: parent.ID, ExpiresAt: expires}, filepath.Ext(t.Path))
}
func (s *Service) create(ctx context.Context, g Grant, extension string) (Link, error) {
	if s.baseURL == "" {
		return Link{}, ErrUnavailable
	}
	var raw [32]byte
	rand.Read(raw[:])
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	g.ID, g.TokenHash, g.RootID, g.CreatedAt = rand.Text(), hash(token), s.rootID, s.now().UTC()
	if err := s.store.Create(ctx, g); err != nil {
		return Link{}, err
	}
	path := "/s/" + token
	if g.Kind == Playback {
		path = "/play/" + token + "/audio" + extension
	}
	return Link{Grant: g, URL: s.baseURL + path}, nil
}
func hash(token string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(token))) }
func (s *Service) active(g Grant) bool {
	return g.RootID == s.rootID && (g.ExpiresAt.IsZero() || s.now().Before(g.ExpiresAt))
}
func (s *Service) resolve(ctx context.Context, token, kind string) (Grant, error) {
	if len(token) != 43 {
		return Grant{}, ErrNotFound
	}
	g, err := s.store.ByTokenHash(ctx, hash(token))
	if err != nil {
		return Grant{}, err
	}
	if g.Kind != kind || !s.active(g) {
		return Grant{}, ErrNotFound
	}
	return g, nil
}
func (s *Service) ResolveShare(ctx context.Context, token string) (Grant, error) {
	return s.resolve(ctx, token, Share)
}
func (s *Service) ShareTrack(ctx context.Context, token, id string) (library.Track, error) {
	g, err := s.ResolveShare(ctx, token)
	if err != nil {
		return library.Track{}, err
	}
	if !slices.Contains(g.TrackIDs, id) {
		return library.Track{}, ErrNotFound
	}
	return s.library.Get(ctx, id)
}
func (s *Service) PlaybackFromShare(ctx context.Context, token, id string) (Link, error) {
	g, err := s.ResolveShare(ctx, token)
	if err != nil {
		return Link{}, err
	}
	if !slices.Contains(g.TrackIDs, id) {
		return Link{}, ErrNotFound
	}
	return s.playback(ctx, g.Owner, id, 0, g)
}

// AuthorizePlayback is called before every request, including seeks and conditional reads.
func (s *Service) AuthorizePlayback(ctx context.Context, token string) (string, error) {
	g, err := s.resolve(ctx, token, Playback)
	if err != nil {
		return "", err
	}
	if g.ParentID != "" {
		parent, err := s.store.Get(ctx, g.ParentID)
		if err != nil {
			return "", err
		}
		if parent.Kind != Share || parent.Owner != g.Owner || !s.active(parent) || !slices.Contains(parent.TrackIDs, g.TrackIDs[0]) {
			return "", ErrNotFound
		}
	}
	return g.TrackIDs[0], nil
}
func (s *Service) List(ctx context.Context, owner, kind string, limit, offset int) ([]Grant, error) {
	if owner == "" || kind != Share && kind != Playback || limit < 1 || limit > 100 || offset < 0 {
		return nil, ErrInvalid
	}
	return s.store.List(ctx, owner, s.rootID, kind, limit, offset)
}
func (s *Service) Revoke(ctx context.Context, owner, id string) error {
	if owner == "" || id == "" {
		return ErrInvalid
	}
	return s.store.Revoke(ctx, id, owner, s.rootID)
}
