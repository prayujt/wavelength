# Architecture

Wavelength starts as a modular monolith. Composition belongs in `cmd/wavelength`;
implementation packages live under `internal`. Use the standard library where practical.

- `internal/database`: embedded SQLite, connection policy, ordered transactional migrations.
- `internal/httpserver`: HTTP lifecycle/health surface. Future protocol adapters call application services, not SQL.
- Future `internal/library`: catalog model and indexing use cases, independent of Subsonic payloads.
- Future `internal/subsonic`: compatibility adapter; implement endpoints demanded by a real client.
- Future `internal/playback`: original-file streaming, HTTP ranges, then FFmpeg transcoding.

SQLite uses WAL, foreign keys, a busy timeout, and one connection initially. Avoid long
write transactions during indexing. Add read concurrency only when measurements justify it.
Migrations are embedded and applied in one transaction with a schema-version ledger.

## Next vertical slice

1. Read-only music directory scan and metadata extraction; stable catalog IDs persisted in SQLite.
2. Client authentication, library browsing/search, artwork and original-file streaming.
3. Verify playback using the owner's actual OpenSubsonic client alongside Navidrome.

Do not modify the existing music library or Navidrome database. No plugin framework,
frontend, distributed services or transcoding until the basic playback slice works.
Plugin boundaries will follow concrete extension needs rather than speculative interfaces.

## Current acceptance

The binary starts with a fresh data directory, persists its migration version across
restarts, reports process health and database readiness, and shuts down on SIGTERM/SIGINT.
