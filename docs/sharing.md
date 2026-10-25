# Playback and sharing links

Private media stays at `/media/tracks/{id}` and requires the existing browser
session or API bearer token. `GetPlaybackInfo` and `get_playback_info` retain that
behavior. A playback URL is a separate, deliberately created credential.

| Link | Purpose | Lifetime |
| --- | --- | --- |
| `/s/<token>` | Public webpage with the selected songs, artwork and a player | 30 days by default; explicit expiry or no expiry |
| `/play/<token>/audio.<ext>` | Original audio bytes for Sonos or another HTTP player | Three hours by default; 60 seconds to 24 hours |

The extension reflects the original file; the endpoint does not transcode. URLs
use `WAVELENGTH_PUBLIC_URL`, never a request Host or forwarded header. The address
must be reachable from the speaker or recipient. Share pages need the built web
assets (`WAVELENGTH_WEB_DIR`); direct audio links also work without web assets.

## Native RPC and MCP

`SharingService` is available over Connect, gRPC and gRPC-Web. MCP tools call the
same service and persist the same grants.

| RPC | MCP | Permission |
| --- | --- | --- |
| `CreatePlaybackLink` | `create_playback_link` | Named identity and `wavelength:stream` |
| `CreateShare` | `create_share` | Named identity, `wavelength:write` and `wavelength:stream` |
| `ListAccessLinks` | `list_access_links` | Named identity; only your records |
| `RevokeAccessLink` | `revoke_access_link` | Named identity and `wavelength:write`; only your records |

All delegated credentials also require `wavelength:read`. Browser sessions have
the signed-in user's full permissions, with the usual CSRF check for Connect
POSTs. Static development tokens and legacy Subsonic credentials cannot create or
manage links. Existing OAuth grants must be authorized again to gain stream scope.

For an external player:

1. Search the local library and obtain the track ID.
2. Call `CreatePlaybackLink({track_id, expires_in_seconds: 10800})` or
   `create_playback_link({track_id, expires_in_seconds: 10800})`.
3. Give the returned absolute `url` to the player. No Authorization header or
   session cookie is needed when fetching it.

Creation returns `{link, url}`. The record includes `id`, `kind`, `name`,
`track_ids`, `created_at`, and optional `expires_at`. Revoked records are deleted. List requests
accept `kind` (`share` by default or `playback`), `limit` (default/max 100), and
`offset`. Only creation returns the bearer URL; keep it when created. Revoke by
record `id`. Recipient playback grants are managed through their parent share and
do not clutter the owner’s direct playback-link list. A playback link does not
create a share webpage.

For sharing, supply a name, ordered local `track_ids` (1–1000), and an optional
future `expires_at` (protobuf Timestamp for RPC, RFC3339 for MCP). Omission selects
30 days; set `no_expiry: true` explicitly for an unlimited share. Supplying both
expiry and no-expiry is rejected. Album and
playlist buttons supply the current song list. This is a snapshot: later playlist
edits do not add or remove songs from the share. Replacing a song's audio keeps its
track ID, so an existing link serves the current replacement.

## Public player

The public page loads only the share’s metadata, artwork, and saved lyrics. Its
embedded lyrics panel supports timed highlighting, following, and click-to-seek.
Selecting a song
creates a temporary playback link scoped to that song and parent share. The player
requests the next song's link when it starts, rather than issuing short-lived links
for an entire queue in advance. Public metadata and audio do not consume a browser
session or grant access to authenticated RPCs, private media paths, or other songs.

Each audio request rechecks its playback grant and any parent share. Parent expiry
caps the child's expiry; parent revocation also blocks already-issued child links.
Audio uses the same confined file reader and HTTP streamer as private playback:
GET/HEAD, `Accept-Ranges: bytes`, `206`, `Content-Range`, `Content-Length`, the
stored audio content type, conditional reads and `416` for invalid ranges. Links
are reusable until expiry so buffering, retries and seeks work. For long queues or
pauses, a controller must obtain fresh links before starting expired items.

SQLite stores SHA-256 hashes of 256-bit random bearer tokens, separate management
IDs, the owning identity, library root, exact track IDs, timestamps and parent
share IDs. Revocation permanently deletes the grant and cascades to its child
playback grants. Expired grants are deleted when links are created or listed.
The migration also removes previously revoked/expired records; no soft-delete
history is retained. Public responses use private/no-store caching and no-referrer headers. Treat URLs as
credentials and redact them from proxy/access logs. Revocation blocks subsequent
requests; audio already delivered or being transferred cannot be recalled.

This provides HTTP playback URLs for Sonos controllers. Speaker discovery, Sonos
queue control and casting UI are separate integrations.
