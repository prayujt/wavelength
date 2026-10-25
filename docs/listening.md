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
