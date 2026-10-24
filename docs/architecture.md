# Architecture

Wavelength is one Go binary with independently authenticated HTTP listeners and an
embedded SQLite database. `cmd/wavelength` handles flags, signals and exit status.
`internal/app` constructs dependencies and owns their lifetime. Configuration is read
once by `internal/config`, using caarlos0/env/v11 with the existing environment names.

## Package boundaries

| Package | Responsibility |
| --- | --- |
| `library` | Read-only scanning, metadata, path IDs, catalog grouping and source access |
| `userdata` | Playlists, bookmarks, queues and local listening history |
| `transcode` | Encoded audio streams and managed audio HLS sessions |
| `state` | Transactional record storage and typed serialization helpers |
| `database` | SQLite implementations of catalog, authentication and record storage |
| `auth` | OIDC browser sessions, native bearer authentication and CSRF protection |
| `transport/rpc` | The small protobuf/Connect/gRPC library contract |
| `transport/httpserver` | Native routing, authentication mounting and health endpoints |
| `transport/media` | HTTP range/HEAD playback and encoded response delivery |
| `transport/subsonic` | Legacy credentials, parameters, IDs, XML/JSON projections and stateless stubs |

Feature services accept contexts and typed inputs. Their persistence dependencies are
interfaces; they do not import a transport or the SQLite implementation.
The dependency test in `internal/architecture` protects these boundaries.

Subsonic receives its services explicitly. It does not read environment variables,
open SQLite or manage encoder processes. Removed legacy features are stateless
compatibility handlers; no account, sharing or podcast services are constructed.
Filesystem-derived IDs and response shapes remain there because they are part of the
legacy contract. Core track metadata and tag-based album grouping are shared.

## Startup and shutdown

The application opens SQLite and scans the configured root before serving. Native
defaults to 8080; opt-in legacy defaults to 8081. Every listener binds before any start
serving, and a listener failure stops the application. Shutdown drains HTTP, cancels
workers and waits for them before closing the library and database. HLS sessions own
their temporary files and remove them on expiry or shutdown.

Native bearer/OIDC authentication and the single environment-configured Subsonic
identity remain separate. Stored legacy accounts are not consulted. User-management
requests cannot change the configured credentials. No identity mapping or native
per-user role model is implied by sharing storage.

## Catalog and storage

Each scan rereads the supported files into memory. One SQLite transaction upserts the
result and removes rows absent from that scan. A failed scan preserves the previous
catalog. WAL, foreign keys and a busy timeout are enabled; the application uses one
database connection. Search text is normalized during ingestion for Unicode case
matching. Albums and artists are derived from track rows when requested.

Playlists, bookmarks, queues and listening history retain their record scopes and
JSON layouts. Listening counts use the historical `annotations/<user>` scope, but
stars and ratings are no longer read into the model or written by compatibility calls.
Old account, share, podcast, radio and chat records are ignored; existing files are
not deleted as a side effect of removing the services.

Music source files and any existing Navidrome data are never modified. IDs use root
and relative path, not content fingerprints. Generated application state is stored
separately in Wavelength app data. The legacy password encryption key is no longer
used or created. HLS uses temporary
generated files with expiry and process cancellation.

External programs are optional for original playback; FFmpeg/ffprobe power conversion,
audio HLS and richer audio metadata. Video files are excluded from the catalog.
Discovery and listening history stay local.

## Adding a capability or transport

Put a new capability in a focused feature package with the methods its callers need.
Define small dependency interfaces where that package consumes storage, tracks or an
external system, then assemble implementations in `app`. A downloader can own its
download policy and request a library scan after writing files. An MCP transport can
call the same feature services with its own authentication and response mapping.

Keep protocol DTOs, parameter parsing and status codes in the relevant transport.
The native v1 contract currently covers library browsing, scans and playback; adding
services does not automatically expand it. There is no plugin registry or parallel
implementation of feature behavior inside the legacy adapter.

See [OIDC](oidc.md) and [legacy compatibility](subsonic-compatibility.md) for current
boundaries. Actual-client certification, incremental indexing, a browser UI and finer
native authorization remain future work.
