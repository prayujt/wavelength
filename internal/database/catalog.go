package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/prayujt/wavelength/internal/library"
)

// Catalog implements library.Store using the application SQLite database.
type Catalog struct{ DB *sql.DB }

func (c Catalog) Replace(ctx context.Context, rootID, path string, tracks []library.Track) error {
	tx, err := c.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "INSERT INTO library_roots (id,path) VALUES (?,?) ON CONFLICT(id) DO UPDATE SET path=excluded.path", rootID, path); err != nil {
		return err
	}
	scanID := rand.Text()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO tracks (id,root_id,path,title,artist,album,album_artist,track_number,disc_number,year,content_type,size,modified_at,scan_id,genre,duration,bit_rate,created_at,search_text)
 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(root_id,path) DO UPDATE SET
 title=excluded.title,artist=excluded.artist,album=excluded.album,album_artist=excluded.album_artist,
 track_number=excluded.track_number,disc_number=excluded.disc_number,year=excluded.year,
 content_type=excluded.content_type,size=excluded.size,modified_at=excluded.modified_at,scan_id=excluded.scan_id,genre=excluded.genre,duration=excluded.duration,bit_rate=excluded.bit_rate,search_text=excluded.search_text`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, t := range tracks {
		if _, err = stmt.ExecContext(ctx, t.ID, rootID, t.Path, t.Title, t.Artist, t.Album, t.AlbumArtist, t.TrackNumber, t.DiscNumber, t.Year, t.ContentType, t.Size, t.ModifiedAt.Format(time.RFC3339Nano), scanID, t.Genre, t.Duration, t.BitRate, time.Now().UTC().Format(time.RFC3339Nano), strings.ToLower(t.Title+" "+t.Artist+" "+t.Album)); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM tracks WHERE root_id=? AND scan_id<>?", rootID, scanID); err != nil {
		return err
	}
	return tx.Commit()
}

const trackColumns = "id,path,title,artist,album,album_artist,track_number,disc_number,year,content_type,size,modified_at,genre,duration,bit_rate,created_at"
const trackFilter = "root_id=? AND content_type LIKE 'audio/%' AND (?='' OR instr(search_text,?)>0)"

func (c Catalog) List(ctx context.Context, rootID string, q library.Query) (library.Page, error) {
	page := library.Page{Tracks: []library.Track{}, Limit: q.Limit, Offset: q.Offset}
	tx, err := c.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM tracks WHERE "+trackFilter, rootID, q.Search, strings.ToLower(q.Search)).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+trackColumns+" FROM tracks WHERE "+trackFilter+" ORDER BY artist COLLATE NOCASE,album COLLATE NOCASE,disc_number,track_number,path LIMIT ? OFFSET ?", rootID, q.Search, strings.ToLower(q.Search), q.Limit, q.Offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		track, err := readTrack(rows)
		if err != nil {
			return page, err
		}
		page.Tracks = append(page.Tracks, track)
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	if err = rows.Close(); err != nil {
		return page, err
	}
	return page, tx.Commit()
}

func (c Catalog) Get(ctx context.Context, rootID, id string) (library.Track, error) {
	track, err := readTrack(c.DB.QueryRowContext(ctx, "SELECT "+trackColumns+" FROM tracks WHERE root_id=? AND id=? AND content_type LIKE 'audio/%'", rootID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return track, library.ErrNotFound
	}
	return track, err
}

type rowScanner interface{ Scan(...any) error }

func readTrack(row rowScanner) (library.Track, error) {
	var t library.Track
	var modified, created string
	err := row.Scan(&t.ID, &t.Path, &t.Title, &t.Artist, &t.Album, &t.AlbumArtist, &t.TrackNumber, &t.DiscNumber, &t.Year, &t.ContentType, &t.Size, &modified, &t.Genre, &t.Duration, &t.BitRate, &created)
	if err != nil {
		return t, err
	}
	if created != "" {
		t.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return t, err
		}
	}
	t.ModifiedAt, err = time.Parse(time.RFC3339Nano, modified)
	return t, err
}
