// Package library reads the persisted catalog and confines access to source files.
package library

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var ErrNotFound = errors.New("track not found")

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

// Store provides the authoritative catalog independently of source files.
type Store interface {
	Revision(context.Context, string) (int64, error)
	List(context.Context, string, Query) (Page, error)
	Get(context.Context, string, string) (Track, error)
}

type Library struct {
	deleteMu sync.Mutex
	deleted  []func(string)
	store    Store
	root     *os.Root
	rootID   string
	path     string
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
	return &Library{store: store, root: root, rootID: identifier(absolute), path: absolute}, nil
}

func (l *Library) Close() error {
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

// Revision changes only when persisted catalog records change.
func (l *Library) Revision(ctx context.Context) (int64, error) {
	return l.store.Revision(ctx, l.rootID)
}

// SourceRoot identifies the configured root for explicit ingestion; it does not inspect files.
func (l *Library) SourceRoot() (id, path string) { return l.rootID, l.path }
