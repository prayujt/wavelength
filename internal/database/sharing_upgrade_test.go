package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestLinkCleanupUpgradeRemovesOldRecordsAndCascades(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	old, err := sql.Open("sqlite", filepath.Join(dir, "wavelength.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec("CREATE TABLE schema_migrations (name TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() >= "010" {
			break
		}
		body, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := old.Exec(string(body)); err != nil {
			t.Fatal(err)
		}
		if _, err := old.Exec("INSERT INTO schema_migrations VALUES (?)", entry.Name()); err != nil {
			t.Fatal(err)
		}
	}
	for _, g := range []struct {
		id, kind, parent string
		expiry, revoked  int64
	}{
		{"active", "share", "", 0, 0},
		{"active-child", "playback", "active", time.Now().Add(time.Hour).UnixMilli(), 0},
		{"revoked", "share", "", 0, time.Now().UnixMilli()},
		{"revoked-child", "playback", "revoked", time.Now().Add(time.Hour).UnixMilli(), 0},
		{"expired", "share", "", time.Now().Add(-time.Hour).UnixMilli(), 0},
		{"expired-child", "playback", "expired", time.Now().Add(time.Hour).UnixMilli(), 0},
		{"expired-playback", "playback", "", time.Now().Add(-time.Hour).UnixMilli(), 0},
	} {
		_, err := old.Exec("INSERT INTO media_grants VALUES (?,?,?,?,?,?,?,?,?,?,?)", g.id, g.id, "alice", "root", g.kind, g.id, `["track"]`, g.parent, time.Now().UnixMilli(), g.expiry, g.revoked)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow("SELECT count(*) FROM media_grants").Scan(&count); err != nil || count != 2 {
		t.Fatal("migration retained old records", count, err)
	}
	if _, err := db.Query("SELECT revoked_at FROM media_grants"); err == nil {
		t.Fatal("soft deletion column remains")
	}
	if _, err := db.Exec("DELETE FROM media_grants WHERE id='active'"); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*) FROM media_grants").Scan(&count); err != nil || count != 0 {
		t.Fatal("share deletion did not remove children", count, err)
	}
}
