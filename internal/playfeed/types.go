// Package playfeed owns personal, paginated recommendations independently of transport.
package playfeed

import (
	"context"
	"errors"
	"time"

	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/listening"
)

var (
	ErrInvalid  = errors.New("invalid play feed request")
	ErrNotFound = errors.New("play feed cursor expired or was not found")
	ErrConflict = errors.New("play feed request ID was reused with different inputs")
	ErrCapacity = errors.New("play feed is busy; retry shortly")
)

const (
	MaxPageSize   = 25
	MaxCandidates = 120
	MaxExclusions = 1000
)

type Request struct {
	RequestID        string   `json:"request_id"`
	Cursor           string   `json:"cursor"`
	PageSize         int      `json:"page_size"`
	SeedTrackID      string   `json:"seed_track_id"`
	QueuedTrackIDs   []string `json:"queued_track_ids"`
	ExcludedTrackIDs []string `json:"excluded_track_ids"`
}
type Item struct {
	ID    string
	Track library.Track
}
type Page struct {
	FeedID     string
	Items      []Item
	NextCursor string
	Exhausted  bool
}
type Receipt struct {
	Hash string
	Page Page
}
type Continuation struct {
	FeedID string
	Seen   []string
	Issued *Page
}

// Preference contains aggregate observations, not account identifiers or source paths.
type Preference struct {
	Track                             library.Track
	Completed, EarlySkips, OtherSkips int
	ListenedMS                        int64
	LastPlayedAt                      time.Time
}

// RecentListen preserves session order and actual playback timing for the next
// decision without exposing session IDs, account identities, or source paths.
type RecentListen struct {
	Track                         library.Track
	StartedAt, UpdatedAt, EndedAt time.Time
	ListenedMS, PositionMS        int64
	Playing                       bool
	Outcome, ChangeReason         string
}
type Context struct {
	AsOf        time.Time
	Recent      []RecentListen
	Seed        library.Track
	Upcoming    []library.Track
	Preferences []Preference
}
type Score struct {
	TrackID string
	Value   float64
}
type Ranker interface {
	Recommend(context.Context, Context, []library.Track) ([]Score, error)
}
type History interface {
	List(context.Context, string, string, listening.Query) (listening.Page, error)
}
type Store interface {
	Request(context.Context, string, string, string) (Receipt, error)
	Continue(context.Context, string, string, string) (Continuation, error)
	Save(context.Context, string, string, Request, string, Page) (Receipt, error)
	Candidates(context.Context, string, []string, []string, int) ([]library.Track, error)
	Count(context.Context, string, []string) (int, error)
}
