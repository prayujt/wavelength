# Architecture

Wavelength is a modular monolith: one Go binary, two independently authenticated HTTP
listeners, one embedded SQLite database. Composition is in `cmd/wavelength`.

- `library`: protocol-independent catalog, read-only scanning, stable path IDs,
  root-confined file access, artwork/lyrics and optional ffprobe metadata.
- `database`: SQL, migrations, SQLite catalog/auth/state adapters. WAL, foreign keys,
  busy timeout and one connection. Scans commit complete snapshots atomically.
- `state`: protocol-owned durable JSON state and the legacy credential encryption key.
- `playback`: original-file HTTP range/HEAD streaming.
- `auth`: provider-neutral OIDC, persisted hashed sessions, native bearer/CSRF checks.
- `rpc`: native Connect implementation of `proto/wavelength/v1/library.proto`, including
  binary audio server streaming. Generated messages/clients live under `gen/`.
- `httpserver`: native Connect mounting, range-capable `/media/`, auth and health.
- `subsonic`: isolated legacy projection, accounts/roles, user state, schema-shaped XML,
  media conversion, shares and RSS podcasts.

Native defaults to 8080; opt-in legacy defaults to 8081. Both bind before serving and
stop together. Subsonic is a package, not a separate Git fork or divergent database.
No native credential is accepted by the legacy adapter, or vice versa.

Music source files and any existing Navidrome data are never modified. IDs use root
and relative path, not content fingerprints. Podcast files and generated state are
stored separately in Wavelength app data. Back up SQLite and the legacy encryption key
together. HLS uses temporary generated files with expiry and process cancellation.

External programs are optional for original playback; FFmpeg/ffprobe power conversion,
HLS and video probing. Discovery and listening history stay local.

See [OIDC](oidc.md) and [legacy compatibility](subsonic-compatibility.md) for current
boundaries. Future work includes actual-client certification, incremental indexing,
more native services/UI and finer-grained native authorization.
