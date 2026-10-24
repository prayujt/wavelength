# Legacy Subsonic compatibility

Target: the documented Subsonic REST API 1.16.1, not all OpenSubsonic extensions or
all behavior of the original Java server. All documented endpoint names have handler
paths, but endpoint presence alone does not establish full compatibility.

## Implemented groups

- System: ping, license, music folders, scan status and scanning.
- Browsing: indexes/directories, artists/albums/songs, genres, video and video details.
- Discovery: search/search2/search3, album lists, random/genre songs, stars, related
  artists/songs and artist/album info. Local fallback when no external provider exists.
- User state: stars/ratings, scrobble/now-playing, playlists and access control,
  bookmarks, play queues, internet radio and chat.
- Accounts: users, role updates, password changes and deletion; persisted app accounts
  are separate from OIDC. Bootstrap credentials remain configured by environment.
- Media: original/download byte ranges, FFmpeg audio/video conversion, artwork,
  sidecar lyrics, avatars, HLS segments and sidecar/embedded captions.
- Sharing: durable expiring/revocable public links scoped to selected media.
- Podcasts: RSS subscription/refresh, episode download, playback and deletion.
- Jukebox: queue/state/control and an opt-in ffplay output backend.

## Configuration-dependent behavior

- FFmpeg/ffprobe are required for transcoding, HLS and video stream introspection.
- Shares require a public legacy origin and the account's sharing role.
- Podcasts require the podcast role; downloads use Wavelength app storage.
- Jukebox requires an enabled backend, jukebox role and usable host audio output.
- Enriched biography/album info and external scrobbling require Last.fm configuration.
  Without it, related-item recommendations use local catalog/history, not Last.fm parity.
- Avatars and lyrics require source assets. Missing resources return protocol errors.

## Remaining differences and unverified areas

- Real-binary interoperability is verified with py-sonic 1.0.3 for 74 endpoints; its
  two video methods have upstream integer-ID/JSON-caption assumptions and are checked
  directly over HTTP instead. This is not certification of every mobile/desktop app
  or a live Navidrome migration.
- One configured library root; no LDAP authentication integration or original Subsonic
  administration/UI. Native per-user roles and a browser UI are not implemented.
- Transcoded Content-Length requests are supported using disk-backed completion to
  provide an exact length. Optional preconverted MP4 files live at
  `data/converted/<videoID>.mp4`; getVideoInfo advertises them and converted=true
  streams them with range support. Codec availability depends on the FFmpeg build.
- Podcast subscriptions support RSS and Atom enclosures. Downloads are claimed
  atomically and channel/episode deletion cannot resurrect in-flight downloads.
- Real hardware jukebox playback and live Last.fm/OIDC deployments are not verified;
  their integrations use controlled test providers/backends.
- OpenSubsonic extension parity is outside this legacy target.

Therefore this release is **not certified full parity**. The remaining compatibility
work needs explicit parameter/behavior checks and representative real-client testing.

## Verification

`go test -race ./...` exercises browsing, authentication, persisted user state,
permissions, protocol errors and native Connect. With FFmpeg/ffprobe, media tests encode
real audio/video and decode returned transcoding/HLS bytes. Shares, RSS downloads,
caption retrieval and jukebox controls have fixture integration tests.

`scripts/smoke.py` runs the actual binary with tagged audio, both ports, auth, range/HEAD
playback, pruning, restart persistence, source immutability and graceful shutdown.

`scripts/validate-subsonic.py` validates 40 exported XML response fixtures against the
pinned official 1.16.1 XSD. It documents one schema correction: PlayQueue.current is
made a string to accommodate the API's string media IDs. This is fixture/schema
coverage, not exhaustive validation of every optional resource and request parameter.

## Executable endpoint contract

The official endpoint/parameter inventory is recorded in
`internal/subsonic/testdata/contract.json`. `TestDocumentedRequiredParameters` checks
missing required inputs across that inventory; filesystem-ID and account-limit
regressions cover cross-endpoint behavior rather than dispatch presence alone.

`uv run scripts/client-compatibility.py` starts the real binary with isolated media
and SQLite, then checks all 76 documented endpoints through 172 operations in its
current fixture run. It includes actual podcast downloads, video/captions, sharing,
account administration, playlists, original and transcoded playback and HLS.
The unmodified py-sonic client handles 74 endpoints; direct HTTP covers the two video
methods described above. Its repeated-parameter GET helpers reference Python 2's
urllib2, so those client calls use POST; GET connectivity is checked separately.

Additional media tests verify encoded GET/HEAD Content-Length, per-account bitrate
limits, adaptive HLS bitrate@dimensions, caption conversion and preconverted MP4s.
Legacy filesystem IDs are distinct from ID3 identities and round-trip through search,
stars, lists, artwork, metadata and public sharing.
