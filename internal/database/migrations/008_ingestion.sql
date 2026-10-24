CREATE TABLE ingest_jobs (
 id TEXT PRIMARY KEY,
 root_id TEXT NOT NULL REFERENCES library_roots(id),
 owner TEXT NOT NULL,
 request_id TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 state TEXT NOT NULL,
 body BLOB NOT NULL,
 created_at TEXT NOT NULL,
 UNIQUE(root_id, owner, request_id)
);
CREATE INDEX ingest_jobs_queue ON ingest_jobs(root_id, state, created_at, id);
CREATE INDEX ingest_jobs_owner ON ingest_jobs(root_id, owner, created_at, id);

CREATE TABLE track_origins (
 root_id TEXT NOT NULL REFERENCES library_roots(id),
 provider TEXT NOT NULL,
 external_id TEXT NOT NULL,
 track_id TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
 source_provider TEXT NOT NULL,
 source_id TEXT NOT NULL,
 sha256 TEXT NOT NULL,
 PRIMARY KEY(root_id, provider, external_id)
);
