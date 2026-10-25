# Generic OIDC authentication

Wavelength speaks standard OpenID Connect, not an Authentik-specific API. Any provider
with compatible discovery, authorization-code/PKCE and signed ID tokens can be used.
Authentik, Keycloak and others are configuration choices, not code paths. No live
external provider deployment is configured by this repository.

## Provider registration

Create an OIDC application/client at your provider:

- Authorization code grant with S256 PKCE.
- Redirect URI: `https://music.example.com/auth/callback` (exact match).
- Scopes: `openid profile email`.
- Prefer a confidential web client with a client secret. Public clients are also
  supported: omit the secret when that is how your provider registered the client.
- Use the provider's **issuer URL**, not its login page or authorization endpoint.
  Wavelength discovers authorization, token and JWKS endpoints automatically.

All users whom the provider allows to sign into this application receive access to
the shared library and their own personal collections. Restrict application access
at the provider (for example with an Authentik music-users group binding). Roles,
group mappings and per-user catalog permissions are not implemented. Email/name
claims are display metadata, not authorization. Private playlists, queues, bookmarks
and history are keyed by the verified issuer/subject identity.

## Server configuration

```sh
export WAVELENGTH_MUSIC_DIR=/absolute/path/to/music
export WAVELENGTH_DATA_DIR=./data
export WAVELENGTH_ADDR=0.0.0.0:8080
export WAVELENGTH_PUBLIC_URL=https://music.example.com
export WAVELENGTH_OIDC_ISSUER=https://identity.example.com/your-issuer
export WAVELENGTH_OIDC_CLIENT_ID=wavelength
export WAVELENGTH_OIDC_CLIENT_SECRET='your-client-secret'
go run ./cmd/wavelength
```

Use your provider's exact issuer, including any required trailing slash. Keep the
client secret out of Git. These are environment variables; Wavelength does not load
`.env` files automatically.

The public URL is the externally visible **origin**, with no path prefix, query or
fragment. Place TLS at a reverse proxy and forward `/auth/`, `/oauth/`,
`/.well-known/oauth-authorization-server`, `/.well-known/oauth-protected-resource`,
`/.well-known/oauth-protected-resource/mcp`, `/mcp`, `/wavelength.v1.LibraryService/`,
`/wavelength.v1.UserDataService/` and `/media/` to the native listener. Forwarding the
whole hostname also works. Neither forwarded identity headers nor an external auth
proxy are required.
The configured public URL—not incoming Host/X-Forwarded headers—determines callback
URLs, secure cookies and allowed browser mutation origins.

HTTPS is required for public URLs and provider/discovered endpoints, except HTTP
loopback URLs (`localhost`, `127.0.0.1`, `::1`) for development. For local testing,
register `http://127.0.0.1:8080/auth/callback` and set the public URL accordingly.
Don't mix localhost and 127.0.0.1 during a flow; cookies bind to the hostname.
Partial configuration or failed discovery prevents startup; it never silently enables
anonymous access. The issuer, client ID and public URL are required together. A
client secret is optional only for a provider-configured public client.

`WAVELENGTH_TOKEN` is optional when OIDC is enabled. If set, it remains an independent
static bearer credential for development/automation. It is **not** an OIDC access
token and cannot access personal-state RPCs or MCP. No token is needed for a signed-in browser. With neither OIDC nor a token,
only loopback binding is allowed and the native API is locally anonymous.

## Browser endpoints

- `GET /auth/login`: begins authorization-code login with state, nonce and S256 PKCE.
  The web app uses `?return_to=/` to return to its landing page after sign-in; this
  fixed destination is the only accepted override.
- `GET /auth/callback`: exchanges the one-use authorization code, verifies the signed
  ID token's issuer/audience/expiry/nonce, and creates a local session. Redirects to
  `/auth/me` by default, or `/` when login requested the web landing page.
- `GET /auth/me`: returns `user`, `csrf_token`, and `expires_at`; returns 401 when not
  signed in. The user is keyed by `(issuer, subject)`, not email.
- `POST /auth/logout`: requires the session, correct Origin, and `X-CSRF-Token`.
  Revokes the local session in SQLite and clears its cookie. It does not sign the
  browser out of the identity provider or revoke other devices' sessions.

All native Connect RPC methods and `/media/` streams accept the session cookie.
The Svelte client uses generated Connect clients with the same cookie and CSRF
header. A same-origin browser can also use ordinary fetch calls and audio elements:

