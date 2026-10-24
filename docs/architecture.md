# Architecture

Wavelength is one Go binary with independently authenticated HTTP listeners and an
embedded SQLite database. `cmd/wavelength` handles flags, signals and exit status.
`internal/app` constructs dependencies and owns their lifetime. Configuration is read
once by `internal/config`, using caarlos0/env/v11 with the existing environment names.

## Package boundaries

| Package | Responsibility |
| --- | --- |
| `library` | Read-only scanning, metadata, path IDs, catalog grouping and source access |
| `accounts` | Local accounts, credential storage and account settings |
| `userdata` | Playlists, bookmarks, queues, annotations, local history, stations and chat |
| `podcasts` | RSS/Atom subscriptions, download claims, episode files and worker lifetime |
| `sharing` | Public-link ownership, selected-track grants, expiry and revocation |
| `transcode` | Encoded streams, captions, converted assets and managed HLS sessions |
| `state` | Transactional record storage and typed serialization helpers |
| `database` | SQLite implementations of catalog, authentication and record storage |
| `auth` | OIDC browser sessions, native bearer authentication and CSRF protection |
| `transport/rpc` | The small protobuf/Connect/gRPC library contract |
| `transport/httpserver` | Native routing, authentication mounting and health endpoints |
| `transport/media` | HTTP range/HEAD playback and encoded response delivery |
| `transport/subsonic` | Legacy parameters, permission mapping, IDs and XML/JSON projections |

Feature services accept contexts and typed inputs. Their persistence dependencies are
interfaces; they do not import a transport or the SQLite implementation. The podcast
package uses HTTP as an outbound feed/download client, not as its application API.
The dependency test in `internal/architecture` protects these boundaries.

Subsonic receives its services explicitly. It does not read environment variables,
open SQLite, create account stores, download episodes or manage encoder processes.
Filesystem-derived IDs and response shapes remain there because they are part of the
legacy contract. Core track metadata and tag-based album grouping are shared.

## Startup and shutdown

The application opens SQLite and scans the configured root before serving. Native
defaults to 8080; opt-in legacy defaults to 8081. Every listener binds before any start
serving, and a listener failure stops the application. Shutdown drains HTTP, cancels
workers and waits for them before closing the library and database. HLS sessions own
their temporary files and remove them on expiry or shutdown.

Native bearer/OIDC authentication and local Subsonic accounts remain separate. No
identity mapping or native per-user role model is implied by sharing storage.

## Catalog and storage

Each scan rereads the supported files into memory. One SQLite transaction upserts the
result and removes rows absent from that scan. A failed scan preserves the previous
catalog. WAL, foreign keys and a busy timeout are enabled; the application uses one
database connection. Search text is normalized during ingestion for Unicode case
matching. Albums and artists are derived from track rows when requested.

Personal collections, credentials, shares and subscriptions retain their existing
record scopes and JSON layouts. The package extraction does not reset that state.

Music source files and any existing Navidrome data are never modified. IDs use root
and relative path, not content fingerprints. Podcast files and generated state are
stored separately in Wavelength app data. Back up SQLite and the legacy encryption key
together. HLS uses temporary generated files with expiry and process cancellation.

External programs are optional for original playback; FFmpeg/ffprobe power conversion,
HLS and video probing. Discovery and listening history stay local.

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
