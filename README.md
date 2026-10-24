# Wavelength

A self-hosted Go music server: one binary, embedded SQLite, a native protobuf/Connect
RPC API and a separate legacy Subsonic listener. No external database server.

## Run

Go 1.24+ is required to build. Original-file playback works without FFmpeg; install
`ffmpeg` and `ffprobe` for transcoding, HLS and richer audio/video metadata.

```sh
export WAVELENGTH_MUSIC_DIR=/absolute/path/to/music
export WAVELENGTH_DATA_DIR=./data
go run ./cmd/wavelength
```

The source library is read-only. Startup scans finish before serving; without a music
root the server exposes health only. SQLite, sessions, app data and downloaded
podcasts live in the data directory. Back up that directory, including `subsonic.key`.

## Native API: Connect RPC, port 8080

The native contract is [library.proto](proto/wavelength/v1/library.proto), with generated
Go messages and Connect clients under `gen/`. Library browsing, search, scanning and
playback discovery use Connect; there is no grpc-go server or native REST control API.

```sh
curl http://127.0.0.1:8080/healthz
curl -X POST http://127.0.0.1:8080/wavelength.v1.LibraryService/ListTracks \
  -H 'Content-Type: application/json' -d '{"limit":20}'
curl -H 'Range: bytes=0-1023' http://127.0.0.1:8080/media/tracks/TRACK_ID
```

`GetPlaybackInfo` returns a media URL for browser audio elements. GET/HEAD media
supports byte ranges and seeking; `StreamAudio` also provides server-streamed binary
chunks for native Connect clients. No credentials are embedded in media URLs.

- `WAVELENGTH_ADDR`: default `127.0.0.1:8080`.
- `WAVELENGTH_TOKEN`: optional static native bearer token; send an Authorization header.
- Non-loopback native binding requires that token or configured OIDC.
- Generic OIDC: `WAVELENGTH_PUBLIC_URL`, `WAVELENGTH_OIDC_ISSUER`,
  `WAVELENGTH_OIDC_CLIENT_ID`, optional `WAVELENGTH_OIDC_CLIENT_SECRET`.
  Register `/auth/callback` and visit `/auth/login`.

OIDC browser sessions also authenticate media playback. Cookie-authenticated Connect
POSTs require Origin and `X-CSRF-Token`, including read-only RPCs. See [OIDC](docs/oidc.md).
TLS belongs at the deployment proxy. A UI is not included yet.

## Legacy Subsonic: separate port 8081

```sh
export WAVELENGTH_SUBSONIC_USER=admin
export WAVELENGTH_SUBSONIC_PASSWORD='choose-your-own-password'
export WAVELENGTH_SUBSONIC_ADDR=127.0.0.1:8081
go run ./cmd/wavelength
```

The same binary runs both listeners. Legacy credentials are independent of native
bearer tokens and OIDC sessions. Both listeners share indexing/storage and shut down
together; neither serves if either configured port cannot bind.

Legacy features include browsing/search, playlists, stars/ratings, bookmarks/queues,
scrobbling/history, cover art/lyrics, account roles, radio/chat, scanning, downloads,
transcoding, HLS, video/captions, public shares, podcasts and optional jukebox output.
XML, JSON and JSONP are supported with `.view` aliases and salted-token/password auth.
**Broad endpoint coverage is not a claim of complete client/behavioral parity.**
See [compatibility and verification](docs/subsonic-compatibility.md) for conditions and gaps.

Optional settings:

- `WAVELENGTH_SUBSONIC_PUBLIC_URL`: public legacy origin, required to create shares.
- `WAVELENGTH_JUKEBOX_PLAYER=ffplay`: enable actual server-side jukebox output.
- `WAVELENGTH_LASTFM_API_KEY`: enable external metadata enrichment.
- `WAVELENGTH_LASTFM_SECRET` and `WAVELENGTH_LASTFM_SESSIONS`: enable signed Last.fm
  submissions; sessions is a JSON map of legacy usernames to Last.fm session keys.
- Avatar images: `data/avatars/<username>.png`, `.jpg`, `.jpeg` or `.gif` (URL-escape
  usernames containing special path characters).

Account roles gate privileged features; share/podcast permissions must be granted
with `updateUser` before use. Podcast downloads go to app data, not the source library.

## Indexing

Recursive audio/video indexing extracts available tags and optional ffprobe metadata.
Missing/bad tags fall back to filenames. Symlinks and non-regular files are skipped.
IDs are stable for root+relative path; moving/renaming changes identity. Successful
full rescans atomically update/prune the catalog; failed scans preserve the old snapshot.
One root is supported. Scans are full and synchronous, not incremental/background jobs.

```sh
go run ./cmd/wavelength --scan-only
```

## Development

```sh
make generate                 # protoc required; pinned Go generators installed locally
make check                    # vet and race tests
make smoke                    # real binary, Python 3, FFmpeg and ffprobe
make compatibility            # independent client, all legacy endpoints + XSD
uv run scripts/validate-subsonic.py  # pinned official XSD and lxml
```

Media integration tests skip without FFmpeg/ffprobe unless
`WAVELENGTH_REQUIRE_MEDIA_TOOLS=1` is set. Tests use isolated fixtures, not a live library.
See [architecture](docs/architecture.md).
