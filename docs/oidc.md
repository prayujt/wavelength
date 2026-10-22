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

All users whom the provider allows to sign into this application currently receive
the same native library access, including rescanning. Restrict application access at
the provider if needed. Local roles, group mappings and per-user library permissions
are not implemented yet. Email/name claims are display metadata, not authorization.

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
fragment. Place TLS at a reverse proxy and forward `/auth/` and `/api/` to the native
listener. Neither forwarded identity headers nor an external auth proxy are required.
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
token. No token is needed for a signed-in browser. With neither OIDC nor a token,
only loopback binding is allowed and the native API is locally anonymous.

## Browser endpoints

- `GET /auth/login`: begins authorization-code login with state, nonce and S256 PKCE.
- `GET /auth/callback`: exchanges the one-use authorization code, verifies the signed
  ID token's issuer/audience/expiry/nonce, and creates a local session. Redirects to
  `/auth/me` for now; there is no UI homepage yet.
- `GET /auth/me`: returns `user`, `csrf_token`, and `expires_at`; returns 401 when not
  signed in. The user is keyed by `(issuer, subject)`, not email.
- `POST /auth/logout`: requires the session, correct Origin, and `X-CSRF-Token`.
  Revokes the local session in SQLite and clears its cookie. It does not sign the
  browser out of the identity provider or revoke other devices' sessions.

All `/api/` endpoints, including original-file streams, accept the session cookie.
A same-origin future UI can use ordinary fetch calls and audio elements:

```js
const response = await fetch('/auth/me');
if (response.status === 401) location.assign('/auth/login');
const { csrf_token } = await response.json();

await fetch('/api/scan', {
  method: 'POST',
  headers: { 'X-CSRF-Token': csrf_token },
});
// The browser supplies Origin automatically; cookies accompany same-origin requests.
```

```html
<audio controls src="/api/tracks/ACTUAL_TRACK_ID/stream"></audio>
```

Cookie-authenticated mutations require both the exact public Origin and CSRF header.
Bearer-authenticated API calls do not require CSRF, because the bearer credential is
not automatically attached by a browser. An invalid explicit Authorization header
is rejected, rather than falling back to a cookie. `/auth/me` and `/auth/logout` are
browser-session endpoints, not bearer-token management endpoints.

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
native bearer token. Per-user Subsonic app credentials and scoped/revocable native API
tokens are future work, not features claimed by this initial OIDC integration.

## Verification

`go test -race ./...` includes a real local HTTP OIDC fixture exposing discovery,
authorization, token and JWKS endpoints with RSA-signed ID tokens. It checks S256
PKCE, state/nonce, issuer/audience/expiry/signature rejection, cookie-authenticated
streaming, CSRF, logout/replay, SQLite session persistence, HTTPS cookie attributes,
and isolation after changing issuers. No external identity-provider credentials are
needed for the tests.
