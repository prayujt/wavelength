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

func (s IngestStore) Origin(ctx context.Context, root, id string) (discovery.Ref, error) {
	var ref discovery.Ref
	err := s.DB.QueryRowContext(ctx, "SELECT provider,external_id FROM track_origins WHERE root_id=? AND track_id=? ORDER BY provider,external_id LIMIT 1", root, id).Scan(&ref.Provider, &ref.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return ref, ingest.ErrNotFound
	}
	return ref, err
}

func (s IngestStore) LookupMany(ctx context.Context, root string, refs []discovery.Ref) (map[discovery.Ref]string, error) {
	found := make(map[discovery.Ref]string)
	if len(refs) == 0 {
		return found, nil
	}
	conditions := make([]string, len(refs))
	args := []any{root}
	for i, ref := range refs {
		conditions[i] = "(o.provider=? AND o.external_id=?)"
		args = append(args, ref.Provider, ref.ID)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT o.provider,o.external_id,o.track_id FROM track_origins o
		JOIN tracks t ON t.root_id=o.root_id AND t.id=o.track_id
		WHERE o.root_id=? AND (`+strings.Join(conditions, " OR ")+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref discovery.Ref
		var id string
		if err := rows.Scan(&ref.Provider, &ref.ID, &id); err != nil {
			return nil, err
		}
		found[ref] = id
	}
	return found, rows.Err()
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
		if item.ReplaceTrackID != "" && item.State != ingest.Completed {
			if existing != item.ReplaceTrackID || p.Track.ID != existing || p.Track.Path == "" || p.SHA256 == "" {
				return t, ingest.ErrConflict
			}
			// Keep catalog identity and metadata; only the validated audio asset changes.
			t.Path, t.Size, t.ModifiedAt = p.Track.Path, p.Track.Size, p.Track.ModifiedAt
			t.Duration, t.BitRate, t.ContentType = p.Track.Duration, p.Track.BitRate, p.Track.ContentType
			_, err = tx.ExecContext(ctx, "UPDATE tracks SET path=?,size=?,modified_at=?,duration=?,bit_rate=?,content_type=? WHERE root_id=? AND id=?", t.Path, t.Size, t.ModifiedAt.Format(time.RFC3339Nano), t.Duration, t.BitRate, t.ContentType, root.ID, existing)
			if err != nil {
				return t, err
			}
			_, err = tx.ExecContext(ctx, "UPDATE track_origins SET source_provider=?,source_id=?,sha256=? WHERE root_id=? AND track_id=?", item.SourceRef.Provider, item.SourceRef.ID, p.SHA256, root.ID, existing)
			if err != nil {
				return t, err
			}
			reused = false
		}
	} else {
		if item.ReplaceTrackID != "" {
			return t, ingest.ErrNotFound
		}
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
	group := t.AlbumIdentity()
	item.AlbumID, item.ArtistID = group.AlbumID, group.ArtistID
	if item.Metadata == nil {
		item.Metadata = &discovery.Track{Ref: item.MetadataRef, Title: t.Title, Artist: t.Artist, Album: t.Album, AlbumArtist: t.AlbumArtist, Duration: t.Duration}
	}
	item.Reused = reused
	item.Error = ""
	if err = saveJob(ctx, tx, &j); err != nil {
		return t, err
	}
	return t, tx.Commit()
}

var _ ingest.Store = IngestStore{}
