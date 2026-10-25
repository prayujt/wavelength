CREATE TABLE media_grants (
    id TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    owner TEXT NOT NULL,
    root_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('share', 'playback')),
    name TEXT NOT NULL,
    track_ids TEXT NOT NULL,
    parent_id TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL DEFAULT 0,
    revoked_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX media_grants_owner ON media_grants(owner, root_id, kind, created_at DESC, id);
CREATE INDEX media_grants_expiry ON media_grants(expires_at) WHERE kind = 'playback';
