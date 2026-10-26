# Listening history

`ListeningService` is available over Connect, gRPC and gRPC-Web. MCP exposes the
same service through four tools:

| RPC | MCP tool |
| --- | --- |
| `StartListeningSession` | `start_listening_session(track_id, origin, request_id)` |
| `ReportListeningProgress` | `report_listening_progress(session_id, listened_ms, position_ms, playing)` |
| `FinishListeningSession` | `finish_listening_session(session_id, outcome, listened_ms, position_ms)` |
| `ListListeningSessions` | `list_listening_sessions(track_id?, artist_id?, album_id?, since?, until?, origin?, outcome?, limit?, offset?)` |

These calls record activity; audio still travels through the media endpoint.
The web player reports automatically when playback starts, every 15 seconds while
its media clock updates, on pause, on seeks made through the player, and on track
changes. Pausing and resuming retain the same session. Listening time excludes
pauses, buffering and seek jumps. Origin is `direct` for a selection, `queue` for
subsequent queued tracks, or `restored` when resuming the saved queue. Outcome is
`completed` at the end, `skipped` when changing tracks, or `stopped` on player
teardown. An interrupted client can leave an unfinished session; no skip or
completion is inferred. Reporting is best effort, so abrupt tab closure can lose
the last interval. Failed starts retry on the next report with the same ID.

Each SQLite record contains the track's metadata snapshot, artist and album IDs,
timestamps, cumulative listened milliseconds, last position, origin and
outcome. Metadata survives subsequent library changes. List queries combine
supplied filters, ordered newest first with an ID tie breaker; defaults are 100
rows with a maximum of 500. `since` is inclusive and `until` exclusive, both based
on session start time. RPC timestamps use protobuf Timestamp; MCP uses RFC3339.
Artist/album filters use local catalog IDs returned by library browsing, including
its album-artist grouping. Origin and outcome filters accept the values above.

Reusing a request ID with the same track and origin returns the original session.
A different input with that ID conflicts. Cumulative time cannot decrease;
finished sessions cannot be reopened or assigned a different outcome. Sessions are
isolated by authenticated owner and library root. Reading requires a named OIDC
identity and `wavelength:read`; reporting also requires `wavelength:write`.
Browser sessions use their usual CSRF protection. Static development tokens do
not represent a listener.

Anonymous share visitors and raw audio requests are not added to the owner's
personal history. External players using direct URLs must report their activity
through RPC or MCP if they want this detail captured. Legacy scrobbles and the
existing `RecordPlay`/`get_history` aggregate counts remain available separately;
they cannot reconstruct duration or skips from old playback.

The installed Expo PWA keeps durable local listening checkpoints and replays them
after validating the account when connectivity returns. RPC requests accept
optional `occurred_at` timestamps, preserving the original start/progress/end time
instead of assigning reconnection time. Omission keeps server-time behavior.
Timestamps must be valid, nonnegative, no more than five minutes in the future,
and progress cannot precede its session start.

An RPC start may set `count_play: true` to increment aggregate play history in the
same SQLite transaction as the first insertion of its request ID. Such clients
must not also send a counting `RecordPlay`; they may use `now_playing_only` for
the live indicator. Replaying the same start cannot increment the count twice.
Existing Svelte, native and MCP callers retain their previous behavior by
omitting this flag. PWA outboxes are isolated by server and stable account ID;
auth/network failures remain pending, while unavailable/deleted-track or invalid
events are retained separately as failed records. Abrupt OS termination can
still lose the interval since the last durable checkpoint.

## Personalized play feeds

`PlayFeedService.GetPlayFeed` is available over Connect, gRPC and gRPC-Web, using
the same named identity and `wavelength:read` authorization as personal history.
There is no MCP feed tool. It returns playable tracks already in the catalog;
it neither downloads music nor changes a saved queue. See
[playfeed.proto](../proto/wavelength/v1/playfeed.proto) for the wire contract.

Start with a unique `request_id`, optional `seed_track_id`, ordered
`queued_track_ids` (current and manual upcoming tracks), and temporary
`excluded_track_ids`. The default page size is 10, maximum 25; each exclusion
list has at most 1,000 IDs. Use `next_cursor` for subsequent pages and stop when
`exhausted` is true. Requests are idempotent for 24 hours; reuse with different
input conflicts. Cursors are opaque, bound to the owner and library, and expire
after an hour without use. A feed retains at most 100 pages, with at most 128
active feeds per listener. Invalid/expired cursors return `failed_precondition`;
start a fresh feed with a new request ID. New work is limited to a burst of four,
then one request per second per listener (`resource_exhausted`); retries of saved
requests do not rerank. Issued pages are stable, except that deleted tracks are
removed. Unissued pages read current listening history and queue context.

The ranker uses up to 500 listening sessions from the last 90 days, summarized
into at most 50 track preferences. Completion and substantial listening are
positive signals. An explicit Next within 10 seconds and 10% of the track is a
strong negative signal for that recording, withheld for two hours. Other track
changes are weaker context; pauses, stops and unfinished sessions are not
dislikes. Recently heard recordings are withheld for 15 minutes. Candidate
sampling mixes favorite artists with the rest of the catalog, and selection
penalizes consecutive/repeated artists. Unknown songs remain eligible.

With the existing backend-only `WAVELENGTH_JEV_API_KEY`, Jev ranks a bounded
pool of up to 120 catalog candidates using musical metadata, current/manual
queue context, and the compact listening summary. Account IDs, file paths,
credentials and media URLs are excluded. This is separate from recording-match
ranking for downloads. Ranking work has a 12-second bound and a short context
cache; provider errors or invalid answers use local preference/diversity scoring.
The service exposes `wavelength_play_feed_requests_total`,
`wavelength_play_feed_duration_seconds` and `wavelength_play_feed_items` (histogram),
with bounded source/outcome labels and no user/track IDs.

The web, native app and installed PWA refresh only unplayed generated queue
entries in the background after playback or queue actions, coalescing rapid
operations. Current and manually queued entries stay in place. Their queue view
labels recommendations and provides an Autoplay toggle; turning it off removes
future generated entries. Explicitly moving a recommendation makes it manual.
Repeat modes retain their explicit behavior and suspend feed work. Generated
entry IDs and the toggle persist with the saved queue, including offline PWA
storage. Offline playback retains its queue; on reconnection, durable listening
reports sync before a recommendation request. Stale responses cannot replace a
new selection or another account's queue.

For attribution, pass a returned item's `id` as `play_feed_item_id` on
`StartListeningSession`. Receipts are valid for 30 days and must belong to the
same owner, library and track. Unknown, expired or foreign attribution is cleared
without discarding the listening report, so delayed offline sync still works.
`FinishListeningSession.change_reason` is `next`, `previous`, `selection`,
`ended`, or `stop`; clients may omit it for legacy behavior. Only `next` qualifies
as an intentional early skip. The first finished outcome/reason is immutable.
