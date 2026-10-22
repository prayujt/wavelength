CREATE TABLE tracks (
 id TEXT PRIMARY KEY,
 root_id TEXT NOT NULL REFERENCES library_roots(id),
 path TEXT NOT NULL,
 title TEXT NOT NULL,
 artist TEXT NOT NULL DEFAULT '',
 album TEXT NOT NULL DEFAULT '',
 album_artist TEXT NOT NULL DEFAULT '',
 track_number INTEGER NOT NULL DEFAULT 0,
 disc_number INTEGER NOT NULL DEFAULT 0,
 year INTEGER NOT NULL DEFAULT 0,
 content_type TEXT NOT NULL,
 size INTEGER NOT NULL,
 modified_at TEXT NOT NULL,
 scan_id TEXT NOT NULL,
 UNIQUE(root_id, path)
);
CREATE INDEX tracks_browse ON tracks(root_id, artist, album, disc_number, track_number, path);
