# Legacy Subsonic compatibility

The target is the documented Subsonic REST API 1.16.1 envelope and core music
workflow. Of its 76 documented endpoints, 75 respond, including 32 deliberately
stubbed endpoints. Jukebox remains explicitly unsupported. This is not full original
Subsonic or OpenSubsonic parity.

## Functional music features

- System: ping, fixed license response, one configured music folder, and idle scan-status compatibility responses.
- Browsing: filesystem indexes/directories, tag-based artists/albums/songs, genres,
  search variants, album lists, random songs and songs by genre.
- Collections: persisted playlists, bookmarks and play queues.
- Listening: local play counts, last-played timestamps and reported now-playing entries.
- Media: original playback/downloads with byte ranges and HEAD, FFmpeg audio
  transcoding, audio HLS, embedded/adjacent artwork and embedded/sidecar lyrics.
- Discovery: local genre relationships and listening counts; no external enrichment.

FFmpeg is needed for conversion and audio HLS. Metadata comes from the persisted
catalog; no files are discovered or classified automatically. `startScan` and
`getScanStatus` return `scanning: false` and the persisted track count.
Missing lyrics return an empty result. Missing artwork returns a protocol error.

## Empty compatibility surface

These handlers require authentication and documented required parameters, but have
no backing feature service, persistent mutations, downloads or outbound requests.

| Feature | Read response | Command behavior |
| --- | --- | --- |
| Podcasts | Empty `podcasts.channel` and `newestPodcasts.episode` arrays | Subscribe, refresh, download and delete commands acknowledge without doing work |
| Video | Empty `videos.video`; `videoInfo` echoes the requested ID with empty caption/audio/conversion arrays | No video indexing, encoding or preconverted assets |
| Captions | Empty SRT body, or only the `WEBVTT` header for VTT | No subtitle reads or extraction |
| Radio | Empty `internetRadioStations.internetRadioStation` array | Create/update/delete are no-ops |
| Chat | Empty `chatMessages.chatMessage` array | Adding a message is a no-op |
| Avatars | HTTP 200 with an empty image body | Existing avatar files are not read |
| Sharing | Empty `shares.share` array, including `createShare` | Update/delete are no-ops; `/share/` routes return HTTP 404 |
| Favorites | Empty `starred`/`starred2` artist, album and song arrays | Star/unstar are no-ops |
| Ratings | Optional rating fields are absent; highest-rated and starred album lists are empty | `setRating` validates the numeric range and then does nothing |

JSON collections are arrays, never null. XML uses the same schema-shaped containers.
These are intentional compatibility stubs and must not be advertised as real features.

## One configured legacy identity

Only `WAVELENGTH_SUBSONIC_USER` (default `admin`) and
`WAVELENGTH_SUBSONIC_PASSWORD` authenticate. Plain/hex passwords and salted-token
legacy authentication remain supported. Native bearer/OIDC authentication stays separate.

`getUsers` reports that one identity; `getUser` returns it or a missing-user error.
Role fields are fixed compatibility metadata. Settings, ratings, podcast, sharing,
video conversion, upload, jukebox and external scrobbling capabilities are disabled.
User creation, updates, deletion and password changes acknowledge without persisting
anything or changing credentials. There is no local-account database or password key.

Old account/share/podcast/radio/chat records and files are ignored. Old stars/ratings
are not projected into responses. Existing playlists, bookmarks, queues and listening
counts are preserved. The obsolete `WAVELENGTH_SUBSONIC_PUBLIC_URL` setting is ignored.

## Verification and limits

- `go test -race ./...` covers authentication, core collections, browsing, protocol
  errors and stateless stubs. Tests seed old records, verify they stay invisible,
  ensure commands leave storage unchanged and check that no feed requests occur.
- Media tests encode real audio, decode transcoding/HLS results and check stream ownership,
  traversal protection and temporary-file cleanup. Tests seed isolated catalog records
  directly; they do not depend on filesystem ingestion.
- `scripts/smoke.py` exercises the real binary, both listeners, ranges/HEAD, pruning,
  restart persistence, source immutability and graceful shutdown.
- `scripts/client-compatibility.py` checks all 75 responding endpoints using py-sonic
  and direct HTTP, distinguishing real music functionality from empty stubs.
- `scripts/validate-subsonic.py` validates exported XML fixtures and JSON field types
  against the pinned official 1.16.1 XSD, with the documented string-ID correction
  for `PlayQueue.current`. Music-folder IDs remain integers in both `getMusicFolders`
  and user permissions, while artist, album and track IDs are strings.

The endpoint inventory is in `internal/transport/subsonic/testdata/contract.json`.
Required-input checks cover that inventory. Real-client coverage does not certify every
parameter combination or every desktop/mobile app. One library root, path-based track
IDs and the minimal native RPC API are unchanged. There is no LDAP integration,
original administration UI or OpenSubsonic extension parity.
