# Wavelength

A small self-hosted music server in Go, with SQLite indexing and original-file streaming.
Early development: the native API works; the separate legacy Subsonic adapter implements
only the subset listed below, not full client compatibility.

## Run

Requires Go 1.24 or newer. SQLite is embedded; no database server, CGO, or FFmpeg
is needed to run the server. Your music directory is only read, never modified.

```sh
export WAVELENGTH_MUSIC_DIR=/absolute/path/to/music
export WAVELENGTH_DATA_DIR=./data
go run ./cmd/wavelength
```

The startup scan finishes before the HTTP listener opens. No music directory means
health-only mode. An invalid/unreadable directory fails startup instead of exposing
an incomplete catalog.

Native API defaults to `127.0.0.1:8080`:

```sh
curl http://127.0.0.1:8080/healthz
curl 'http://127.0.0.1:8080/api/tracks?limit=20&offset=0&q=artist'
curl http://127.0.0.1:8080/api/tracks/TRACK_ID
curl -o track.flac http://127.0.0.1:8080/api/tracks/TRACK_ID/stream
curl -H 'Range: bytes=0-1023' http://127.0.0.1:8080/api/tracks/TRACK_ID/stream
curl -X POST http://127.0.0.1:8080/api/scan
```

Replace `TRACK_ID` with an actual `id` returned by the listing. You can open a stream
URL directly in an audio player/browser when the local native API has no token.
Streams support GET/HEAD, byte ranges (seeking), conditional modification requests,
and the original file's content type. No transcoding occurs.

### Authentication and binding

- `WAVELENGTH_ADDR`: native listener, default `127.0.0.1:8080`.
- `WAVELENGTH_TOKEN`: optional on loopback; non-loopback native listeners require
  this token **or configured OIDC**.
  When using this token, send `Authorization: Bearer <token>` for `/api/` requests,
  including streams. With OIDC, signed-in browser sessions also authenticate these routes.
  Tokens are not accepted in URLs. Health endpoints remain unauthenticated.
- `WAVELENGTH_MUSIC_DIR`: one read-only music root.
- `WAVELENGTH_DATA_DIR`: SQLite storage, default `./data`.

For example, use `WAVELENGTH_ADDR=0.0.0.0:8080` with `WAVELENGTH_TOKEN` set to
bind the native API outside loopback. Put TLS at your reverse proxy if exposing it.

### Generic OIDC login for the native API / future UI

Use a standard OIDC provider with these settings:

```sh
export WAVELENGTH_PUBLIC_URL=https://music.example.com
export WAVELENGTH_OIDC_ISSUER=https://identity.example.com/your-issuer
export WAVELENGTH_OIDC_CLIENT_ID=wavelength
export WAVELENGTH_OIDC_CLIENT_SECRET='your-client-secret'
```

Register `$WAVELENGTH_PUBLIC_URL/auth/callback` at the provider and visit
`/auth/login`. Wavelength uses discovery + authorization code/PKCE, verifies ID tokens,
and issues SQLite-backed HttpOnly browser sessions. Signed-in sessions can browse and
stream; cookie-authenticated writes require CSRF protection. The native bearer token
remains an independent optional development/automation credential.

See [OIDC setup and browser integration](docs/oidc.md) for provider registration,
configuration, endpoints, security boundaries and current limitations. No external
provider is required unless you enable OIDC. Subsonic auth is unaffected.

### Separate legacy Subsonic listener

Enable it by setting a password before starting the same binary:

```sh
export WAVELENGTH_SUBSONIC_USER=admin
export WAVELENGTH_SUBSONIC_PASSWORD='choose-your-own-password'
# Defaults to 127.0.0.1:8081; override to bind elsewhere:
export WAVELENGTH_SUBSONIC_ADDR=127.0.0.1:8081
go run ./cmd/wavelength
```

The native listener **never** registers `/rest` routes. The legacy listener **never**
registers native `/api` routes. Both share the same catalog and read-only streaming
service and shut down together; neither starts if either port fails to bind.
The legacy listener is disabled unless its password is configured. Its credentials
are separate from the native bearer token.

Implemented `/rest/<endpoint>` and `/rest/<endpoint>.view`:

- `ping`, `getLicense`, `getMusicFolders`
- `getArtists`, `getArtist`, `getAlbum`, `getSong`
- `search3` (**song results only**, `songCount`/`songOffset` supported)
- `stream` (**original bytes only**, transcoding parameters are not implemented)

XML is the default; `f=json` returns the Subsonic JSON envelope. GET and form POST
are accepted. Use standard `u`, `v`, `c` and either `t`/`s` token authentication
or `p` (plain or `enc:` hex). For a local connectivity check:

```sh
curl --get http://127.0.0.1:8081/rest/ping.view \
  --data-urlencode "u=$WAVELENGTH_SUBSONIC_USER" \
  --data-urlencode "p=$WAVELENGTH_SUBSONIC_PASSWORD" \
  --data-urlencode 'v=1.16.1' --data-urlencode 'c=curl' --data-urlencode 'f=json'
```

No playlists, cover art, scrobbling, user management, folder-style browsing,
OpenSubsonic extensions, or audio duration extraction yet. Full playback in a
particular Subsonic app is **not** verified. Unsupported endpoints return protocol
errors rather than pretending to succeed. Album grouping currently uses album
artist (or artist) plus album name; untagged music uses unknown artist/album groups.

## Indexing behavior

Recursively indexes `.mp3`, `.flac`, `.m4a`, `.mp4`, `.ogg`, `.opus`, `.wav`, `.aac`,
`.aiff`, and `.aif` candidates. Embedded tags are extracted where the parser supports
them; missing/unsupported/corrupt tags fall back to the filename title and increment
`metadata_warnings`. File extensions are not a guarantee that a file is decodable.
Symlinks, non-regular files, and unrelated extensions are skipped.

IDs remain stable for a canonical root path + relative file path across restarts
and rescans. Renaming/moving a track changes its ID. Successful rescans update
metadata and prune missing files; a failed/cancelled scan leaves the old snapshot
untouched. A concurrent scan returns 409. Scans are synchronous, read all candidate
metadata, and hold the scan snapshot in memory before a single database transaction;
incremental indexing and background jobs are future work.

A scan without running HTTP listeners:

```sh
go run ./cmd/wavelength --scan-only
```

## Development

```sh
make check
make build
# Optional real-binary smoke test: Python 3 + FFmpeg required only for this test.
make smoke
```

The smoke test generates short tagged FLAC/MP3 fixtures in a temporary directory,
starts both listeners on ephemeral loopback ports, verifies streaming and FFmpeg
decoding, range/HEAD requests, rescan pruning, auth, port isolation, read-only source
files, restart persistence, and graceful shutdown. It never touches your library.

See [architecture](docs/architecture.md) for package boundaries and next milestones.
