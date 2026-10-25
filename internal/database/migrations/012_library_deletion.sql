-- Retain completed tombstones for idempotency and to reject stale collection writes.
CREATE TABLE library_deletions (
 root_id TEXT NOT NULL REFERENCES library_roots(id),
 track_id TEXT NOT NULL,
 paths_json TEXT NOT NULL,
 files_deleted INTEGER NOT NULL DEFAULT 0,
 created_at TEXT NOT NULL,
 PRIMARY KEY(root_id, track_id)
);
CREATE INDEX library_deletions_pending ON library_deletions(root_id, files_deleted);
CREATE INDEX library_deletions_track ON library_deletions(track_id);
