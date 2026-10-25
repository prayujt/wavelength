# Architecture

Wavelength is one Go binary with independently authenticated HTTP listeners and an
embedded SQLite database. `cmd/wavelength` handles flags, signals and exit status.
`internal/app` constructs dependencies and owns their lifetime. Configuration is read
once by `internal/config`, using caarlos0/env/v11 with the existing environment names.

## Package boundaries

| Package | Responsibility |
| --- | --- |
| `library` | Persisted catalog grouping, path IDs and confined source access |
| `discovery` | External metadata, provider references and conservative source selection |
| `ingest` | Durable acquisition jobs, media preparation and explicit publication |
| `realtime` | Bounded in-process delivery of committed changes to connected clients |
| `providers/itunes`, `providers/youtube`, `providers/jev` | Bounded metadata, yt-dlp and optional Jev ranking implementations |
| `sharing` | Scoped, revocable public shares and temporary playback grants |
| `userdata` | Playlists, bookmarks, queues and local listening history |
| `transcode` | Encoded audio streams and managed audio HLS sessions |
| `state` | Transactional record storage and typed serialization helpers |
| `database` | SQLite implementations of catalog, authentication and record storage |
| `auth` | OIDC browser sessions, delegated OAuth, scoped API authentication and CSRF |
| `transport/rpc` | Protobuf/Connect/gRPC library and personal-state contracts |
| `transport/mcp` | Stateless Streamable HTTP tools over the same feature services |
| `transport/httpserver` | Native routing, authentication mounting and health endpoints |
| `transport/sharing` | Public share metadata, scoped artwork and capability-authorized media |
| `transport/media` | HTTP range/HEAD playback, authenticated artwork, and encoded response delivery |
| `transport/subsonic` | Legacy credentials, parameters, IDs, XML/JSON projections and stateless stubs |

Feature services accept contexts and typed inputs. Their persistence dependencies are
interfaces; they do not import a transport or the SQLite implementation.
The dependency test in `internal/architecture` protects these boundaries.

Subsonic receives its services explicitly. It does not read environment variables,
open SQLite or manage encoder processes. Removed legacy features are stateless
compatibility handlers; no account, sharing or podcast backend is constructed for
that boundary. Native sharing has its own feature service.
Filesystem-derived IDs and response shapes remain there because they are part of the
legacy contract. Core track metadata and tag-based album grouping are shared.

## Startup and shutdown

The application opens SQLite and the configured source root without enumerating
files or probing metadata. It serves the persisted catalog immediately. Native
defaults to 8080; opt-in legacy defaults to 8081. Every listener binds before any start
serving, and a listener failure stops the application. Shutdown drains HTTP, cancels
workers and waits for them before closing the library and database. HLS sessions own
their temporary files and remove them on expiry or shutdown.

Native bearer/OIDC authentication and the single environment-configured Subsonic
identity remain separate. Stored legacy accounts are not consulted. User-management
requests cannot change the configured credentials. Native/MCP personal state uses
the verified OIDC issuer/subject's `oidc:<identity-id>` namespace. No legacy account
is mapped to a named user. Default-private playlists, queues, bookmarks and history
are shared across a listener's native/MCP sessions; only explicit public playlists
are readable by other users, and all playlist writes require ownership.

## Catalog and storage

SQLite is authoritative. Startup never imports or reconciles source files, and a
fresh database remains empty until explicit ingestion jobs supply records.
The scanner, metadata probing, native scan RPCs and scan-only CLI flag are removed.
Legacy scan controls report the stored count with `scanning: false` and perform no
work. Catalog revisions are persisted and updated by track-table triggers for
legacy cache invalidation, including removals; restarts leave the revision intact.

The schema upgrade preserves existing track IDs and metadata, removes obsolete
scan bookkeeping, and keeps user state. WAL, foreign keys and a busy timeout remain
enabled. Search uses the stored normalized text. Albums and artists are derived
from track rows when requested.

Native catalog responses include authenticated artwork paths derived from track
IDs; listing tracks never reads tags to populate them. Artwork and saved lyrics
are read on demand through the same confined library operations as the legacy
adapter. `GetLyrics` / `get_lyrics` do not fetch lyrics from external services.
Import lookup maps exact provider provenance to catalog IDs, independent of job
ownership, because completed tracks belong to the shared library.

`RealtimeService.Subscribe` sends a protobuf `oneof` of ready, import-update,
library-publication, and heartbeat messages over Connect/gRPC server streaming.
Job snapshots go only to their owner; publication messages describe the shared
catalog. The hub never blocks ingestion on a slow reader. A disconnected or slow
client reconnects and reads fresh snapshots after ready; notifications are not
a durable event log. SQLite remains authoritative. The web app shares one
subscription across pages and removes its former download polling loop.

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

External programs are optional for original playback; FFmpeg powers conversion
and audio HLS. Ingestion probes only newly downloaded audio; video indexing is absent.
Personal playlists and listening history stay local. Optional Jev ranking sends
only public music metadata to TypeSafe.

## Adding a capability or transport

Put a new capability in a focused feature package with the methods its callers need.
Define small dependency interfaces where that package consumes storage, tracks or an
external system, then assemble implementations in `app`. The ingestion service owns acquisition policy and publishes explicit catalog
records after preparing source files. Its SQLite queue and provenance table preserve
job state and deduplicate provider identities across users. Publication records the
track and item receipt in one transaction, while a persisted prepared-file checksum
recovers the filesystem/SQLite boundary after a crash. Recovery visits recorded
jobs only; it never reconciles a directory. One worker owns the queue in the single
application process. Artist/album folders remain useful to legacy folder browsers,
but metadata and identity come from the catalog. The MCP
transport calls the same library/userdata feature services as native RPCs, with its
own schemas and response mapping. It does not tunnel through a legacy or native
HTTP endpoint, enumerate files, or own a second persistence model.

Keep protocol DTOs, parameter parsing and status codes in the relevant transport.
The native v1 contract covers library browsing/playback and personal collections.
MCP exposes all of those operations except audio-byte streaming, returning private
playback information or explicitly created temporary bearer audio links instead.
DiscoveryService and IngestionService expose metadata lookup, recording selection,
and durable import jobs through native RPC and MCP. Imports require a named user
and wavelength:ingest; attaching a playlist also requires wavelength:write. There is no plugin registry or parallel
implementation of feature behavior inside the legacy adapter.

See [OIDC](oidc.md) and [legacy compatibility](subsonic-compatibility.md) for current
boundaries. Actual-client certification, a browser UI and finer
catalog authorization remain future work. Delegated OAuth reuses the existing OIDC
application and issues resource-bound local credentials for MCP and native clients;
see [OIDC](oidc.md#delegated-oauth-for-mcp-and-native-clients) for scopes and lifecycle.
