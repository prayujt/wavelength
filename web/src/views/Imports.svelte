<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import {
    CheckCircle2,
    Download,
    Play,
    RefreshCw,
    SlidersHorizontal,
    X,
  } from '@lucide/svelte';
  import type {
    ExternalTrack,
    ImportJob,
    ProviderRef,
  } from '../gen/wavelength/v1/ingestion_pb';
  import type { RealtimeState } from '../lib/realtime.svelte';
  import type { PlayerState } from '../lib/player.svelte';
  import { api } from '../lib/session.svelte';
  import { errorMessage } from '../lib/api';
  import { stateLabel } from '../lib/format';
  import { link } from '../lib/navigation';
  import Cover from '../components/Cover.svelte';
  import Notice from '../components/Notice.svelte';
  import SourcePicker from '../components/SourcePicker.svelte';
  let {
    initialID = '',
    player,
    realtime,
  }: {
    initialID?: string;
    player: PlayerState;
    realtime: RealtimeState;
  } = $props();
  let jobs = $state<ImportJob[]>([]);
  let selected = $state<ImportJob>();
  let total = $state(0);
  let offset = $state(0);
  let loading = $state(true);
  let refreshing = $state(false);
  let busy = $state(false);
  let error = $state('');
  let overrides = $state<Record<number, ProviderRef>>({});
  let choosing = $state<{ track: ExternalTrack; index: number }>();
  const controller = new AbortController();
  let request = 0;
  let refreshQueued = false;
  const seenUpdates = new Set<string>();
  let selectionRequest = 0;
  const active = (job: ImportJob) => ['queued', 'running'].includes(job.state);
  const completed = $derived(
    selected?.items.filter((item) => item.state === 'completed').length || 0,
  );
  const phases: Record<string, string> = {
    queued: 'Waiting to start',
    resolving: 'Looking up song details',
    downloading: 'Downloading audio',
    tagging: 'Adding metadata and artwork',
    publishing: 'Adding to library',
    completed: 'Ready to play',
    needs_selection: 'Choose a recording to continue',
    cancelled: 'Cancelled',
    failed: 'Download failed',
  };
  onMount(() => {
    void load();
    return () => {
      ++request;
      controller.abort();
    };
  });
  async function load(nextOffset = offset) {
    const generation = ++request;
    refreshing = true;
    try {
      const result = await api.ingestion.listImports(
        { limit: 20, offset: nextOffset },
        { signal: controller.signal },
      );
      let detail = result.imports.find(
        (job) => job.id === (initialID || selected?.id),
      );
      if (!detail && (initialID || selected?.id))
        detail = await api.ingestion.getImport(
          { id: initialID || selected?.id || '' },
          { signal: controller.signal },
        );
      if (!detail && !initialID)
        detail = result.imports.find(active) || result.imports[0];
      if (generation !== request) return;
      if (nextOffset !== offset) overrides = {};
      jobs = result.imports.map((job) => realtime.latest(job));
      total = result.total;
      offset = nextOffset;
      if (!initialID || detail?.id === initialID)
        selected = detail ? realtime.latest(detail) : undefined;
    } catch (cause) {
      if (generation === request && !controller.signal.aborted)
        error = errorMessage(cause);
    } finally {
      if (generation === request) {
        loading = false;
        refreshing = false;
        if (refreshQueued && !controller.signal.aborted) {
          refreshQueued = false;
          void load();
        }
      }
    }
  }
  $effect(() => {
    if (realtime.resetVersion) untrack(() => void load());
  });
  $effect(() => {
    const updates = realtime.imports;
    untrack(() => {
      let unknown = false;
      for (const job of Object.values(updates)) {
        if (seenUpdates.has(job.id)) continue;
        seenUpdates.add(job.id);
        if (!jobs.some((known) => known.id === job.id)) unknown = true;
      }
      jobs = jobs.map((job) => realtime.latest(job));
      if (selected) selected = realtime.latest(selected);
      if (unknown) {
        if (refreshing) refreshQueued = true;
        else void load();
      }
    });
  });
  $effect(() => {
    const id = initialID;
    if (!loading && id) untrack(() => void select(id));
  });
  async function select(id: string) {
    if (selected?.id === id) return;
    const generation = ++selectionRequest;
    overrides = {};
    choosing = undefined;
    error = '';
    const known = jobs.find((job) => job.id === id);
    if (known) {
      selected = realtime.latest(known);
      return;
    }
    try {
      const job = await api.ingestion.getImport(
        { id },
        { signal: controller.signal },
      );
      if (generation === selectionRequest) selected = realtime.latest(job);
    } catch (cause) {
      if (generation === selectionRequest && !controller.signal.aborted)
        error = errorMessage(cause);
    }
  }
  async function mutate(kind: 'cancel' | 'retry') {
    if (!selected || busy) return;
    const id = selected.id;
    busy = true;
    error = '';
    try {
      const job =
        kind === 'cancel'
          ? await api.ingestion.cancelImport({ id })
          : await api.ingestion.retryImport({
              id,
              overrides: Object.entries(overrides).map(
                ([index, sourceRef]) => ({ index: Number(index), sourceRef }),
              ),
            });
      if (selected?.id === id) {
        selected = job;
        overrides = {};
      }
      await load();
    } catch (cause) {
      error = errorMessage(cause);
    } finally {
      busy = false;
    }
  }
  async function playCompleted() {
    if (!selected || busy) return;
    busy = true;
    try {
      const ids = selected.items
        .filter((item) => item.state === 'completed' && item.trackId)
        .map((item) => item.trackId);
      const tracks = await Promise.all(
        ids.map(async (id) => (await api.library.getTrack({ id })).track),
      );
      await player.play(tracks.flatMap((track) => (track ? [track] : [])));
    } catch (cause) {
      error = errorMessage(cause);
    } finally {
      busy = false;
    }
  }