```js
const response = await fetch('/auth/me');
if (response.status === 401) location.assign('/auth/login');
const { csrf_token } = await response.json();

await fetch('/wavelength.v1.LibraryService/ListTracks', {
  method: 'POST',
  headers: { 'X-CSRF-Token': csrf_token, 'Content-Type': 'application/json' },
  body: '{}',
});
// The browser supplies Origin automatically; cookies accompany same-origin requests.
```

```html
<audio controls src="/media/tracks/ACTUAL_TRACK_ID"></audio>
```

All cookie-authenticated POST requests, including read-only Connect RPC calls, require
both the exact public Origin and CSRF header. GET/HEAD media playback does not.
Bearer-authenticated API calls do not require CSRF, because the bearer credential is
not automatically attached by a browser. An invalid explicit Authorization header
is rejected, rather than falling back to a cookie. `/auth/me` and `/auth/logout` are
browser-session endpoints, not bearer-token management endpoints.

## Delegated OAuth for MCP and native clients

Wavelength provides a small OAuth authorization server in front of the same OIDC
login. The native API and MCP are one application/resource with the same stable user
identity and permissions. A new AI client registers **at Wavelength**, not at
Authentik. Upstream registration and the exact `/auth/callback` URI stay unchanged;
the AI client's callback is validated locally. Existing Authentik application/group
bindings apply to MCP sign-ins too. No provider tokens are passed to AI clients.

Connect the client to `https://music.example.com/mcp`. An unauthenticated request
returns 401 with a `WWW-Authenticate` resource-metadata link. Discovery follows
[MCP authorization](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization):

| Endpoint | Purpose |
| --- | --- |
| `GET /.well-known/oauth-protected-resource/mcp` | Resource URL, authorization server, scopes; root metadata alias is also available. |
| `GET /.well-known/oauth-authorization-server` | OAuth endpoint and capability discovery. |
| `POST /oauth/register` | Dynamic registration with exact HTTP loopback or HTTPS redirect URIs. |
| `GET /oauth/authorize` | Authorization code, S256 PKCE, state and resource binding; redirects to the existing OIDC login and then local consent. |
| `POST /oauth/authorize` | Browser-bound, same-origin, CSRF-protected allow/deny decision. |
| `POST /oauth/token` | Exchange a one-use code or rotate a refresh token. |
| `POST /oauth/revoke` | Revoke a client's token family immediately. |

Public clients use `token_endpoint_auth_method: none`; confidential clients can use
`client_secret_basic` or `client_secret_post`. PKCE is required for all clients.
Client registration defaults to `client_secret_basic` when the method is omitted.
Client ID metadata documents, device grants, and upstream access-token passthrough
are not supported. Registered clients expire after one year. Anonymous registration
does not grant library access: each connection requires an allowed OIDC user and
explicit consent displaying the unverified client name, callback, and permissions.

Use an implicit/trusted-app authorization flow for Wavelength's fixed upstream
OIDC registration if your provider would otherwise show a second app-consent
prompt. Keep application/group access policies enabled. Wavelength still asks
once for each connecting AI client, showing the account, permissions and return
address on its own approval screen.

The approval document uses `same-origin` referrer policy, including a matching HTML
meta policy when a proxy overrides response headers. A plain form POST under
`no-referrer` sends `Origin: null` in browsers, which correctly fails the strict
CSRF check. CSP permits the registered callback origin as well as Wavelength,
because browsers also apply `form-action` to the final OAuth redirect. Inline
styles require a per-response nonce; scripts, framing and external assets remain
disabled. Missing, null or foreign origins and invalid CSRF tokens are rejected.

The protected resource identifier is the exact public `/mcp` URL. Request it using
the OAuth `resource` parameter, including when using the resulting credential for
native RPCs. A missing resource defaults to this one resource for older clients;
another resource is rejected. No credential from another issuer/resource or an
upstream ID/access token is accepted. Identity-provider policies are revisited for
each new grant; starting a flow with an old browser session cannot skip that check.

| Scope | Permission |
| --- | --- |
| `wavelength:read` | Required: shared catalog and your own collections/activity. |
| `wavelength:write` | Create/change your collections and listening activity. |
| `wavelength:stream` | HTTP audio bytes and native `StreamAudio`. |
| `wavelength:ingest` | Acquire music into the shared catalog; cancel/retry your own imports. |

Default consent and the MCP authentication challenge request read/write/ingest.
Ingestion is listed explicitly on the consent screen; clients can request read-only
or other narrower grants. Existing grants do not gain this permission on refresh;
reconnect and authorize it to acquire music. Streaming must be requested separately. MCP offers playback URLs and never embeds a token in
them: a signed-in browser can open them, or a native client supplies a stream-scoped
bearer token. MCP has no audio-byte tool. Cookie/development credentials cannot
authenticate MCP. Requests carrying a browser Origin must match the public origin.

