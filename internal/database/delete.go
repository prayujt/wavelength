package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/prayujt/wavelength/internal/discovery"
	"github.com/prayujt/wavelength/internal/ingest"
	"github.com/prayujt/wavelength/internal/library"
	"github.com/prayujt/wavelength/internal/userdata"
)

func readDeletion(row rowScanner) (library.Deletion, error) {
	var d library.Deletion
	var paths string
	err := row.Scan(&d.TrackID, &paths, &d.Done)
	if err == nil {
		err = json.Unmarshal([]byte(paths), &d.Paths)
	}
	return d, err
}

func (c Catalog) BeginDelete(ctx context.Context, root, id string, validate func([]string) error) (library.Deletion, error) {
	tx, err := c.DB.BeginTx(ctx, nil)
	if err != nil {
		return library.Deletion{}, err
	}
	defer tx.Rollback()
	old, err := readDeletion(tx.QueryRowContext(ctx, "SELECT track_id,paths_json,files_deleted FROM library_deletions WHERE root_id=? AND track_id=?", root, id))
	if err == nil {
		return old, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return old, err
	}
	track, err := readTrack(tx.QueryRowContext(ctx, "SELECT "+trackColumns+" FROM tracks WHERE root_id=? AND id=? AND content_type LIKE 'audio/%'", root, id))
	if errors.Is(err, sql.ErrNoRows) {
		return old, library.ErrNotFound
	}
	if err != nil {
		return old, err
	}
	if !filepath.IsLocal(track.Path) || filepath.Clean(track.Path) == "." {
		return old, library.ErrInvalid
	}
	if err := cleanDeletionJobs(ctx, tx, root, id); err != nil {
		return old, err
	}
	paths := []string{track.Path}
	dir, stem := filepath.Dir(track.Path), strings.TrimSuffix(track.Path, filepath.Ext(track.Path))
	sharedStem, remaining := false, false
	query := "SELECT path FROM tracks WHERE root_id=? AND id<>?"
	args := []any{root, id}
	if dir == "." {
		query += " AND instr(path,'/')=0"
	} else {
		// Use the existing (root_id,path) index to inspect only this album folder.
		query += " AND path>=? AND path<?"
		args = append(args, dir+"/", dir+"0")
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return old, err
	}
	for rows.Next() {
		var path string
		if err = rows.Scan(&path); err != nil {
			rows.Close()
			return old, err
		}
		if filepath.Dir(path) == dir {
			remaining = true
		}
		if strings.TrimSuffix(path, filepath.Ext(path)) == stem {
			sharedStem = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return old, err
	}
	if !sharedStem {
		paths = append(paths, stem+".lrc", stem+".txt")
	}
	if !remaining {
		for _, name := range []string{"cover.jpg", "cover.jpeg", "cover.png", "cover.gif", "folder.jpg", "folder.jpeg", "folder.png", "front.jpg", "front.png"} {
			paths = append(paths, filepath.Join(dir, name))
		}
	}
	if err = validate(paths); err != nil {
		return old, err
	}
	if err = cleanDeletionState(ctx, tx, id); err != nil {
		return old, err
	}
	for _, q := range []string{
		"DELETE FROM media_grants WHERE root_id=? AND EXISTS (SELECT 1 FROM json_each(media_grants.track_ids) WHERE value=?)",
		"DELETE FROM listening_sessions WHERE root_id=? AND track_id=?",
		"DELETE FROM tracks WHERE root_id=? AND id=?",
	} {
		if _, err = tx.ExecContext(ctx, q, root, id); err != nil {
			return old, err
		}
	}
	raw, err := json.Marshal(paths)
	if err != nil {
		return old, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO library_deletions(root_id,track_id,paths_json,created_at) VALUES(?,?,?,?)", root, id, string(raw), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return old, err
	}
	return library.Deletion{TrackID: id, Paths: paths}, tx.Commit()
}

func cleanDeletionJobs(ctx context.Context, tx *sql.Tx, root, id string) error {
	rows, err := tx.QueryContext(ctx, "SELECT provider,external_id FROM track_origins WHERE root_id=? AND track_id=?", root, id)
	if err != nil {
		return err
	}
	refs := map[discovery.Ref]bool{}
	for rows.Next() {
		var ref discovery.Ref
		if err = rows.Scan(&ref.Provider, &ref.ID); err != nil {
			rows.Close()
			return err
		}
		refs[ref] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = tx.QueryContext(ctx, "SELECT body FROM ingest_jobs WHERE root_id=? AND (state IN ('queued','running') OR (state='cancelled' AND json_extract(body,'$.cleaned')=0) OR instr(CAST(body AS TEXT),?)>0)", root, id)
	if err != nil {
		return err
	}
	var changed []ingest.Job
	for rows.Next() {
		j, e := readJob(rows)
		if e != nil {
			rows.Close()
			return e
		}
		active := j.State == ingest.Queued || j.State == ingest.Running || (j.State == ingest.Cancelled && !j.Cleaned)
		if active && !j.Expanded {
			rows.Close()
			return library.ErrBusy
		}
		touched := false
		for n := range j.Items {
			i := &j.Items[n]
			matches := i.TrackID == id || i.ReplaceTrackID == id || (i.Prepared != nil && i.Prepared.Track.ID == id) || refs[i.MetadataRef]
			if active && matches {
				rows.Close()
				return library.ErrBusy
			}
			if i.Prepared != nil && i.Prepared.Track.ID == id && i.State != ingest.Completed && j.State != ingest.Cancelled {
				rows.Close()
				return library.ErrBusy
			}
			if i.TrackID == id || (i.Prepared != nil && i.Prepared.Track.ID == id) {
				i.TrackID, i.AlbumID, i.ArtistID = "", "", ""
				i.Prepared = nil
				i.State = "deleted"
				i.Error = "This library track was deleted."
				touched = true
			}
		}
		if touched {
			changed = append(changed, j)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for n := range changed {
		if err := saveJob(ctx, tx, &changed[n]); err != nil {
			return err
		}
	}
	return nil
}

func cleanDeletionState(ctx context.Context, tx *sql.Tx, id string) error {
	type record struct {
		scope, key string
		raw        []byte
	}
	rows, err := tx.QueryContext(ctx, "SELECT scope,key,value FROM app_state WHERE scope IN ('playlists','queues','now-playing')")
	if err != nil {
		return err
	}
	var records []record
	for rows.Next() {
		var r record
		if err = rows.Scan(&r.scope, &r.key, &r.raw); err != nil {
			rows.Close()
			return err
		}
		records = append(records, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	for _, r := range records {
		var value any
		switch r.scope {
		case "playlists":
			var p userdata.Playlist
			if err = json.Unmarshal(r.raw, &p); err != nil {
				return err
			}
			if !slices.Contains(p.IDs, id) {
				continue
			}
			p.IDs = slices.DeleteFunc(p.IDs, func(v string) bool { return v == id })
			p.Changed = stamp
			value = p
		case "queues":
			var q userdata.Queue
			if err = json.Unmarshal(r.raw, &q); err != nil {
				return err
			}
			if !slices.Contains(q.IDs, id) && q.Current != id {
				continue
			}
			q.IDs = slices.DeleteFunc(q.IDs, func(v string) bool { return v == id })
			if q.Current == id {
				q.Current = ""
				q.Position = 0
			}
			q.Changed = stamp
			value = q
		case "now-playing":
			var p userdata.Playing
			if err = json.Unmarshal(r.raw, &p); err != nil {
				return err
			}
			if p.ID != id {
				continue
			}
			if _, err = tx.ExecContext(ctx, "DELETE FROM app_state WHERE scope=? AND key=?", r.scope, r.key); err != nil {
				return err
			}
			continue
		}
		body, e := json.Marshal(value)
		if e != nil {
			return e
		}
		if _, err = tx.ExecContext(ctx, "UPDATE app_state SET value=? WHERE scope=? AND key=?", body, r.scope, r.key); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, "DELETE FROM app_state WHERE key=? AND (scope LIKE 'bookmarks/%' OR scope LIKE 'annotations/%')", id)
	return err
}

func (c Catalog) PendingDeletions(ctx context.Context, root string) ([]library.Deletion, error) {
	rows, err := c.DB.QueryContext(ctx, "SELECT track_id,paths_json,files_deleted FROM library_deletions WHERE root_id=? AND files_deleted=0", root)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []library.Deletion{}
	for rows.Next() {
		d, e := readDeletion(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (c Catalog) FinishDelete(ctx context.Context, root, id string) error {
	_, err := c.DB.ExecContext(ctx, "UPDATE library_deletions SET files_deleted=1,paths_json='[]' WHERE root_id=? AND track_id=?", root, id)
	return err
}

// Recheck tombstones in the same transaction as a write: a request may have
// validated a track just before another client deleted it.
func rejectDeletedTracks(ctx context.Context, tx *sql.Tx, ids []string) error {
	for _, id := range ids {
		if id == "" {
			continue
		}
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM library_deletions WHERE track_id=?", id).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return library.ErrNotFound
		}
	}
	return nil
}
func rejectDeletedState(ctx context.Context, tx *sql.Tx, scope, key string, raw []byte) error {
	if raw == nil {
		return nil
	}
	var ids []string
	switch {
	case scope == "playlists":
		var p userdata.Playlist
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		ids = p.IDs
	case scope == "queues":
		var q userdata.Queue
		if err := json.Unmarshal(raw, &q); err != nil {
			return err
		}
		ids = append(q.IDs, q.Current)
	case scope == "now-playing":
		var p userdata.Playing
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		ids = []string{p.ID}
	case strings.HasPrefix(scope, "annotations/"), strings.HasPrefix(scope, "bookmarks/"):
		ids = []string{key}
	}
	return rejectDeletedTracks(ctx, tx, ids)
}
