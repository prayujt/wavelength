// Package ingest owns durable acquisition jobs and explicit catalog publication.
package ingest

import (
	"context"
	"errors"
	"time"

	"github.com/prayujt/wavelength/internal/discovery"
	"github.com/prayujt/wavelength/internal/library"
)

var (
	ErrInvalid   = errors.New("invalid import input")
	ErrNotFound  = errors.New("import was not found")
	ErrConflict  = errors.New("import conflicts with an existing request or state")
	ErrCancelled = errors.New("import was cancelled")
	ErrCapacity  = errors.New("too many queued imports")
)

const MaxItems = 200
const (
	Queued         = "queued"
	Running        = "running"
	Completed      = "completed"
	Failed         = "failed"
	Partial        = "partial"
	Cancelled      = "cancelled"
	NeedsSelection = "needs_selection"
)

type Selection struct {
	MetadataRef discovery.Ref `json:"metadata_ref"`
	SourceRef   discovery.Ref `json:"source_ref,omitempty"`
}
type Request struct {
	RequestID  string        `json:"request_id"`
	Items      []Selection   `json:"items,omitempty"`
	ReleaseRef discovery.Ref `json:"release_ref,omitempty"`
	PlaylistID string        `json:"playlist_id,omitempty"`
}
type Override struct {
	Index     int           `json:"index"`
	SourceRef discovery.Ref `json:"source_ref"`
}
type Root struct{ ID, Path string }

// Prepared is persisted before a file becomes visible, allowing finalization to
// resume after a crash without enumerating the library or redoing a download.
type Prepared struct {
	Track  library.Track `json:"track"`
	SHA256 string        `json:"sha256"`
}
type Item struct {
	Selection
	State     string           `json:"state"`
	Error     string           `json:"error,omitempty"`
	TrackID   string           `json:"track_id,omitempty"`
	AlbumID   string           `json:"album_id,omitempty"`
	ArtistID  string           `json:"artist_id,omitempty"`
	Reused    bool             `json:"reused"`
	Metadata  *discovery.Track `json:"metadata,omitempty"`
	Prepared  *Prepared        `json:"prepared,omitempty"`
	Automatic bool             `json:"automatic"`
	Warnings  []string         `json:"warnings,omitempty"`
}
type Job struct {
	Cleaned     bool      `json:"cleaned"`
	ID          string    `json:"id"`
	RootID      string    `json:"root_id"`
	Owner       string    `json:"owner"`
	Request     Request   `json:"request"`
	RequestHash string    `json:"request_hash"`
	State       string    `json:"state"`
	Error       string    `json:"error,omitempty"`
	Items       []Item    `json:"items"`
	Expanded    bool      `json:"expanded"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (j Job) Terminal() bool { return j.State != Queued && j.State != Running }

type Store interface {
	EnsureRoot(context.Context, Root) error
	Create(context.Context, Job) (Job, error)
	Get(context.Context, string, string, string) (Job, error)
	List(context.Context, string, string, int, int) ([]Job, int, error)
	Next(context.Context, string) (Job, error)
	Update(context.Context, string, func(*Job) error) (Job, error)
	Lookup(context.Context, string, discovery.Ref) (library.Track, error)
	Publish(context.Context, Root, string, int, Prepared) (library.Track, error)
}
type Downloader interface {
	Download(context.Context, discovery.Ref, string) error
}
type Media interface {
	Prepare(context.Context, string, discovery.Track) (library.Track, []string, error)
}
type Playlists interface {
	CheckImport(context.Context, string, string) error
	AppendImport(context.Context, string, string, string, []string) error
}
