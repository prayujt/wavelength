package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestCatalogAuthorityUpgradePreservesRecordsAndRevisions(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	legacy, err := sql.Open("sqlite", filepath.Join(dir, "wavelength.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = legacy.Exec("CREATE TABLE schema_migrations(name TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() >= "006" {
			break
		}
		body, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = legacy.Exec(string(body)); err != nil {
			t.Fatal(err)
		}
		if _, err = legacy.Exec("INSERT INTO schema_migrations(name) VALUES (?)", entry.Name()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = legacy.Exec("INSERT INTO library_roots(id,path) VALUES ('root','/music')"); err != nil {
		t.Fatal(err)
	}
	if _, err = legacy.Exec(`INSERT INTO tracks(id,root_id,path,title,content_type,size,modified_at,scan_id) VALUES ('track','root','one.mp3','One','audio/mpeg',4,'2020-01-01T00:00:00Z','old-scan')`); err != nil {
		t.Fatal(err)
	}
	if _, err = legacy.Exec(`INSERT INTO app_state(scope,key,value) VALUES ('playlists','saved','{"name":"Preserved","IDs":["track"]}')`); err != nil {
		t.Fatal(err)
	}
	if err = legacy.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var id, title, value string
	if err = db.QueryRow("SELECT id,title FROM tracks").Scan(&id, &title); err != nil || id != "track" || title != "One" {
		t.Fatal(id, title, err)
	}
	if err = db.QueryRow("SELECT value FROM app_state WHERE scope='playlists'").Scan(&value); err != nil || value != `{"name":"Preserved","IDs":["track"]}` {
		t.Fatal(value, err)
	}
	if _, err = db.Query("SELECT scan_id FROM tracks"); err == nil {
		t.Fatal("obsolete scan column remains")
	}
	catalog := Catalog{DB: db}
	before, err := catalog.Revision(ctx, "root")
	if err != nil || before <= 0 {
		t.Fatal(before, err)
	}
	for _, statement := range []string{"UPDATE tracks SET title='Changed' WHERE id='track'", "DELETE FROM tracks WHERE id='track'"} {
		if _, err = db.Exec(statement); err != nil {
			t.Fatal(err)
		}
		after, err := catalog.Revision(ctx, "root")
		if err != nil || after <= before {
			t.Fatal("catalog change did not invalidate caches", after, err)
		}
		before = after
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	after, err := (Catalog{DB: db}).Revision(ctx, "root")
	if err != nil || after != before {
		t.Fatal("restart changed catalog revision", after, err)
	}
}

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
	if count != 6 {
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
