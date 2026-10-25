# Music discovery and ingestion

Wavelength uses explicit acquisition jobs to add music. It never imports a directory,
rescans after a download, or treats a file appearing on disk as a catalog change.
Spotify imports are outside this capability. Saved audio can be replaced explicitly while keeping its catalog identity.

## Workflow

1. Search iTunes music metadata with `search_music` / `DiscoveryService.Search`.
2. Inspect an artist's available releases or a release's ordered tracks.
   `lookup_imports` / `IngestionService.LookupImports` maps up to 200 exact provider
   references to tracks already in the shared library, so clients can offer Play
   instead of another download. It reads provenance without contacting a provider
   or inspecting source files; it requires read permission rather than ingestion permission.
   Native clients can resolve up to 20 album track lists in one `DiscoveryService.GetReleases`
   call, then look up every song's exact reference. Search cards use those full lists
   for partial and complete library badges; matching a few search hits never proves
   that an entire album is saved.
3. Find candidate recordings for a track. Results expose uploader, duration, match
   score, suitability, and a reason. Scores rank metadata evidence, not audio quality
   or fingerprint identity.
4. Submit selected tracks or one release. Explicit YouTube source references bypass
   automatic selection. Without one, only strong, unambiguous matches are accepted.
   Discovery metadata already seen by the server is included in queued receipts, so
   titles and artwork can appear before a worker starts. This display preview is
   persisted with the job and does not bypass authoritative metadata resolution.
5. Subscribe to `RealtimeService.Subscribe` before loading the job snapshot, or poll the returned job. Completed items contain local track IDs for existing
   playback and collection APIs. Ambiguous items pause at `needs_selection`; choose
   a source and retry with a zero-based item override.

Metadata and audio references are separate from local IDs. Supported providers are
`itunes` (numeric metadata IDs) and `youtube` (11-character video IDs). URLs, shell
options and client-supplied filesystem paths are not accepted as references.

Individual-song imports resolve the album artist from the song's exact iTunes
collection, keeping guest performers as track artists. If the collection credit
cannot be resolved, metadata resolution fails before downloading so the import
can be retried. Search results may leave the album artist unset until resolution.

## Recording ranking

Without `WAVELENGTH_JEV_API_KEY`, ranking uses the existing title-word overlap (45%),
artist-word overlap (30%), and duration similarity (25%). Automatic selection requires
a suitable result scoring at least 0.85 with a 0.05 lead over other suitable results.

