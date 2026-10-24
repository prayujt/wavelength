package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/prayujt/wavelength/internal/discovery"
	"github.com/prayujt/wavelength/internal/ingest"
	"github.com/prayujt/wavelength/internal/library"
)

type IngestStore struct{ DB *sql.DB }

func (s IngestStore) EnsureRoot(ctx context.Context, r ingest.Root) error {
	_, err := s.DB.ExecContext(ctx, "INSERT INTO library_roots(id,path) VALUES(?,?) ON CONFLICT(id) DO NOTHING", r.ID, r.Path)
	return err
}
func readJob(row interface{ Scan(...any) error }) (ingest.Job, error) {
	var raw []byte
	var j ingest.Job
	err := row.Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return j, ingest.ErrNotFound
	}
	if err != nil {
		return j, err
	}
	err = json.Unmarshal(raw, &j)
	return j, err
}
func saveJob(ctx context.Context, tx *sql.Tx, j *ingest.Job) error {
	j.UpdatedAt = time.Now().UTC()
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE ingest_jobs SET state=?,body=? WHERE id=?", j.State, raw, j.ID)
	return err
}
func (s IngestStore) Create(ctx context.Context, j ingest.Job) (ingest.Job, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return j, err
	}
	defer tx.Rollback()
	old, err := readJob(tx.QueryRowContext(ctx, "SELECT body FROM ingest_jobs WHERE root_id=? AND owner=? AND request_id=?", j.RootID, j.Owner, j.Request.RequestID))
	if err == nil {
		if old.RequestHash != j.RequestHash {
			return old, ingest.ErrConflict
		}
		return old, nil
	}
	if !errors.Is(err, ingest.ErrNotFound) {
		return j, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM ingest_jobs WHERE root_id=? AND state IN ('queued','running')", j.RootID).Scan(&count); err != nil {
		return j, err
	}
	if count >= 100 {
		return j, ingest.ErrCapacity
	}
	raw, err := json.Marshal(j)
	if err != nil {
		return j, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO ingest_jobs(id,root_id,owner,request_id,request_hash,state,body,created_at) VALUES(?,?,?,?,?,?,?,?)", j.ID, j.RootID, j.Owner, j.Request.RequestID, j.RequestHash, j.State, raw, j.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return j, err
	}
	return j, tx.Commit()
}
func (s IngestStore) Get(ctx context.Context, root, owner, id string) (ingest.Job, error) {
	return readJob(s.DB.QueryRowContext(ctx, "SELECT body FROM ingest_jobs WHERE root_id=? AND owner=? AND id=?", root, owner, id))
}
func (s IngestStore) List(ctx context.Context, root, owner string, limit, offset int) ([]ingest.Job, int, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM ingest_jobs WHERE root_id=? AND owner=?", root, owner).Scan(&count); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT body FROM ingest_jobs WHERE root_id=? AND owner=? ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?", root, owner, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	out := []ingest.Job{}
	for rows.Next() {
		j, err := readJob(rows)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		out = append(out, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	return out, count, tx.Commit()
}
func (s IngestStore) Next(ctx context.Context, root string) (ingest.Job, error) {
	return readJob(s.DB.QueryRowContext(ctx, "SELECT body FROM ingest_jobs WHERE root_id=? AND (state IN ('queued','running') OR (state='cancelled' AND json_extract(body,'$.cleaned')=0)) ORDER BY CASE WHEN state='cancelled' THEN 1 ELSE 0 END,created_at,id LIMIT 1", root))
}
func (s IngestStore) Update(ctx context.Context, id string, change func(*ingest.Job) error) (ingest.Job, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return ingest.Job{}, err
	}
	defer tx.Rollback()
	j, err := readJob(tx.QueryRowContext(ctx, "SELECT body FROM ingest_jobs WHERE id=?", id))
	if err != nil {
		return j, err
	}
	if err = change(&j); err != nil {
		return j, err
	}
	if err = saveJob(ctx, tx, &j); err != nil {
		return j, err
	}
	return j, tx.Commit()
}
func lookupOrigin(ctx context.Context, tx *sql.Tx, root string, ref discovery.Ref) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, "SELECT track_id FROM track_origins WHERE root_id=? AND provider=? AND external_id=?", root, ref.Provider, ref.ID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ingest.ErrNotFound
	}
	return id, err
}
func (s IngestStore) Lookup(ctx context.Context, root string, ref discovery.Ref) (library.Track, error) {
	var id string
	err := s.DB.QueryRowContext(ctx, "SELECT track_id FROM track_origins WHERE root_id=? AND provider=? AND external_id=?", root, ref.Provider, ref.ID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return library.Track{}, ingest.ErrNotFound
	}
	if err != nil {
		return library.Track{}, err
	}
	return (Catalog{DB: s.DB}).Get(ctx, root, id)
}

// Publication and its job receipt share a transaction. A crash cannot expose a
// catalog row without also recording that this item completed.
func (s IngestStore) Publish(ctx context.Context, root ingest.Root, jobID string, index int, p ingest.Prepared) (library.Track, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return library.Track{}, err
	}
	defer tx.Rollback()
	j, err := readJob(tx.QueryRowContext(ctx, "SELECT body FROM ingest_jobs WHERE id=? AND root_id=?", jobID, root.ID))
	if err != nil {
		return library.Track{}, err
	}
	if j.State == ingest.Cancelled {
		return library.Track{}, ingest.ErrCancelled
	}
	if index < 0 || index >= len(j.Items) {
		return library.Track{}, ingest.ErrInvalid
	}
	item := &j.Items[index]
	existing, err := lookupOrigin(ctx, tx, root.ID, item.MetadataRef)
	reused := err == nil
	if err != nil && !errors.Is(err, ingest.ErrNotFound) {
		return library.Track{}, err
	}
	t := p.Track
	if reused {
		t, err = readTrack(tx.QueryRowContext(ctx, "SELECT "+trackColumns+" FROM tracks WHERE root_id=? AND id=?", root.ID, existing))
		if err != nil {
			return t, err
		}
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO tracks(id,root_id,path,title,artist,album,album_artist,track_number,disc_number,year,content_type,size,modified_at,genre,duration,bit_rate,created_at,search_text) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, t.ID, root.ID, t.Path, t.Title, t.Artist, t.Album, t.AlbumArtist, t.TrackNumber, t.DiscNumber, t.Year, t.ContentType, t.Size, t.ModifiedAt.Format(time.RFC3339Nano), t.Genre, t.Duration, t.BitRate, t.CreatedAt.Format(time.RFC3339Nano), strings.ToLower(t.Title+" "+t.Artist+" "+t.Album))
		if err != nil {
			return t, err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO track_origins(root_id,provider,external_id,track_id,source_provider,source_id,sha256) VALUES(?,?,?,?,?,?,?)", root.ID, item.MetadataRef.Provider, item.MetadataRef.ID, t.ID, item.SourceRef.Provider, item.SourceRef.ID, p.SHA256)
		if err != nil {
			return t, err
		}
	}
	item.State = ingest.Completed
	item.TrackID = t.ID
	item.Reused = reused
	item.Error = ""
	if err = saveJob(ctx, tx, &j); err != nil {
		return t, err
	}
	return t, tx.Commit()
}

var _ ingest.Store = IngestStore{}
