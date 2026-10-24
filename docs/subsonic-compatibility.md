# Legacy Subsonic compatibility

Target: the documented Subsonic REST API 1.16.1, not all OpenSubsonic extensions or
all behavior of the original Java server. 75 documented endpoints are supported.
Jukebox requests return an unsupported operation error. Endpoint coverage alone does
not establish full compatibility.

## Implemented groups

- System: ping, license, music folders, scan status and scanning.
- Browsing: indexes/directories, artists/albums/songs, genres, video and video details.
- Discovery: search/search2/search3, album lists, random/genre songs, stars, related
  artists/songs and artist/album info, using the local catalog and listening history.
- User state: stars/ratings, scrobble/now-playing, playlists and access control,
  bookmarks, play queues, internet radio and chat.
- Accounts: users, role updates, password changes and deletion; persisted app accounts
  are separate from OIDC. Bootstrap credentials remain configured by environment.
- Media: original/download byte ranges, FFmpeg audio/video conversion, artwork,
  embedded/sidecar lyrics, avatars, HLS segments and sidecar/embedded captions.
- Sharing: durable expiring/revocable public links scoped to selected media.
- Podcasts: RSS subscription/refresh, episode download, playback and deletion.

## Configuration-dependent behavior

- FFmpeg/ffprobe are required for transcoding, HLS and video stream introspection.
- Shares require a public legacy origin and the account's sharing role.
- Podcasts require the podcast role; downloads use Wavelength app storage.
- Related items and top songs use local catalog relationships and listening history.
  Remote biographies, album notes and external scrobbling are not provided.
- Artwork and avatars require source assets; missing resources return protocol errors.
  Lyrics require embedded tags or sidecar files; missing lyrics return an empty result.

## Remaining differences and unverified areas

- Real-binary interoperability is verified with py-sonic 1.0.3 for 73 endpoints; its
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
- Live OIDC deployments are not verified; authentication tests use controlled issuers.
- OpenSubsonic extension parity is outside this legacy target.

Therefore this release is **not certified full parity**. The remaining compatibility
work needs explicit parameter/behavior checks and representative real-client testing.

## Verification

`go test -race ./...` exercises browsing, authentication, persisted user state,
permissions, protocol errors and native Connect. With FFmpeg/ffprobe, media tests encode
real audio/video and decode returned transcoding/HLS bytes. Shares, RSS downloads and
caption retrieval have fixture integration tests.

`scripts/smoke.py` runs the actual binary with tagged audio, both ports, auth, range/HEAD
playback, pruning, restart persistence, source immutability and graceful shutdown.

`scripts/validate-subsonic.py` validates 40 exported XML response fixtures against the
pinned official 1.16.1 XSD. It documents one schema correction: PlayQueue.current is
made a string to accommodate the API's string media IDs. This is fixture/schema
coverage, not exhaustive validation of every optional resource and request parameter.

## Executable endpoint contract

The official endpoint/parameter inventory is recorded in
`internal/transport/subsonic/testdata/contract.json`. `TestDocumentedRequiredParameters` checks
missing required inputs across that inventory; filesystem-ID and account-limit
regressions cover cross-endpoint behavior rather than dispatch presence alone.

`uv run scripts/client-compatibility.py` starts the real binary with isolated media
and SQLite, then checks all 75 supported endpoints through real-client operations. It
includes actual podcast downloads, video/captions, sharing, account administration,
playlists, original and transcoded playback and HLS.
The unmodified py-sonic client handles 73 endpoints; direct HTTP covers the two video
methods described above. Its repeated-parameter GET helpers reference Python 2's
urllib2, so those client calls use POST; GET connectivity is checked separately.

Additional media tests verify encoded GET/HEAD Content-Length, per-account bitrate
limits, adaptive HLS bitrate@dimensions, caption conversion and preconverted MP4s.
Legacy filesystem IDs are distinct from ID3 identities and round-trip through search,
stars, lists, artwork, metadata and public sharing.

## Library regression coverage

Mixed-container fixtures cover WAV/AIFF fallback metadata, WMA discovery, fractional
Vorbis track/disc tags, timestamp-formatted years and audio-only MP4 classification.
Native search normalizes Unicode case at ingestion. Embedded lyrics and case-insensitive
adjacent artwork are read from the source library. Successful rescans advance index
cache timestamps even when added files preserve old modification times. Native and
legacy album views use the same track ordering.

FFprobe remains optional for original playback and required for complete duration,
bitrate and container fallback metadata. Path-based track identity is unchanged.
