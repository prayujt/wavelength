# Wavelength web

A Svelte 5 + TypeScript client for Wavelength's Connect RPC API. The first version
includes OIDC sign-in, local song/album/artist browsing, playback and a saved queue,
playlist management, external discovery, recording selection, and import progress
with cancellation and retries. Bookmarks and listening history remain available
through the API; they do not have screens yet.

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
goes through those clients. Session endpoints remain under `/auth/`, and audio
uses the cookie-authenticated, range-capable path from `GetPlaybackInfo`.

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
