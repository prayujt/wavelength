package library

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dhowden/tag"
)

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
		track := Track{ID: identifier(l.rootID + "\x00" + path), Path: path, ContentType: mime, Size: info.Size(), ModifiedAt: info.ModTime().UTC()}
		metadata, tagErr := tag.ReadFrom(file)
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
			raw := make(map[string]string)
			for key, value := range metadata.Raw() {
				if text, ok := value.(string); ok {
					raw[strings.ReplaceAll(strings.ToLower(key), "_", "")] = text
				}
			}
			fillTags(&track, raw)
		} else {
			result.MetadataWarnings++
			slog.Debug("metadata unavailable; using filename", "path", path, "error", tagErr)
		}
		probe(ctx, file, &track)
		if err := file.Close(); err != nil {
			return err
		}
		if track.Title == "" {
			track.Title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
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
	// A scan can add old files or remove the newest one; mtimes are not a catalog revision.
	l.lastScan.Store(max(time.Now().UnixMilli(), l.lastScan.Load()+1))
	return result, nil
}

var contentTypes = map[string]string{
	".mp3": "audio/mpeg", ".flac": "audio/flac", ".m4a": "audio/mp4", ".mp4": "video/mp4",
	".wma": "audio/x-ms-wma", ".ape": "audio/x-ape", ".oga": "audio/ogg", ".m4b": "audio/mp4", ".mpc": "audio/x-musepack", ".shn": "audio/x-shorten", ".wv": "audio/wavpack", ".dsf": "audio/dsf",
	".ogg": "audio/ogg", ".opus": "audio/ogg", ".wav": "audio/wav", ".aac": "audio/aac", ".aiff": "audio/aiff", ".aif": "audio/aiff",
	".mkv": "video/x-matroska", ".webm": "video/webm", ".m4v": "video/mp4", ".mov": "video/quicktime", ".avi": "video/x-msvideo", ".flv": "video/x-flv", ".mpeg": "video/mpeg", ".mpg": "video/mpeg",
}
