CREATE TABLE listening_sessions (
    owner TEXT NOT NULL,
    root_id TEXT NOT NULL,
    id TEXT NOT NULL,
    track_id TEXT NOT NULL,
    artist_id TEXT NOT NULL,
    album_id TEXT NOT NULL,
    origin TEXT NOT NULL,
    started_at INTEGER NOT NULL,
    session_json TEXT NOT NULL,
    PRIMARY KEY (owner, root_id, id)
);
CREATE INDEX listening_sessions_recent ON listening_sessions(owner, root_id, started_at DESC, id);
CREATE INDEX listening_sessions_track ON listening_sessions(owner, root_id, track_id, started_at DESC);
CREATE INDEX listening_sessions_artist ON listening_sessions(owner, root_id, artist_id, started_at DESC);
CREATE INDEX listening_sessions_album ON listening_sessions(owner, root_id, album_id, started_at DESC);
