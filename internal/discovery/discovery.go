// Package discovery resolves music metadata and candidate recordings before acquisition.
package discovery

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	ErrInvalid     = errors.New("invalid discovery input")
	ErrNotFound    = errors.New("music was not found")
	ErrUnavailable = errors.New("music provider is unavailable")
)

// Ref keeps provider identities separate from local catalog IDs.
type Ref struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
}

var metadataID = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
var videoID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func (r Ref) ValidMetadata() bool { return r.Provider == "itunes" && metadataID.MatchString(r.ID) }
func (r Ref) ValidSource() bool   { return r.Provider == "youtube" && videoID.MatchString(r.ID) }
func (r Ref) Empty() bool         { return r == (Ref{}) }

type Track struct {
	Ref         Ref     `json:"ref"`
	Title       string  `json:"title"`
	Artist      string  `json:"artist"`
	Album       string  `json:"album"`
	AlbumArtist string  `json:"album_artist"`
	Genre       string  `json:"genre"`
	ReleaseDate string  `json:"release_date"`
	TrackNumber int     `json:"track_number"`
	DiscNumber  int     `json:"disc_number"`
	Duration    float64 `json:"duration_seconds"`
	ArtworkURL  string  `json:"artwork_url"`
}
type Release struct {
	Ref        Ref     `json:"ref"`
	Title      string  `json:"title"`
	Artist     string  `json:"artist"`
	ArtworkURL string  `json:"artwork_url"`
	Tracks     []Track `json:"tracks"`
}
type Artist struct {
	Ref        Ref       `json:"ref"`
	Name       string    `json:"name"`
	ArtworkURL string    `json:"artwork_url,omitempty"`
	Releases   []Release `json:"releases"`
}
type Results struct {
	Tracks   []Track   `json:"tracks"`
	Releases []Release `json:"releases"`
	Artists  []Artist  `json:"artists"`
}
type Candidate struct {
	Ref      Ref     `json:"ref"`
	Title    string  `json:"title"`
	Uploader string  `json:"uploader"`
	Duration float64 `json:"duration_seconds"`
	Score    float64 `json:"score"`
	Suitable bool    `json:"suitable"`
	Reason   string  `json:"reason"`
}

type Metadata interface {
	Search(context.Context, string, int) (Results, error)
	Track(context.Context, Ref) (Track, error)
	Release(context.Context, Ref) (Release, error)
	Artist(context.Context, Ref) (Artist, error)
}
type Sources interface {
	Candidates(context.Context, Track) ([]Candidate, error)
}
type Service struct {
	metadata Metadata
	sources  Sources
}

func New(metadata Metadata, sources Sources) *Service { return &Service{metadata, sources} }
func (s *Service) Search(ctx context.Context, query string, limit int) (Results, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 300 || limit < 0 || limit > 50 {
		return Results{}, ErrInvalid
	}
	if limit == 0 {
		limit = 10
	}
	lookup, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	return s.metadata.Search(lookup, query, limit)
}
func (s *Service) Track(ctx context.Context, ref Ref) (Track, error) {
	if !ref.ValidMetadata() {
		return Track{}, ErrInvalid
	}
	lookup, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	t, err := s.metadata.Track(lookup, ref)
	if err == nil && (t.Ref != ref || t.Title == "" || t.Artist == "" || t.Duration <= 0) {
		return Track{}, fmt.Errorf("%w: incomplete track metadata", ErrUnavailable)
	}
	return t, err
}
func (s *Service) Release(ctx context.Context, ref Ref) (Release, error) {
	if !ref.ValidMetadata() {
		return Release{}, ErrInvalid
	}
	lookup, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	r, err := s.metadata.Release(lookup, ref)
	if err == nil && (r.Ref != ref || len(r.Tracks) == 0 || len(r.Tracks) > 200) {
		return Release{}, fmt.Errorf("%w: release must contain 1..200 tracks", ErrUnavailable)
	}
	if err == nil {
		for _, t := range r.Tracks {
			if !t.Ref.ValidMetadata() || t.Title == "" || t.Artist == "" || t.Duration <= 0 {
				return Release{}, fmt.Errorf("%w: incomplete release metadata", ErrUnavailable)
			}
		}
	}
	return r, err
}
func (s *Service) Artist(ctx context.Context, ref Ref) (Artist, error) {
	if !ref.ValidMetadata() {
		return Artist{}, ErrInvalid
	}
	lookup, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	return s.metadata.Artist(lookup, ref)
}
func (s *Service) FindSources(ctx context.Context, ref Ref) ([]Candidate, error) {
	t, err := s.Track(ctx, ref)
	if err != nil {
		return nil, err
	}
	return s.Candidates(ctx, t)
}

// Select only automates a strong, unambiguous match. Scores are heuristics, not probabilities.
func Select(candidates []Candidate) (Ref, bool) {
	if len(candidates) == 0 || !candidates[0].Suitable || candidates[0].Score < .85 {
		return Ref{}, false
	}
	if len(candidates) > 1 && candidates[1].Suitable && candidates[0].Score-candidates[1].Score < .05 {
		return Ref{}, false
	}
	return candidates[0].Ref, true
}

func (s *Service) Candidates(ctx context.Context, t Track) ([]Candidate, error) {
	lookup, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	return s.sources.Candidates(lookup, t)
}
