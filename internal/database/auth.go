package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/prayujt/wavelength/internal/auth"
)

// AuthStore persists OIDC identities, one-use login transactions and hashed sessions.
type AuthStore struct{ DB *sql.DB }

func (s AuthStore) SaveFlow(ctx context.Context, f auth.Flow) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM auth_flows WHERE expires_at<=?", time.Now().Unix()); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO auth_flows (token_hash,state_hash,nonce,verifier,expires_at) VALUES (?,?,?,?,?)", f.TokenHash, f.StateHash, f.Nonce, f.Verifier, f.ExpiresAt.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}
func (s AuthStore) ConsumeFlow(ctx context.Context, hash string) (auth.Flow, error) {
	f := auth.Flow{TokenHash: hash}
	var expires int64
	err := s.DB.QueryRowContext(ctx, `DELETE FROM auth_flows WHERE token_hash=? AND expires_at>? RETURNING state_hash,nonce,verifier,expires_at`, hash, time.Now().Unix()).Scan(&f.StateHash, &f.Nonce, &f.Verifier, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return f, auth.ErrUnauthenticated
	}
	f.ExpiresAt = time.Unix(expires, 0)
	return f, err
}
func (s AuthStore) CreateSession(ctx context.Context, session auth.Session) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at<=?", time.Now().Unix()); err != nil {
		return err
	}
	u := session.User
	if _, err = tx.ExecContext(ctx, `INSERT INTO users (id,issuer,subject,name,email) VALUES (?,?,?,?,?) ON CONFLICT(issuer,subject) DO UPDATE SET name=excluded.name,email=excluded.email`, u.ID, u.Issuer, u.Subject, u.Name, u.Email); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO sessions (token_hash,user_id,csrf,expires_at) VALUES (?,?,?,?)", session.TokenHash, u.ID, session.CSRF, session.ExpiresAt.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}
func (s AuthStore) Session(ctx context.Context, hash string) (auth.Session, error) {
	session := auth.Session{TokenHash: hash}
	var expires int64
	err := s.DB.QueryRowContext(ctx, `SELECT u.id,u.issuer,u.subject,u.name,u.email,s.csrf,s.expires_at FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.expires_at>?`, hash, time.Now().Unix()).Scan(&session.User.ID, &session.User.Issuer, &session.User.Subject, &session.User.Name, &session.User.Email, &session.CSRF, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return session, auth.ErrUnauthenticated
	}
	session.ExpiresAt = time.Unix(expires, 0)
	return session, err
}
func (s AuthStore) DeleteSession(ctx context.Context, hash string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash=?", hash)
	return err
}