Access tokens last up to one hour. Refresh token families expire 30 days after
authorization and rotate on each refresh; rotation also invalidates the previous
access token. Reuse of a known rotated refresh token revokes the whole family.
Refresh cannot increase scopes. Tokens, client secrets and codes are stored only as
SHA-256 hashes in SQLite; downstream tokens never contain provider credentials.
Native/browser personal scopes use `oidc:<identity-id>` and preserve the existing
legacy scopes. Code consumption and refresh rotation are transactional.

Signed-in browsers can list their grants with `GET /auth/connections` and revoke one
using `POST /auth/connections/{id}/revoke` with Origin and X-CSRF-Token. The listing
contains client names/scopes/expiry and no credential hashes. Other users cannot
list or revoke your grants. Browser logout ends that browser session; it does not
disconnect separately authorized AI clients. Provider-side account/group changes do
not revoke existing local grants automatically; revoke the connection to end it
immediately, otherwise the fixed grant expiry requires a fresh provider login.

OAuth request bodies are limited to 64 KiB; MCP/native collection messages to 1 MiB.
OAuth public endpoints have a process-wide rate budget of 5 requests/second, burst
20, without trusting forwarded client-IP headers. OAuth client/request/code/grant
stores prune expired records and reject creation at 4,096 live records per scope. This
deployment uses one SQLite-backed process; the protocol handler itself keeps no MCP
session affinity or per-user servers.

## Persistence and boundaries

SQLite stores issuer/subject identities, pending ten-minute login transactions and
fixed twelve-hour sessions. Raw session/login cookie credentials are never stored:
only their SHA-256 hashes. Transient login nonce/PKCE verifier and session CSRF tokens
are persisted to validate their respective flows. Provider access/refresh/ID tokens
are not retained after login. Expired flows/sessions are pruned on subsequent logins.

Sessions survive restarts and expire without requiring the provider on every request.
Logout revokes them immediately; provider-side account changes do not automatically
revoke existing local sessions. Sessions from a different configured issuer are
rejected. Switching issuers creates different identities; automatic email-based
account linking is deliberately absent.

Production cookies are host-only `__Host-` cookies with Secure, HttpOnly, Path=/ and
SameSite=Lax. Loopback HTTP development uses non-Secure cookie names. API/auth replies
are not cached. No browser tokens need to be placed in localStorage or stream URLs.

Legacy Subsonic on port 8081 still authenticates exclusively through its own configured
username/password or salted protocol token. It does not accept OIDC sessions or the
native bearer token. It accepts only the configured legacy username and password;
legacy account-management calls cannot change those credentials. Native users'
delegated OAuth credentials remain separate.

## Verification

`go test -race ./...` includes a real local HTTP OIDC fixture exposing discovery,
authorization, token and JWKS endpoints with RSA-signed ID tokens. It checks S256
PKCE, state/nonce, issuer/audience/expiry/signature rejection, cookie-authenticated
streaming, CSRF, logout/replay, SQLite session persistence, HTTPS cookie attributes,
and isolation after changing issuers. No external identity-provider credentials are
needed for the tests. Delegated OAuth tests additionally cover discovery, dynamic
registration, all three client-auth methods, real upstream login and consent,
resource/redirect/client/PKCE binding, expiry, concurrent code redemption, refresh
rotation/replay, revocation, database reopen, and issuer/resource isolation. The
official MCP Go client also completes automatic metadata discovery, client
registration, login/consent, and a renewable token exchange before creating a
playlist. It exercises every tool and shared data across Connect,
Connect JSON, gRPC-Web and gRPC, with cross-user and read-only mutation denials.
Raw HTTP checks also cover the three 2025 MCP protocol versions. Tests use only
temporary local credentials and databases; third-party AI client certification is
not implied.

CI also runs Chromium through the actual OIDC redirects, rendered approval form,
browser-generated Origin header, callback and token exchange. It covers normal
approval, a proxy forcing `no-referrer`, cancellation, and a negative control
reproducing the original Origin-null denial. To run it locally, install Playwright
and Chromium, then set `WAVELENGTH_BROWSER_PYTHON` to that Python executable and run
`go test ./internal/auth -run TestOAuthBrowserConsent`. An optional
`WAVELENGTH_BROWSER_BIN` selects an existing Chromium executable.
