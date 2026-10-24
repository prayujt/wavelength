// Package testutil creates isolated catalog fixtures; it is never imported by the application.
package testutil

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dhowden/tag"
)

// SeedCatalog records test fixture files in SQLite without relying on application ingestion.
func SeedCatalog(t testing.TB, db *sql.DB, music string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(music)
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	rootID := fmt.Sprintf("%x", sha256.Sum256([]byte(root)))
	if _, err = db.Exec("INSERT INTO library_roots(id,path) VALUES (?,?) ON CONFLICT DO NOTHING", rootID, root); err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		kind := map[string]string{".mp3": "audio/mpeg", ".flac": "audio/flac", ".wav": "audio/wav"}[filepath.Ext(path)]
		if kind == "" {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		id := fmt.Sprintf("%x", sha256.Sum256([]byte(rootID+"\x00"+relative)))
		title, artist, album, albumArtist, genre := strings.TrimSuffix(entry.Name(), filepath.Ext(path)), "", "", "", ""
		track, disc, year := 0, 0, 0
		if metadata, err := tag.ReadFrom(file); err == nil {
			if metadata.Title() != "" {
				title = metadata.Title()
			}
			artist, album, albumArtist, genre = metadata.Artist(), metadata.Album(), metadata.AlbumArtist(), metadata.Genre()
			track, _ = metadata.Track()
			disc, _ = metadata.Disc()
			year = metadata.Year()
		}
		_, err = db.Exec(`INSERT INTO tracks(id,root_id,path,title,artist,album,album_artist,track_number,disc_number,year,content_type,size,modified_at,created_at,genre,duration,bit_rate,search_text)
            VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, id, rootID, relative, title, artist, album, albumArtist, track, disc, year, kind, info.Size(), info.ModTime().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano), genre, 0, 0, strings.ToLower(title+" "+artist+" "+album))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
