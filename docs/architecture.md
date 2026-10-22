# Architecture

Wavelength is a modular monolith with two independent HTTP listeners in one process.
Composition belongs in `cmd/wavelength`; packages live under `internal`.

- `library`: protocol-independent tracks, catalog store interface, read-only scanner,
  music-root confinement, stable path-based IDs, scan exclusion.
- `database`: SQLite connection policy, embedded ordered migrations, and the catalog
  store implementation. SQL stays here, never in a protocol handler.
- `playback`: original media streaming with HTTP range/HEAD support. Opens only
  indexed IDs through `os.Root`, preventing symlink/path traversal outside the root.
- `httpserver`: native JSON API and operational health endpoints, default port 8080.
- `subsonic`: isolated legacy XML/JSON projection and authentication, default port
  8081, opt-in via password. It calls library/playback, not SQLite. Protocol-specific
  artist/album views currently derive from catalog tags rather than owning core data.

There is no legacy Git fork: the adapter lives in its own package in the same repo,
so both protocols use one implementation of indexing, storage, and streaming.

SQLite uses WAL, foreign keys, a busy timeout, and one connection initially. Filesystem
walking/tag reading happens outside the write transaction. A completed scan upserts
all discovered tracks and prunes obsolete rows atomically. Failed scans cannot prune.
Catalog queries are scoped to the currently configured root; tracks from a different
root in the same database are not exposed. Listing/count use one read transaction.

The source music directory and Navidrome database are never written to. Catalog IDs
are hashes of canonical root + relative path, not a content fingerprint. This keeps
rescans/restarts stable but does not preserve identity through moves or renames.

## Current scope

One music root, startup/manual full scans, embedded tags, native search/list/detail,
original-file streaming, and a small legacy compatibility subset. Both listeners
bind before either starts serving, and both stop on process shutdown/listener failure.
Native auth uses an optional bearer token on loopback and requires it elsewhere.
Legacy auth uses separate username/password with the protocol's salted MD5 token
scheme. Neither listener provides TLS; that belongs at the deployment proxy.

## Next milestones

1. Exercise the legacy adapter with the owner's chosen client and add its required
   endpoints; do not claim full Subsonic compatibility based only on protocol tests.
2. Incremental/background indexing, duration/artwork extraction, catalog-level
   artist/album identity and richer native browsing.
3. Persistent user-owned data: playlists, favorites, listening history.
4. FFmpeg transcoding only after original playback works in the chosen client.
5. Add concrete extension interfaces driven by actual integrations, not a speculative
   plugin framework. Keep the library core independent of legacy payloads.
