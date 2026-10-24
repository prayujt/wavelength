// Package library owns the protocol-independent catalog and read-only file scanner.
package library

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

var ErrNotFound = errors.New("track not found")
var ErrScanRunning = errors.New("scan already running")

// Track describes a file in the configured library; paths are relative to its root.
type Track struct {
	ID          string    `json:"id"`
	Path        string    `json:"path"`
	Title       string    `json:"title"`
	Artist      string    `json:"artist"`
	Album       string    `json:"album"`
	AlbumArtist string    `json:"album_artist"`
	TrackNumber int       `json:"track_number"`
	DiscNumber  int       `json:"disc_number"`
	Year        int       `json:"year"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	ModifiedAt  time.Time `json:"modified_at"`
	CreatedAt   time.Time `json:"created_at"`
	Genre       string    `json:"genre"`
	Duration    float64   `json:"duration"`
	BitRate     int       `json:"bit_rate"`
}

type Query struct {
	Search        string
	Limit, Offset int
}
type Page struct {
	Tracks []Track `json:"tracks"`
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
}
type ScanResult struct {
	Indexed          int `json:"indexed"`
	MetadataWarnings int `json:"metadata_warnings"`
}

// Store keeps catalog persistence separate from scanning and transport concerns.
type Store interface {
	Replace(context.Context, string, string, []Track) error
	List(context.Context, string, Query) (Page, error)
	Get(context.Context, string, string) (Track, error)
}

type Library struct {
	store    Store
	root     *os.Root
	rootID   string
	path     string
	scan     sync.Mutex
	scanning atomic.Bool
	scanned  atomic.Int64
	lastScan atomic.Int64
}

func Open(path string, store Store) (*Library, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("resolve music root: %w", err)
	}
	root, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, fmt.Errorf("open music root: %w", err)
	}
	return &Library{store: store, root: root, path: absolute, rootID: identifier(absolute)}, nil
}

func (l *Library) Close() error {
	l.scan.Lock()
	defer l.scan.Unlock()
	return l.root.Close()
}

func (l *Library) List(ctx context.Context, query Query) (Page, error) {
	if query.Limit <= 0 {
		query.Limit = 100
	}
	if query.Limit > 500 {
		query.Limit = 500
	}
	if query.Offset < 0 {
		query.Offset = 0
	}
	return l.store.List(ctx, l.rootID, query)
}
func (l *Library) Get(ctx context.Context, id string) (Track, error) {
	return l.store.Get(ctx, l.rootID, id)
}

// OpenTrack never accepts a user-supplied filesystem path. os.Root also prevents symlink escapes.
func (l *Library) OpenTrack(ctx context.Context, id string) (*os.File, Track, error) {
	track, err := l.Get(ctx, id)
	if err != nil {
		return nil, Track{}, err
	}
	file, err := l.root.Open(track.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, Track{}, ErrNotFound
		}
		return nil, Track{}, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		if err != nil {
			return nil, Track{}, err
		}
		return nil, Track{}, ErrNotFound
	}
	return file, track, nil
}

func identifier(value string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(value))) }

func (l *Library) ScanStatus() (bool, int64) { return l.scanning.Load(), l.scanned.Load() }

// LastScan is a monotonic timestamp for cache invalidation after a successful scan.
func (l *Library) LastScan() int64 { return l.lastScan.Load() }
