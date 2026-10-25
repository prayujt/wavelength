package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/prayujt/wavelength/internal/sharing"
)

type GrantStore struct{ DB *sql.DB }

const grantColumns = "id,token_hash,owner,root_id,kind,name,track_ids,parent_id,created_at,expires_at,revoked_at"

func millis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}
func fromMillis(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.UnixMilli(v).UTC()
}
func scanGrant(row interface{ Scan(...any) error }) (sharing.Grant, error) {
	var g sharing.Grant
	var ids string
	var created, expires, revoked int64
	err := row.Scan(&g.ID, &g.TokenHash, &g.Owner, &g.RootID, &g.Kind, &g.Name, &ids, &g.ParentID, &created, &expires, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return g, sharing.ErrNotFound
	}
	if err != nil {
		return g, err
	}
	g.CreatedAt, g.ExpiresAt, g.RevokedAt = fromMillis(created), fromMillis(expires), fromMillis(revoked)
	err = json.Unmarshal([]byte(ids), &g.TrackIDs)
	return g, err
}
func (s GrantStore) Create(ctx context.Context, g sharing.Grant) error {
	ids, err := json.Marshal(g.TrackIDs)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Ephemeral playback grants have no history to retain after expiry.
	if _, err = tx.ExecContext(ctx, "DELETE FROM media_grants WHERE kind='playback' AND expires_at<=?", millis(g.CreatedAt)); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO media_grants ("+grantColumns+") VALUES (?,?,?,?,?,?,?,?,?,?,?)", g.ID, g.TokenHash, g.Owner, g.RootID, g.Kind, g.Name, string(ids), g.ParentID, millis(g.CreatedAt), millis(g.ExpiresAt), millis(g.RevokedAt))
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s GrantStore) Get(ctx context.Context, id string) (sharing.Grant, error) {
	return scanGrant(s.DB.QueryRowContext(ctx, "SELECT "+grantColumns+" FROM media_grants WHERE id=?", id))
}
func (s GrantStore) ByTokenHash(ctx context.Context, hash string) (sharing.Grant, error) {
	return scanGrant(s.DB.QueryRowContext(ctx, "SELECT "+grantColumns+" FROM media_grants WHERE token_hash=?", hash))
}

// Derived playback grants are managed through their parent share.
func (s GrantStore) List(ctx context.Context, owner, root, kind string, limit, offset int) ([]sharing.Grant, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT "+grantColumns+" FROM media_grants WHERE owner=? AND root_id=? AND kind=? AND parent_id='' ORDER BY created_at DESC,id LIMIT ? OFFSET ?", owner, root, kind, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []sharing.Grant{}
	for rows.Next() {
		g, err := scanGrant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
func (s GrantStore) Revoke(ctx context.Context, id, owner, root string, now time.Time) error {
	result, err := s.DB.ExecContext(ctx, "UPDATE media_grants SET revoked_at=CASE WHEN revoked_at=0 THEN ? ELSE revoked_at END WHERE id=? AND owner=? AND root_id=?", millis(now), id, owner, root)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return sharing.ErrNotFound
	}
	return err
}