Set the backend environment variable `WAVELENGTH_JEV_API_KEY` to enable
[TypeSafe Jev](https://docs.typesafe.ai/api/system-one). The browser and MCP clients
never receive this key. No RPC, MCP tool, or request/response fields change.
`WAVELENGTH_JEV_MODEL` defaults to the pinned `jev-1.14.0`; changing the model may
change its judgments and should be evaluated before relying on automatic selection.

One request evaluates all candidates. It includes the complete resolved iTunes track
(title, performers, album, album artist, genre, date, duration, track/disc numbers,
artwork URL and provider reference), each candidate's canonical YouTube URL, title,
uploader and duration, and **every field** yt-dlp returned for that search entry.
That includes artist/album, description, channel ID/verification, thumbnails and other
provider metadata when available. Flat YouTube search does not guarantee those fields;
missing evidence stays missing. Wavelength does not fetch or listen to candidate audio
to rank it. Only public music metadata is sent, not users, playlists, or OAuth credentials.

Jev answers two questions per candidate: a Noul probability of the same complete
recording/version, and a source-evidence rubric from 0 to 4 that favors corroborated
artist, label or auto-generated releases over uncertain fan uploads. The returned
rank is `recording_probability * (0.8 + 0.2 * source_rubric / 4)`. It is a composite
rank, **not a calibrated percentage of correctness**. Source preference can reduce
the rank but cannot lift a weak recording match above the recording probability. Existing title/artist, duration
and alternate-version checks still apply; Jev cannot override their rejection.
Suitability also requires recording probability >= 0.90. Automatic selection requires
rank >= 0.90 and a 0.03 margin over every other suitable candidate. These are conservative
policy values, covered by fixtures rather than a measured music benchmark.
For an audio library, corroborated artist/label audio and Topic/YouTube Music releases
are preferred over fan lyric videos. Official lyric videos remain valid alternatives.
Recording identity and source preference are evaluated separately: punctuation and
on-screen lyrics do not change the recording, and absent optional metadata alone
is not evidence of a mismatch.

Requests have an eight-second deadline, at most two concurrent calls, and bounded
input/output. Successful results are cached in memory for ten minutes (up to 256
track/candidate/model/question combinations). Credentials and upstream error bodies
are not logged. A timeout, rate limit, invalid answer or oversized input preserves
candidate choices with score zero, suitability false and a manual-selection reason.
Automatic imports then pause at `needs_selection`; they never silently fall back to
heuristic automatic downloading while Jev is enabled. Retrying with an explicit
`source_ref` bypasses ranking as before. Completed imports and playback are unaffected.

## Replacing saved audio

Resolve a saved song's provenance with `LookupImports` / `lookup_imports` using
`track_ids`. Queue one item with that `metadata_ref`, an explicit YouTube
`source_ref`, and `replace_track_id` set to the saved ID. Replacement requires
ingestion permission and cannot be combined with an album import or playlist attachment.

The worker downloads and validates a separate asset, preserves embedded artwork
and lyrics, then switches the catalog path and source provenance in the same
transaction as the completed job receipt. The ID, catalog metadata, playlists,
queue and listening history remain intact. A failed or cancelled replacement
leaves the original playable. Completed replacements publish a library event;
HTTP validators and HLS session keys change with the asset.

## Authentication and interfaces

MCP and native clients use the same delegated OAuth identity. Ingestion requires
`wavelength:read` and `wavelength:ingest`; a target playlist additionally requires
`wavelength:write`. Native browser sessions use their signed-in user's permissions.
Shared development tokens and Subsonic credentials cannot own acquisition jobs.
Existing delegated grants need fresh consent to add ingestion permission.

MCP tools are `search_music`, `get_music_artist`, `get_music_release`, `lookup_imports`,
`find_audio_sources`, `import_music`, `get_import`, `list_imports`, `cancel_import`,
and `retry_import`. They call the same services as DiscoveryService and
IngestionService in [ingestion.proto](../proto/wavelength/v1/ingestion.proto).

After ordinary MCP initialization, each tool call is a POST to `/mcp` with:

```http
Authorization: Bearer <Wavelength OAuth access token>
Content-Type: application/json
Accept: application/json, text/event-stream
MCP-Protocol-Version: <negotiated version>
```

For example, these are separate JSON-RPC request bodies. References and IDs are
illustrative; use values returned by discovery.

```json
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_music","arguments":{"query":"Morgan Wallen Last Night","limit":5}}}
```

```json
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"find_audio_sources","arguments":{"metadata_ref":{"provider":"itunes","id":"123"}}}}
```

```json
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"import_music","arguments":{"request_id":"my-import-001","items":[{"metadata_ref":{"provider":"itunes","id":"123"},"source_ref":{"provider":"youtube","id":"BaW_jenozKc"}}]}}}
```

```json
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_import","arguments":{"id":"returned-import-id"}}}
```

A release import supplies `release_ref` instead of `items`; metadata expansion runs
inside the durable job. Retry can supply `overrides`, for example:

```json
{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"retry_import","arguments":{"id":"returned-import-id","overrides":[{"index":0,"source_ref":{"provider":"youtube","id":"BaW_jenozKc"}}]}}}
```

The generated native clients support Connect, Connect JSON, gRPC-Web and gRPC.
For actual gRPC, clients send Protobuf messages with bearer metadata over HTTP/2.
A Connect JSON request for the same import looks like:

```sh
curl https://music.example.com/wavelength.v1.IngestionService/CreateImport \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Connect-Protocol-Version: 1' \
  -d '{"requestId":"my-import-001","items":[{"metadataRef":{"provider":"itunes","id":"123"},"sourceRef":{"provider":"youtube","id":"BaW_jenozKc"}}]}'
```

## Jobs and publication

Requests require an idempotency key, scoped to requester and music root. Repeating
an identical request returns its existing job; reusing the key with different inputs
is a conflict. A job accepts 1–200 tracks or one release of up to 200 tracks. The
queue permits 100 unfinished queued/running jobs. Searches return up to 50 results
per entity kind; artist release coverage is limited to the provider's lookup results.

Jobs survive client disconnection and process restarts. A bounded pool processes them
in the single application instance. `WAVELENGTH_INGEST_CONCURRENCY` defaults to two
concurrent item attempts, with a supported range of one to eight. Both separate jobs
and items within an album can run concurrently, sharing the same global limit.
Duplicate metadata references serialize before the second attempt rechecks and
reuses the committed track. Provider rate limits still apply. The yt-dlp command
budget includes one additional slot for interactive searches. Increase concurrency
with CPU/memory and provider capacity in mind; this is not multi-replica job ownership.
States are `queued`, `running`, `completed`,
`failed`, `partial`, `needs_selection`, and `cancelled`. Item stages additionally
show `resolving`, `downloading`, `tagging`, and `publishing`. Read snapshots through
GetImport, or subscribe to `RealtimeService.Subscribe` for owner-scoped job updates
and shared publication messages. Subscribe before reading initial snapshots and
refresh them after each ready message, including reconnects. The stream requires
a signed-in user with `wavelength:read`, sends heartbeats, and reconnects every
five minutes to reauthorize. It supports Connect, gRPC-Web, and gRPC; proxies must
forward streaming responses without buffering. Job completion is persisted before
notifications are emitted, and jobs continue when clients disconnect.

Completed items include local album and artist IDs for navigation. Older job
records remain readable; clients can use their track ID when those fields are
absent. Artist search includes representative artwork from matching provider IDs
already present in song/album results, without a lookup for each artist.

An item downloads into the data directory, validates its MP3 audio, writes metadata,
and optionally embeds artwork and lyrics. Missing artwork/lyrics produce warnings,
not failed audio imports. Acquisition is limited to non-live sources of up to two
hours and 256 MiB before tagging. Metadata/source requests and individual downloads
have bounded deadlines. Automatic choices also check the downloaded duration.

A prepared file and checksum are recorded before publication. A temporary file on
the music filesystem is synced and renamed into its final artist/album directory;
then one SQLite transaction inserts metadata/provenance and records the item's
completion. A crash between those operations resumes from that exact job and file,
without repeating a completed download or walking the library.

New tracks have opaque stable IDs, and their physical paths are generated by the
server. Provider text is confined to safe directory components. Existing catalog IDs
and source files are preserved. Exact provider track references deduplicate across
imports and users; old catalog records without provider provenance are not guessed
or retroactively merged by title. An existing catalog track with missing source
bytes is reported as an error rather than falsely reported as playable.

A cancelled import stops its active subprocess group, preserves already published
tracks, and cleans unpublished assets. Cancellation cleanup is recorded and resumed
after a crash. Retrying while the old worker is still stopping can return a conflict;
retry after it exits. Other users cannot inspect, cancel or retry your jobs.

Cancellation stops every active item in that job and leaves other jobs running.
Shutdown waits for all workers before closing source handles and the database.
Interrupted jobs resume from their recorded items after restart. Playlist publication
waits for all items and always uses request order, regardless of completion order.

## Prometheus metrics

Set `WAVELENGTH_METRICS_ADDR=127.0.0.1:9090` for `GET /metrics` on a separate operational
listener. It is disabled by default and is not mounted on native/UI, OAuth, or
Subsonic listeners. Metrics are unauthenticated: bind loopback locally or restrict
the port to trusted scrapers with network policy. They contain bounded categories,
not track IDs, playlist names, user identities, URLs, provider keys, or error text.

The per-instance registry includes:

| Metrics | Meaning |
| --- | --- |
| `wavelength_downloads_total{outcome}` | Audio download attempts, including failures and repeated attempts. |
| `wavelength_tracks_published_total{kind}` | Successfully published imports and replacements. |
| `wavelength_audio_published_bytes_total{kind}`, `wavelength_audio_file_size_bytes` | Tagged MP3 output bytes and size distribution; network transfer is separate. |
| `wavelength_ingest_operations_total{operation,outcome}` | Create, retry and cancel calls. |
| `wavelength_ingest_item_attempts_total{outcome}` | Completed, reused, failed, ambiguous, cancelled or interrupted item attempts. |
| `wavelength_ingest_item_duration_seconds`, `wavelength_ingest_job_duration_seconds` | Active execution time per attempt; retries are distinct observations. |
| `wavelength_ingest_queue_wait_seconds`, `wavelength_ingest_oldest_queued_job_age_seconds` | Submission/retry-to-start latency and oldest queued age. Recovery/cleanup does not generate queue-wait samples. |
| `wavelength_ingest_item_queue_wait_seconds`, `wavelength_ingest_worker_wait_duration_seconds{kind}` | Per-item backlog latency, including time behind earlier batch items; global worker-slot and duplicate-reference lock waits. |
| `wavelength_ingest_jobs{state}`, `wavelength_ingest_items{state}` | Current persisted inventory by state, read from SQLite during the scrape. |
| `wavelength_ingest_items_active`, `wavelength_ingest_jobs_active`, `wavelength_ingest_concurrency_limit` | Busy item slots, active jobs, and the configured global item limit. |
| `wavelength_ingest_stage_duration_seconds{stage,outcome}`, `wavelength_ingest_stage_active{stage}` | Latency and activity for metadata search/lookups, source search, ranking, download/conversion, probing, tagging, artwork, lyrics, tag writing and publication. |
| `wavelength_source_candidates`, `wavelength_source_selections_total{decision}` | Search shortlist sizes and automatic, explicit or manual-required decisions. |
| `wavelength_provider_requests_total`, `wavelength_provider_request_duration_seconds` | iTunes, YouTube, Jev, artwork and LRCLIB attempts, HTTP status categories or command outcomes, and latency. |
| `wavelength_provider_wait_duration_seconds{provider,kind}` | Rate-limit, retry-backoff and command/API-slot waits. |
| `wavelength_provider_cache_lookups_total`, `wavelength_jev_tokens_total` | Jev/album-artist cache effectiveness and Jev-reported input/output tokens. |
| `wavelength_library_tracks`, `wavelength_library_audio_bytes`, `wavelength_library_audio_duration_seconds` | Shared catalog totals. |
| `wavelength_ingest_queue_scrape_error` | A failed operational snapshot; failed reads do not publish misleading zero inventories. |
| `wavelength_http_*` | Request counts, inflight requests, full request latency, time to headers and request/response sizes by listener, method and status. HTTP/2 and streaming interfaces are preserved. |
| `go_*`, `process_*`, `go_sql_*` | Go runtime, process and SQLite connection-pool statistics. |

Latency histograms use seconds and include success, error, timeout and cancellation
outcomes. Metadata stage timings include provider waits; provider HTTP timings end at
response headers, while source commands run through completion. Tagging is an aggregate
containing probe/artwork/lyrics/tag-write stages, so those observations are not additive.
HTTP duration includes stream lifetime; use time-to-headers for streaming responsiveness.
Counters reset on process restart; use `rate()`/`increase()`. Queue and catalog gauges
read persisted state and survive restarts. Pending unexpanded album jobs have no item
inventory until their track list is resolved. Cache metrics count cache probes, including
the second check after waiting for a Jev slot, rather than unique logical requests.

Example queries:

```promql
# Downloads per minute and failed attempts over the last hour
sum(rate(wavelength_downloads_total{outcome="ok"}[5m])) * 60
sum(increase(wavelength_downloads_total{outcome="error"}[1h]))

# p95 active download/conversion latency and worker utilization
histogram_quantile(0.95, sum by (le) (rate(wavelength_ingest_stage_duration_seconds_bucket{stage="download",outcome="ok"}[5m])))
wavelength_ingest_items_active / wavelength_ingest_concurrency_limit

# Waiting work and p95 provider rate-limit wait
sum(wavelength_ingest_items{state="queued"})
histogram_quantile(0.95, sum by (le,provider) (rate(wavelength_provider_wait_duration_seconds_bucket{kind="rate_limit"}[5m])))
```

For a Kubernetes scraper using pod annotations, set the metrics bind address to
`0.0.0.0:9090`, declare container port `metrics: 9090`, and annotate the pod template:

```yaml
prometheus.io/scrape: "true"
prometheus.io/port: "9090"
prometheus.io/path: /metrics
```

Keep the metrics port out of public Services/Gateway routes. Extend the existing
ingress NetworkPolicy with a rule allowing TCP 9090 only from the scraper's namespace
and pod selector, such as namespace `monitoring` with pod label `app: prometheus`.
No Prometheus Operator or ServiceMonitor is required for annotation-based discovery.

An optional target playlist must belong to the requester. Once every item succeeds,
their IDs are appended in the requested order. The playlist update stores a receipt
so interrupted finalization cannot append a batch twice. A failed or partially
resolved batch leaves that playlist untouched; completed tracks are still available
in the shared catalog. Retry skips completed items, including after playlist errors.

## Deployment

Use writable persistent volumes for the music root and data directory. Staging is
under the data directory, so keep both volumes across restarts. Playback-only setups
can retain a read-only music mount, but acquisition jobs then fail publication.
There is no startup enumeration or metadata probing of existing music.

The image includes FFmpeg/ffprobe, yt-dlp 2026.10.21 with its EJS dependencies, and
Deno on Alpine 3.24. Source deployments need equivalent executables on PATH.
The optional bgutil PO-token provider plugin is included; where YouTube requires it,
run its HTTP helper alongside the application on the default localhost port 4416,
as in audio-scraper's deployment. No helper or browser session is provisioned by
Wavelength. Downloads use bounded fixed command arguments and ignore ambient
user yt-dlp configuration.

Provider behavior can change independently of Wavelength. Update the pinned runtime
dependency when necessary. Automated tests use deterministic provider fixtures;
external services are not contacted during CI.
