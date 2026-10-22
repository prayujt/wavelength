package database

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prayujt/wavelength/internal/auth"
)

func TestAuthFlowIsConsumedExactlyOnce(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := AuthStore{DB: db}
	flow := auth.Flow{TokenHash: "hash", StateHash: "state", Nonce: "nonce", Verifier: "verifier", ExpiresAt: time.Now().Add(time.Minute)}
	if err = store.SaveFlow(ctx, flow); err != nil {
		t.Fatal(err)
	}
	var consumed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := store.ConsumeFlow(ctx, "hash")
			if err == nil {
				if got.Nonce != "nonce" {
					t.Error("flow corrupted")
				}
				consumed.Add(1)
			} else if !errors.Is(err, auth.ErrUnauthenticated) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if consumed.Load() != 1 {
		t.Fatalf("flow consumed %d times", consumed.Load())
	}
	flow.ExpiresAt = time.Now().Add(-time.Minute)
	if err = store.SaveFlow(ctx, flow); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ConsumeFlow(ctx, "hash"); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("expired flow accepted: %v", err)
	}
}

func TestAuthIdentitiesAreNotLinkedByEmailAndSessionsExpire(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := AuthStore{DB: db}
	users := []auth.User{
		{ID: "one", Issuer: "https://issuer-one.test", Subject: "subject", Email: "same@example.test"},
		{ID: "two", Issuer: "https://issuer-two.test", Subject: "subject", Email: "same@example.test"},
		{ID: "three", Issuer: "https://issuer-one.test", Subject: "other-subject", Email: "same@example.test"},
	}
	for _, user := range users {
		if err = store.CreateSession(ctx, auth.Session{TokenHash: user.ID, User: user, CSRF: "csrf", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM users").Scan(&count); err != nil || count != 3 {
		t.Fatalf("identities merged: %d %v", count, err)
	}
	if _, err = db.Exec("UPDATE sessions SET expires_at=0 WHERE token_hash='one'"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Session(ctx, "one"); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("expired session accepted: %v", err)
	}
	if session, err := store.Session(ctx, "two"); err != nil || session.User.ID != "two" {
		t.Fatalf("other session affected: %+v %v", session, err)
	}
}
