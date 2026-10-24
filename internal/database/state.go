package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/prayujt/wavelength/internal/state"
)

type StateStore struct{ DB *sql.DB }

func (s StateStore) Get(ctx context.Context, scope, key string) (json.RawMessage, error) {
	var value []byte
	err := s.DB.QueryRowContext(ctx, "SELECT value FROM app_state WHERE scope=? AND key=?", scope, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, state.ErrNotFound
	}
	return value, err
}
func (s StateStore) List(ctx context.Context, scope string) (map[string]json.RawMessage, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT key,value FROM app_state WHERE scope=? ORDER BY key", scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var key string
		var v []byte
		if err = rows.Scan(&key, &v); err != nil {
			return nil, err
		}
		out[key] = v
	}
	return out, rows.Err()
}
func (s StateStore) Update(ctx context.Context, scope, key string, update func(json.RawMessage) (json.RawMessage, error)) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var old []byte
	err = tx.QueryRowContext(ctx, "SELECT value FROM app_state WHERE scope=? AND key=?", scope, key).Scan(&old)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	value, err := update(old)
	if err != nil {
		return err
	}
	if value == nil {
		_, err = tx.ExecContext(ctx, "DELETE FROM app_state WHERE scope=? AND key=?", scope, key)
	} else {
		_, err = tx.ExecContext(ctx, "INSERT INTO app_state (scope,key,value) VALUES (?,?,?) ON CONFLICT(scope,key) DO UPDATE SET value=excluded.value", scope, key, []byte(value))
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
