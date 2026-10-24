// Package library owns the protocol-independent catalog and read-only file scanner.
package library

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dhowden/tag"
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
	IncludeVideo  bool
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

func (l *Library) Close() error { return l.root.Close() }

// Scan builds a snapshot before committing it. Failed/cancelled scans leave the catalog intact.
// Symlinks and non-regular files are excluded. Unsupported/missing tags use a filename title.
func (l *Library) Scan(ctx context.Context) (ScanResult, error) {
	if !l.scan.TryLock() {
		return ScanResult{}, ErrScanRunning
	}
	defer l.scan.Unlock()
	l.scanning.Store(true)
	defer l.scanning.Store(false)
	tracks := make([]Track, 0)
	result := ScanResult{}
	err := fs.WalkDir(l.root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		mime, ok := contentTypes[strings.ToLower(filepath.Ext(path))]
		if !ok {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		file, err := l.root.Open(path)
		if err != nil {
			return err
		}
		// Re-check after open in case the directory changed during the scan.
		info, err = file.Stat()
		if err != nil {
			file.Close()
			return err
		}
		if !info.Mode().IsRegular() {
			file.Close()
			return fmt.Errorf("non-regular audio file: %s", path)
		}
		track := Track{ID: identifier(l.rootID + "\x00" + path), Path: path, Title: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), ContentType: mime, Size: info.Size(), ModifiedAt: info.ModTime().UTC()}
		metadata, tagErr := tag.ReadFrom(file)
		probe(ctx, file, &track)
		closeErr := file.Close()
		if closeErr != nil {
			return closeErr
		}
		if tagErr == nil {
			if title := strings.TrimSpace(metadata.Title()); title != "" {
				track.Title = title
			}
			if genre := metadata.Genre(); genre != "" {
				track.Genre = genre
			}
			track.Artist = metadata.Artist()
			track.Album = metadata.Album()
			track.AlbumArtist = metadata.AlbumArtist()
			track.TrackNumber, _ = metadata.Track()
			track.DiscNumber, _ = metadata.Disc()
			track.Year = metadata.Year()
		} else {
			result.MetadataWarnings++
			slog.Debug("metadata unavailable; using filename", "path", path, "error", tagErr)
		}
		tracks = append(tracks, track)
		return nil
	})
	if err != nil {
		return result, fmt.Errorf("scan music root: %w", err)
	}
	if err = l.store.Replace(ctx, l.rootID, l.path, tracks); err != nil {
		return result, err
	}
	result.Indexed = len(tracks)
	l.scanned.Store(int64(len(tracks)))
	return result, nil
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

var contentTypes = map[string]string{
	".mp3": "audio/mpeg", ".flac": "audio/flac", ".m4a": "audio/mp4", ".mp4": "video/mp4",
	".ogg": "audio/ogg", ".opus": "audio/ogg", ".wav": "audio/wav", ".aac": "audio/aac", ".aiff": "audio/aiff", ".aif": "audio/aiff",
	".mkv": "video/x-matroska", ".webm": "video/webm", ".m4v": "video/mp4", ".mov": "video/quicktime", ".avi": "video/x-msvideo", ".flv": "video/x-flv", ".mpeg": "video/mpeg", ".mpg": "video/mpeg",
}

func (l *Library) ScanStatus() (bool, int64) { return l.scanning.Load(), l.scanned.Load() }
