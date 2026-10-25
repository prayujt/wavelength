package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/prayujt/wavelength/internal/listening"
)

type ListeningStore struct{ DB *sql.DB }

func scanSession(row interface{ Scan(...any) error }) (listening.Session, error) {
	var body string
	var session listening.Session
	if err := row.Scan(&body); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = listening.ErrNotFound
		}
		return session, err
	}
	err := json.Unmarshal([]byte(body), &session)
	return session, err
}

func (s ListeningStore) Create(ctx context.Context, session listening.Session) (listening.Session, error) {
	body, err := json.Marshal(session)
	if err != nil {
		return listening.Session{}, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return listening.Session{}, err
	}
	defer tx.Rollback()
	if err = rejectDeletedTracks(ctx, tx, []string{session.Track.ID}); err != nil {
		return listening.Session{}, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO listening_sessions(owner,root_id,id,track_id,artist_id,album_id,origin,started_at,session_json) VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT(owner,root_id,id) DO NOTHING`, session.Owner, session.RootID, session.ID, session.Track.ID, session.ArtistID, session.AlbumID, session.Origin, session.StartedAt.UnixMilli(), string(body))
	if err != nil {
		return listening.Session{}, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return listening.Session{}, err
	}
	if inserted == 1 && session.CountPlay {
		// Session insertion and aggregate count commit together. Retried offline starts
		// cannot increment twice, including after an uncertain HTTP response.
		stamp := session.StartedAt.Format(time.RFC3339Nano)
		_, err = tx.ExecContext(ctx, `INSERT INTO app_state(scope,key,value) VALUES(?,?,json_object('plays',1,'last_played',?))
		ON CONFLICT(scope,key) DO UPDATE SET value=json_set(app_state.value,'$.plays',coalesce(json_extract(app_state.value,'$.plays'),0)+1,'$.last_played',
		CASE WHEN julianday(?) > coalesce(julianday(json_extract(app_state.value,'$.last_played')),0) THEN ? ELSE json_extract(app_state.value,'$.last_played') END)`, "annotations/"+session.Owner, session.Track.ID, stamp, stamp, stamp)
		if err != nil {
			return listening.Session{}, err
		}
	}
	stored, err := scanSession(tx.QueryRowContext(ctx, "SELECT session_json FROM listening_sessions WHERE owner=? AND root_id=? AND id=?", session.Owner, session.RootID, session.ID))
	if err != nil {
		return listening.Session{}, err
	}
	return stored, tx.Commit()
}

func (s ListeningStore) Update(ctx context.Context, owner, root, id string, update func(*listening.Session) error) (listening.Session, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return listening.Session{}, err
	}
	defer tx.Rollback()
	session, err := scanSession(tx.QueryRowContext(ctx, "SELECT session_json FROM listening_sessions WHERE owner=? AND root_id=? AND id=?", owner, root, id))
	if err != nil {
		return listening.Session{}, err
	}
	if err := update(&session); err != nil {
		return listening.Session{}, err
	}
	body, err := json.Marshal(session)
	if err != nil {
		return listening.Session{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE listening_sessions SET session_json=? WHERE owner=? AND root_id=? AND id=?", string(body), owner, root, id); err != nil {
		return listening.Session{}, err
	}
	return session, tx.Commit()
}

func (s ListeningStore) List(ctx context.Context, owner, root string, q listening.Query) (listening.Page, error) {
	where := []string{"owner=?", "root_id=?"}
	args := []any{owner, root}
	for _, f := range []struct{ column, value string }{{"track_id", q.TrackID}, {"artist_id", q.ArtistID}, {"album_id", q.AlbumID}, {"origin", q.Origin}, {"json_extract(session_json,'$.Outcome')", q.Outcome}} {
		if f.value != "" {
			where = append(where, f.column+"=?")
			args = append(args, f.value)
		}
	}
	if !q.Since.IsZero() {
		where = append(where, "started_at>=?")
		args = append(args, q.Since.UnixMilli())
	}
	if !q.Until.IsZero() {
		where = append(where, "started_at<?")
		args = append(args, q.Until.UnixMilli())
	}
	clause := strings.Join(where, " AND ")
	out := listening.Page{Sessions: []listening.Session{}}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM listening_sessions WHERE "+clause, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT session_json FROM listening_sessions WHERE "+clause+" ORDER BY started_at DESC,id LIMIT ? OFFSET ?", append(args, q.Limit, q.Offset)...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			rows.Close()
			return out, err
		}
		out.Sessions = append(out.Sessions, session)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}
