package database

import (
	"context"
	"testing"
)

func TestReopenPreservesDataAndDoesNotRepeatMigrations(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO library_roots (id, path) VALUES (?, ?)", "library-1", "/music"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var path string
	if err = db.QueryRow("SELECT path FROM library_roots WHERE id = ?", "library-1").Scan(&path); err != nil {
		t.Fatal(err)
	}
	if path != "/music" {
		t.Fatalf("unexpected path %q", path)
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("migration count %d", count)
	}
	for pragma, want := range map[string]int{"foreign_keys": 1, "busy_timeout": 5000} {
		var got int
		if err = db.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s = %d; want %d", pragma, got, want)
		}
	}
	var mode string
	if err = db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal mode %s", mode)
	}
}
