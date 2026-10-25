# Wavelength web

A Svelte 5 + TypeScript client for Wavelength's Connect RPC API. It includes OIDC
sign-in, library browsing, dedicated artist/album pages, artwork, playback and a
saved queue, stored lyrics, playlists, and managed downloads. Cmd/Ctrl+K searches
the library and external music catalog together, with explicit Play or Review
download actions. Bookmarks and listening history remain available
through the API; they do not have screens yet.

## Downloading music

1. Search in Discover or with Cmd/Ctrl+K. Songs matched to existing imports show
   **In library** and can be played immediately.
2. Select songs, or open an album and select the songs you are missing. Selections
   remain available as you browse other artists and albums.
3. Open **Review download**. Keep automatic matching or choose specific recordings,
   optionally select a destination playlist, and submit the download.
4. Follow the dedicated job page in Downloads. Uncertain matches ask for a recording;
   failed items can be retried, and unfinished work can be cancelled.
5. Play completed songs directly from the job or find them in the library.

Existing-import checks use provider provenance, never fuzzy title matching. Older
catalog entries without provenance are not guessed to be the same recording.
Artwork is read from existing embedded tags or an adjacent cover on demand. The
lyrics panel reads saved lyrics through `GetLyrics`; timed LRC lines can seek the
player. Opening either does not run a scan or fetch new metadata.

The sidebar uses compact grouped navigation with light/dark themes. The theme
button follows your system preference initially and remembers explicit changes.

## Development

Use Node 22.12+ or Node 24, and configure the backend's existing OIDC environment
variables as described in [OIDC setup](../docs/oidc.md). For development, register
`http://127.0.0.1:5173/auth/callback` at your provider and start Go with:

```sh
export WAVELENGTH_PUBLIC_URL=http://127.0.0.1:5173
export WAVELENGTH_MUSIC_DIR=/absolute/path/to/music
go run ./cmd/wavelength
```

In another terminal:

```sh
cd web
npm ci
npm run dev
```

Open `http://127.0.0.1:5173`. Vite proxies auth, Connect, and media requests to
`http://127.0.0.1:8080`. Set `WAVELENGTH_BACKEND_URL` when starting Vite if the
backend listens elsewhere. Use the same hostname throughout; `localhost` and
`127.0.0.1` are different browser origins. Production also requires one public
origin for the UI and API.

The browser signs in through `/auth/login?return_to=/`. Go owns the OIDC exchange
and HttpOnly session cookie. `/auth/me` supplies the user and CSRF value; one Connect
interceptor adds `X-CSRF-Token` to all RPC requests. No provider credentials or
bearer tokens belong in frontend environment variables or browser storage.

## Build and serve

```sh
npm --prefix web ci
npm --prefix web run build
WAVELENGTH_WEB_DIR=./web/dist go run ./cmd/wavelength
```

Keep the usual music, data, and OIDC configuration in the environment. The Go
server serves the built assets alongside auth, RPCs, and media. The published
container builds these assets in a Node stage and includes them at `/web`; Node
is not needed at runtime. Navigation uses URL fragments, so static serving does
not need a route fallback.

## API contract

`src/gen/` is generated from the repository's protobuf files. `src/lib/api.ts`
constructs typed clients for LibraryService, UserDataService, DiscoveryService,
and IngestionService using the official Connect web transport. Application data
goes through those clients. Session endpoints remain under `/auth/`; audio uses
the cookie-authenticated, range-capable path from `GetPlaybackInfo`, and artwork
uses the authenticated `artwork_path` supplied in catalog responses.

After changing protobuf definitions:

```sh
npm --prefix web run generate
```

Commit the generated files. No Go transport or Subsonic types are duplicated in
the UI. Session/player state lives in `src/lib/`; screens and shared components
live in `src/views/` and `src/components/`.

## Checks

```sh
npm --prefix web run check
npm --prefix web run format:check
npm --prefix web run build
npm --prefix web exec -- playwright install --with-deps --only-shell chromium
npm --prefix web test
WAVELENGTH_WEB_TESTS=1 go test -race -timeout 90s ./internal/auth -run '^TestWebBrowser$'
```

The first browser suite checks client behavior and request serialization against
controlled responses. The Go-driven suite serves the production build and uses
real OIDC redirects, SQLite-backed sessions, CSRF checks, Connect handlers, and
media playback. External discovery/download providers are fixtures so tests do
not depend on public services. FFmpeg is required for its audio fixture.
