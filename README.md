# Wavelength

A self-hosted music server written in Go, with an embedded SQLite catalog,
Subsonic client compatibility, a Svelte web player, a native protobuf API, and
authenticated MCP. It
discovers music, acquires tagged audio, and serves its persistent catalog. It runs
as one binary and does not need an external database. The container includes the web app.

## Run with Docker

The image includes FFmpeg/ffprobe, yt-dlp, and Deno and supports Linux on amd64 and arm64.

```sh
export WAVELENGTH_TOKEN='choose-a-long-random-token'

docker run --rm --name wavelength \
  -p 127.0.0.1:8080:8080 \
  -e WAVELENGTH_TOKEN \
  -e WAVELENGTH_MUSIC_DIR=/music \
  --mount type=bind,src=/absolute/path/to/music,dst=/music \
  --mount type=volume,src=wavelength-data,dst=/data \
  docker.prayujt.com/wavelength:latest
```

Replace the source path with your music directory. Keep the data volume between
runs: it holds the catalog and application state. Music acquisition needs a
writable music mount. Playback alone works with a read-only mount. Imports create new files; existing source files are not rewritten.

Pushes to `main` publish `docker.prayujt.com/wavelength:latest` and a tag containing
the full commit SHA. Images are built with [ko](https://ko.build/) on an Alpine
runtime base. See the [build workflow](.github/workflows/build.yml).

## Web player

Configure [OIDC sign-in](docs/oidc.md) and open your public Wavelength URL. The web
app includes library and external music search with Cmd/Ctrl+K, dedicated artist
and album pages, artwork, playback, stored lyrics, and playlists. Downloads have
selection, review, recording choices, progress, cancellation, and retries. It uses typed
Connect RPC clients and your browser session; no API token is needed in the UI.

The Svelte 5 + TypeScript client lives in [`web/`](web/README.md). The container
serves its static build on the native listener. Local Go builds can serve it with
`WAVELENGTH_WEB_DIR=./web/dist` after `npm --prefix web ci` and
`npm --prefix web run build`. See the [web development guide](web/README.md) for
the development proxy and browser checks.

## Mobile app

The independent [React Native app](mobile/README.md) lives in `mobile/` and runs
in Expo Go on iOS and Android. It uses the existing native API and OAuth sign-in
for library browsing, discovery/downloads, playlists, playback, lyrics, sharing,
bookmarks, and listening history. Its dependencies, generated client, builds,
and CI are separate from the web/server image.

## Subsonic clients

Wavelength exposes a separate Subsonic-compatible listener for existing music
clients. It supports library browsing and search, playlists, bookmarks and playback
queues, cover art, lyrics, downloads, audio transcoding, and audio HLS. It uses the
same catalog and storage as the native API.

To enable it in the Docker command above, add these options before the image name:

```sh
-p 127.0.0.1:8081:8081 \
-e WAVELENGTH_SUBSONIC_ADDR=0.0.0.0:8081 \
-e WAVELENGTH_SUBSONIC_USER=admin \
-e WAVELENGTH_SUBSONIC_PASSWORD='choose-your-own-password'
```

Connect your client to `http://localhost:8081` using those credentials. There is one
configured account, independent of the native API's bearer token or OIDC sessions.
XML, JSON, JSONP, `.view` endpoints, and salted-token authentication are supported.
See the [compatibility guide](docs/subsonic-compatibility.md) for endpoint details
and verification coverage.

## Library and storage

Wavelength treats the existing SQLite catalog as authoritative. Startup opens the
database and begins serving without walking the music directory, reading tags, or
probing media. Files are opened on demand for playback, artwork, and lyrics.

The catalog lives in `wavelength.db` inside the data directory. Keep the existing
data volume and container-visible music root (for example `/music`) stable. Existing
track IDs and personal state are preserved. External file additions, moves, edits,
and deletions do not rewrite catalog records. A missing source fails playback
without deleting its metadata.

Automatic scans, native scan RPCs, and the `--scan-only` command have been removed.
The Subsonic scan endpoints report an idle state and the stored track count;
`startScan` performs no filesystem work. A fresh database starts empty even if the
music directory contains files. New music enters through explicit ingestion jobs.

SQLite also holds playlists, bookmarks, playback queues, and [detailed listening history](docs/listening.md),
including time listened, completion/skips, and artist/album filters through RPC and MCP.
Back up the data directory as well as your music collection.

## Discover and acquire music

Search external iTunes metadata, inspect artists and releases, find ranked YouTube
recordings, and import selected songs or albums. Downloads run as persistent jobs;
poll their progress, cancel unfinished work, or retry failed items with another
source. Completed tracks become available immediately through every interface.

The same services power native RPCs and MCP tools. Automatic matching accepts only
strong, unambiguous candidates; other imports pause for a source selection. Jobs are
private to their requester, while imported tracks enter the shared catalog. Optional
playlist imports preserve track order and append once every item succeeds.

Acquisition requires an OIDC user. Delegated clients also need `wavelength:ingest`.
The container includes the required media tools; no metadata-provider credentials
are needed. Optionally set `WAVELENGTH_JEV_API_KEY` to use Jev for recording ranking
in both the source picker and automatic imports. See [ingestion](docs/ingestion.md) for requests, job states, storage,
recovery, and runtime requirements.

## Native API

The native API browses and searches tracks, artists, and albums, returns playback
URLs, serves artwork, reads stored lyrics, and streams audio. Its `UserDataService` manages the signed-in listener's
playlists, queue, bookmarks, listening history, and now-playing state. The contract
is [library.proto](proto/wavelength/v1/library.proto), extended by
[discovery and ingestion](proto/wavelength/v1/ingestion.proto) and
[sharing](proto/wavelength/v1/sharing.proto), with
generated Go messages and Connect clients under `gen/`. The Connect handler serves
Connect, gRPC, and gRPC-Web clients.

```sh
curl http://127.0.0.1:8080/healthz

curl http://127.0.0.1:8080/wavelength.v1.LibraryService/ListTracks \
  -H "Authorization: Bearer $WAVELENGTH_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"limit":20}'

curl http://127.0.0.1:8080/media/tracks/TRACK_ID \
  -H "Authorization: Bearer $WAVELENGTH_TOKEN" \
  -H 'Range: bytes=0-1023'
```

`GetPlaybackInfo` returns a media path for HTTP playback. Media GET/HEAD requests
support byte ranges and seeking; `StreamAudio` provides server-streamed binary
chunks. Media requests use the same authentication as the native API.

Explicitly create temporary direct audio URLs for Sonos and other players with
`SharingService.CreatePlaybackLink`, or public player pages with `CreateShare`.
Playback links default to three hours; shares default to 30 days. Both are scoped
to selected music and revocable. Songs, albums and playlists have
share controls in the UI; **Links** manages public shares. See
[playback and sharing links](docs/sharing.md) for RPC/MCP usage and lifetimes.

For browser sign-in, configure a generic OIDC provider, register `/auth/callback`,
and visit `/auth/login`. Sessions authenticate API and media requests;
cookie-authenticated Connect POSTs also require `Origin` and `X-CSRF-Token`.
See [OIDC setup](docs/oidc.md). Terminate TLS at your deployment proxy.

## MCP for AI clients

With OIDC and a music library configured, connect a remote MCP client to
`https://music.example.com/mcp` on the native listener. The service offers 36 tools:

- External music search, artist/release lookup, and recording candidates.
- Create/get/list/cancel/retry ingestion jobs.
- Current user; browse/search/get local tracks, artists, and albums; playback links and stored lyrics.
- Match external song references to tracks already in the library.
- List/get/create/update/delete playlists, with visibility to other signed-in listeners.
- Create temporary playback URLs and public share pages; list and revoke your links.
- Read/save your queue; list/save/delete bookmarks.
- Read listening history, record plays, and read your own now-playing state.

These tools call the same library and personal-state services as the native API.
Playlists start private. Personal data uses the provider's stable issuer/subject
identity, shared by browser, native, and MCP sessions. The shared Subsonic account
does not impersonate those users. Public playlists are readable by other listeners;
only their owner may edit them.

Clients discover ordinary OAuth endpoints, register a client, use authorization
code with S256 PKCE, and sign in through the **existing** OIDC application. Wavelength
shows consent and issues its own scoped API credentials; clients never receive the
provider's client secret. No second Authentik application or new environment variables
are needed. The same credentials work for native RPCs. See [OAuth details](docs/oidc.md#delegated-oauth-for-mcp-and-native-clients).

The official Go MCP SDK serves stateless Streamable HTTP with JSON responses,
including legacy 2025 protocol versions. MCP returns private playback information
and can explicitly create temporary bearer URLs for external players; audio bytes
stay on HTTP/gRPC. New authorizations request read, personal-write, and
ingestion permissions; audio streaming is a separate scope. Existing connections
need a fresh authorization to gain ingestion permission.

## Configuration

Configuration is read from environment variables at startup.

| Variable | Purpose / default |
| --- | --- |
| `WAVELENGTH_MUSIC_DIR` | Music directory; unset starts without a library. |
| `WAVELENGTH_DATA_DIR` | Persistent data directory; `./data`, or `/data` in the image. |
| `WAVELENGTH_WEB_DIR` | Built web assets; unset disables serving them, `/web` in the image. |
| `WAVELENGTH_ADDR` | Native listener; `127.0.0.1:8080`, or `0.0.0.0:8080` in the image. |
| `WAVELENGTH_TOKEN` | Native API bearer token; required for non-loopback binding unless OIDC is configured. |
| `WAVELENGTH_SUBSONIC_PASSWORD` | Enables the Subsonic listener; requires a music directory. |
| `WAVELENGTH_SUBSONIC_USER` | Subsonic username; `admin`. |
| `WAVELENGTH_SUBSONIC_ADDR` | Subsonic listener; `127.0.0.1:8081`. Use `0.0.0.0:8081` in a container. |
| `WAVELENGTH_PUBLIC_URL` | Public origin for OIDC, such as `https://music.example.com`. |
| `WAVELENGTH_OIDC_ISSUER` | OIDC provider issuer URL. |
| `WAVELENGTH_OIDC_CLIENT_ID` | OIDC client ID. |
| `WAVELENGTH_OIDC_CLIENT_SECRET` | OIDC client secret, if required by your provider. |
| `WAVELENGTH_JEV_API_KEY` | Optional backend-only TypeSafe API key; enables Jev recording ranking. Unset preserves heuristic ranking. |
| `WAVELENGTH_JEV_MODEL` | Jev model; pinned to `jev-1.14.0` by default. |
| `WAVELENGTH_INGEST_CONCURRENCY` | Concurrent acquisition items across jobs and within albums; `2`, allowed range `1`–`8`. |
| `WAVELENGTH_METRICS_ADDR` | Separate Prometheus listener, such as `127.0.0.1:9090`; unset disables the listener. Keep it private. |

## Build and develop

Go 1.25+ is required by the MCP SDK. Original-file playback works without external media tools;
install `ffmpeg` for transcoding and HLS. Acquisition also needs `ffprobe`,
`yt-dlp[default]==2026.10.21`, and a supported Deno runtime. The image includes these.

```sh
export WAVELENGTH_MUSIC_DIR=/absolute/path/to/music
go run ./cmd/wavelength

```

Source builds bind the native API to loopback by default. To enable Subsonic, set
its password and optional username/address as described above; no second process
is needed.

```sh
make build          # bin/wavelength
make check          # go vet and race tests
make smoke          # real-binary integration checks; Python 3 and FFmpeg required
make compatibility  # independent Subsonic client and XML schema checks; uv required
make generate       # regenerate protobuf code; protoc required
```

Media integration tests skip without FFmpeg unless
`WAVELENGTH_REQUIRE_MEDIA_TOOLS=1` is set. Tests use isolated fixtures, not a live
library. See [architecture](docs/architecture.md) for package ownership and adding
services or transports.
