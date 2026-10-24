# Wavelength

A self-hosted music server written in Go, with an embedded SQLite catalog,
Subsonic client compatibility, and a small native protobuf API. Point it at your
music collection to index metadata, browse your library, and stream audio. It runs
as one binary and does not need an external database. A web UI is not included.

## Run with Docker

The image includes FFmpeg and ffprobe and supports Linux on amd64 and arm64.

```sh
export WAVELENGTH_TOKEN='choose-a-long-random-token'

docker run --rm --name wavelength \
  -p 127.0.0.1:8080:8080 \
  -e WAVELENGTH_TOKEN \
  -e WAVELENGTH_MUSIC_DIR=/music \
  --mount type=bind,src=/absolute/path/to/music,dst=/music,readonly \
  --mount type=volume,src=wavelength-data,dst=/data \
  docker.prayujt.com/wavelength:latest
```

Replace the source path with your music directory. Keep the data volume between
runs: it holds the catalog and application state. The music mount can stay
read-only; Wavelength does not modify the source files.

Pushes to `main` publish `docker.prayujt.com/wavelength:latest` and a tag containing
the full commit SHA. Images are built with [ko](https://ko.build/) on an Alpine
runtime base. See the [build workflow](.github/workflows/build.yml).

## Subsonic clients

Wavelength exposes a separate Subsonic-compatible listener for existing music
clients. It supports library browsing and search, playlists, bookmarks and playback
queues, cover art, lyrics, downloads, audio transcoding, and audio HLS. It uses the
same indexed library and storage as the native API.

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

Wavelength recursively scans one music directory, reads embedded tags, and stores
the resulting catalog in `wavelength.db` inside the data directory. FFprobe adds
duration, bitrate, and metadata where available. Common formats include MP3, FLAC,
M4A, Ogg, Opus, WAV, and AIFF; audio-only MP4 files are classified with ffprobe.

Artist and album grouping comes from metadata, so an artist/album folder layout is
not required. Missing titles fall back to filenames. Symlinks, non-regular files,
and files classified as video are skipped.

Startup completes a full scan before serving. Later scans can be requested through
the APIs; there is no filesystem watcher. A successful scan atomically updates the
catalog and removes entries for missing files. A failed scan keeps the previous
catalog. Track IDs depend on the library root and relative file path, so moving or
renaming a file changes its identity on the next scan.

SQLite also holds playlists, bookmarks, playback queues, and listening history.
Back up the data directory as well as your music collection.

## Native API

The native API is intentionally small: browse and search tracks, artists, and
albums; start a scan and inspect its status; discover playback URLs; and stream
audio. The contract is [library.proto](proto/wavelength/v1/library.proto), with
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

For browser sign-in, configure a generic OIDC provider, register `/auth/callback`,
and visit `/auth/login`. Sessions authenticate API and media requests;
cookie-authenticated Connect POSTs also require `Origin` and `X-CSRF-Token`.
See [OIDC setup](docs/oidc.md). Terminate TLS at your deployment proxy.

## Configuration

Configuration is read from environment variables at startup.

| Variable | Purpose / default |
| --- | --- |
| `WAVELENGTH_MUSIC_DIR` | Music directory; unset starts without a library. |
| `WAVELENGTH_DATA_DIR` | Persistent data directory; `./data`, or `/data` in the image. |
| `WAVELENGTH_ADDR` | Native listener; `127.0.0.1:8080`, or `0.0.0.0:8080` in the image. |
| `WAVELENGTH_TOKEN` | Native API bearer token; required for non-loopback binding unless OIDC is configured. |
| `WAVELENGTH_SUBSONIC_PASSWORD` | Enables the Subsonic listener; requires a music directory. |
| `WAVELENGTH_SUBSONIC_USER` | Subsonic username; `admin`. |
| `WAVELENGTH_SUBSONIC_ADDR` | Subsonic listener; `127.0.0.1:8081`. Use `0.0.0.0:8081` in a container. |
| `WAVELENGTH_PUBLIC_URL` | Public origin for OIDC, such as `https://music.example.com`. |
| `WAVELENGTH_OIDC_ISSUER` | OIDC provider issuer URL. |
| `WAVELENGTH_OIDC_CLIENT_ID` | OIDC client ID. |
| `WAVELENGTH_OIDC_CLIENT_SECRET` | OIDC client secret, if required by your provider. |

## Build and develop

Go 1.24+ is required. Original-file playback works without external media tools;
install `ffmpeg` and `ffprobe` for transcoding, HLS, and richer metadata.

```sh
export WAVELENGTH_MUSIC_DIR=/absolute/path/to/music
go run ./cmd/wavelength

# Scan the library and exit without starting listeners.
go run ./cmd/wavelength --scan-only
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

Media integration tests skip without FFmpeg/ffprobe unless
`WAVELENGTH_REQUIRE_MEDIA_TOOLS=1` is set. Tests use isolated fixtures, not a live
library. See [architecture](docs/architecture.md) for package ownership and adding
services or transports.
