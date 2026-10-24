package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/prayujt/wavelength/internal/library"
)

// Catalog implements library.Store using the application SQLite database.
type Catalog struct{ DB *sql.DB }

func (c Catalog) Revision(ctx context.Context, rootID string) (int64, error) {
	var revision int64
	err := c.DB.QueryRowContext(ctx, "SELECT revision FROM library_roots WHERE id=?", rootID).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return revision, err
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