</script>

<header class="page-heading">
  <div>
    <h1>Downloads</h1>
    <p class="subtitle">Progress, recording choices, and completed music.</p>
  </div>
  <div class="entity-actions">
    <span class="muted small-text" role="status"
      >{realtime.connected ? 'Live updates' : 'Reconnecting…'}</span
    >
    <button
      class="button secondary"
      onclick={() => load()}
      disabled={refreshing || busy}><RefreshCw size={14} /> Refresh</button
    ><a class="button primary" href="#discover">Find music</a>
  </div>
</header>
<Notice message={error} />
{#if loading}<div class="loading-state" role="status">
    <span class="spinner"></span> Loading downloads…
  </div>{:else if !jobs.length && !selected && !error}<div class="empty-state">
    <Download size={27} />
    <h2>No downloads yet</h2>
    <p>Find music in Discover, select songs, and review your download.</p>
    <a class="button primary" href="#discover">Discover music</a>
  </div>{:else}<div class="imports-layout">
    <div class="job-list">
      {#each jobs as job}<a
          class="job-card"
          class:active={selected?.id === job.id}
          href={link(`imports/${encodeURIComponent(job.id)}`)}
          ><div class="job-icon">
            {#if job.state === 'completed'}<CheckCircle2
                size={19}
              />{:else}<Download size={19} />{/if}
          </div>
          <span
            ><strong
              >{job.items[0]?.metadata?.title || 'Music download'}{job.items
                .length > 1
                ? ` + ${job.items.length - 1}`
                : ''}</strong
            ><small
              >{job.items.filter((item) => item.state === 'completed').length} of
              {job.items.length} songs saved</small
            ></span
          ><span class="status" class:success={job.state === 'completed'}
            >{stateLabel(job.state)}</span
          ></a
        >{/each}
    </div>
    {#if selected}<section class="job-detail">
        <div class="section-heading">
          <div>
            <h2>
              {selected.state === 'completed'
                ? 'Download complete'
                : selected.state === 'needs_selection'
                  ? 'Choose a recording'
                  : 'Download progress'}
            </h2>
            <p class="muted small-text">
              {completed} of {selected.items.length} songs in your library
            </p>
          </div>
          <span class="status">{stateLabel(selected.state)}</span>
        </div>
        <progress
          class="download-progress"
          aria-label="Download progress"
          max={selected.items.length || 1}
          value={completed}
        ></progress>{#if selected.error}<Notice
            message={selected.error}
          />{/if}{#if selected.state === 'needs_selection'}<p
            class="download-hint"
          >
            Choose a recording for the songs below, then retry the remaining
            songs.
          </p>{/if}{#each selected.items as item, index}<div
            class="import-item"
          >
            {#if item.state === 'completed' && item.trackId}<a
                class="import-artwork-link"
                aria-label={`Open ${item.metadata?.title || 'song'} in library`}
                href={item.albumId
                  ? link(`album/${encodeURIComponent(item.albumId)}`)
                  : link('library', { track: item.trackId })}
                ><Cover
                  name={item.metadata?.album || 'Download'}
                  artwork={`/media/tracks/${encodeURIComponent(item.trackId)}/artwork`}
                  small
                /></a
              >{:else}<Cover
                name={item.metadata?.album ||
                  item.metadata?.title ||
                  'Download'}
                artwork={item.metadata?.artworkUrl}
                small
              />{/if}
            <div class="track-info">
              {#if item.state === 'completed' && item.trackId}<a
                  class="track-link"
                  href={item.albumId
                    ? link(`album/${encodeURIComponent(item.albumId)}`)
                    : link('library', { track: item.trackId })}
                  ><strong>{item.metadata?.title || `Song ${index + 1}`}</strong
                  ></a
                >{:else}<strong
                  >{item.metadata?.title || `Song ${index + 1}`}</strong
                >{/if}
              {#if item.state === 'completed' && item.artistId}<a
                  class="track-artist-link"
                  href={link(`artist/${encodeURIComponent(item.artistId)}`)}
                  >{item.metadata?.artist}</a
                >{:else}<span
                  >{item.metadata?.artist || item.metadataRef?.id}</span
                >{/if}<small class:accent={item.state === 'completed'}
                >{overrides[index]
                  ? 'New recording selected'
                  : phases[item.state] || stateLabel(item.state)}{item.reused
                  ? ' · Already in library'
                  : ''}</small
              >{#if item.error && item.state !== 'needs_selection'}<small
                  class="error-text">{item.error}</small
                >{/if}{#each item.warnings as warning}<small class="muted"
                  >{warning}</small
                >{/each}
            </div>
            {#if item.state !== 'completed' && !active(selected) && item.metadata}<button
                class="recording-button"
                aria-label={`Choose recording for ${item.metadata.title}`}
                disabled={busy}
                onclick={() => (choosing = { track: item.metadata!, index })}
                ><SlidersHorizontal size={14} /><span>Choose</span></button
              >{/if}
          </div>{/each}
        <div class="dialog-actions">
          {#if active(selected) || selected.state === 'needs_selection'}<button
              class="button secondary"
              disabled={busy}
              onclick={() => mutate('cancel')}
              ><X size={14} />{completed
                ? 'Cancel remaining'
                : 'Cancel download'}</button
            >{/if}{#if !active(selected) && selected.state !== 'completed'}<button
              class="button primary"
              disabled={busy}
              onclick={() => mutate('retry')}
              ><RefreshCw size={14} />{busy
                ? 'Retrying…'
                : completed === selected.items.length
                  ? 'Retry playlist'
                  : 'Retry remaining'}</button
            >{/if}{#if completed}<button
              class="button secondary"
              disabled={busy}
              onclick={playCompleted}><Play size={14} /> Play downloaded</button
            >{/if}
        </div>
        {#if completed}<p class="small-text muted download-hint">
            Completed songs are available in your library.
          </p>{/if}
      </section>{/if}
  </div>{/if}
{#if total > 20}<div class="pagination">
    <button
      class="button secondary compact"
      disabled={!offset || refreshing}
      onclick={() => load(offset - 20)}>Previous</button
    ><span>{offset + 1}–{Math.min(offset + 20, total)} of {total}</span><button
      class="button secondary compact"
      disabled={offset + 20 >= total || refreshing}
      onclick={() => load(offset + 20)}>Next</button
    >
  </div>{/if}
{#if choosing}<SourcePicker
    track={choosing.track}
    close={() => (choosing = undefined)}
    choose={(candidate) => {
      if (choosing && candidate.ref)
        overrides = { ...overrides, [choosing.index]: candidate.ref };
      choosing = undefined;
    }}
  />{/if}
