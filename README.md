# Wavelength

A self-hosted Go music server: one binary, embedded SQLite, a native protobuf/Connect
RPC API and a separate legacy Subsonic listener. No external database server.

## Run

Go 1.24+ is required to build. Original-file playback works without FFmpeg; install
`ffmpeg` and `ffprobe` for transcoding, HLS and richer audio metadata.

```sh
export WAVELENGTH_MUSIC_DIR=/absolute/path/to/music
export WAVELENGTH_DATA_DIR=./data
go run ./cmd/wavelength
```

The source library is read-only. Startup scans finish before serving; without a music
root the server exposes health only. SQLite, sessions and app data live in the data directory. Back up that directory.

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

Legacy music features include browsing/search, playlists, bookmarks/queues, local
listening history, cover art/lyrics, scanning, downloads, audio transcoding and audio
HLS. XML, JSON and JSONP are supported with `.view` aliases and salted-token/password
authentication. Only the configured username/password can authenticate; account
management commands are no-ops and cannot change those credentials.

Podcasts, video/captions, radio, chat, avatars, sharing, favorites and ratings retain
empty compatibility responses in the legacy handler. They have no backing services.
The old public sharing routes are unavailable. See [compatibility and
verification](docs/subsonic-compatibility.md) for the exact response behavior.

## Indexing

Recursive audio indexing extracts available tags and optional ffprobe metadata.
Audio-only MP4 files require ffprobe classification; files containing video are skipped.
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
See [architecture](docs/architecture.md) for package ownership, service lifetimes and
how to add another capability or transport. The native API stays limited to library
browsing, scanning and playback; legacy feature handlers delegate to shared services.
