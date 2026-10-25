DELETE FROM media_grants WHERE revoked_at <> 0 OR (expires_at > 0 AND expires_at <= unixepoch('now') * 1000);
DELETE FROM media_grants WHERE parent_id <> '' AND NOT EXISTS (SELECT 1 FROM media_grants AS parent WHERE parent.id = media_grants.parent_id);
CREATE TABLE media_grants_next (
    id TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    owner TEXT NOT NULL,
    root_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('share', 'playback')),
    name TEXT NOT NULL,
    track_ids TEXT NOT NULL,
    parent_id TEXT REFERENCES media_grants_next(id) ON DELETE CASCADE,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL DEFAULT 0
);
INSERT INTO media_grants_next SELECT id, token_hash, owner, root_id, kind, name, track_ids, NULLIF(parent_id, ''), created_at, expires_at FROM media_grants;
DROP TABLE media_grants;
ALTER TABLE media_grants_next RENAME TO media_grants;
CREATE INDEX media_grants_owner ON media_grants(owner, root_id, kind, created_at DESC, id);
CREATE INDEX media_grants_expiry ON media_grants(expires_at) WHERE expires_at > 0;
CREATE INDEX media_grants_parent ON media_grants(parent_id);
