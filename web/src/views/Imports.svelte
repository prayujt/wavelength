<script lang="ts">
  import { onMount } from 'svelte';
  import {
    CheckCircle2,
    Download,
    RefreshCw,
    SlidersHorizontal,
    X,
  } from '@lucide/svelte';
  import type {
    ExternalTrack,
    ImportJob,
    ProviderRef,
  } from '../gen/wavelength/v1/ingestion_pb';
  import { api } from '../lib/session.svelte';
  import { errorMessage } from '../lib/api';
  import { stateLabel } from '../lib/format';
  import Notice from '../components/Notice.svelte';
  import SourcePicker from '../components/SourcePicker.svelte';

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
  let controller: AbortController;
  let timer: ReturnType<typeof setTimeout>;
  let request = 0;
  const active = (job: ImportJob) => ['queued', 'running'].includes(job.state);
  onMount(() => {
    controller = new AbortController();
    void load();
    return () => {
      ++request;
      controller.abort();
      clearTimeout(timer);
    };
  });
  async function load(nextOffset = offset) {
    const generation = ++request;
    refreshing = true;
    clearTimeout(timer);
    try {
      const result = await api.ingestion.listImports(
        { limit: 20, offset: nextOffset },
        { signal: controller.signal },
      );
      if (generation !== request) return;
      if (nextOffset !== offset) {
        selected = undefined;
        overrides = {};
      }
      jobs = result.imports;
      total = result.total;
      offset = nextOffset;
      if (selected) {
        const fresh = jobs.find((job) => job.id === selected?.id);
        if (fresh) selected = fresh;
      }
    } catch (cause) {
      if (generation === request && !controller.signal.aborted)
        error = errorMessage(cause);
    } finally {
      if (generation === request) {
        loading = false;
        refreshing = false;
        if (!controller.signal.aborted && jobs.some(active))
          timer = setTimeout(() => void load(), 3000);
      }
    }
  }
  async function open(job: ImportJob) {
    if (busy) return;
    error = '';
    overrides = {};
    busy = true;
    try {
      selected = await api.ingestion.getImport(
        { id: job.id },
        { signal: controller.signal },
      );
    } catch (cause) {
      if (!controller.signal.aborted) error = errorMessage(cause);
    } finally {
      busy = false;
    }
  }
  async function mutate(kind: 'cancel' | 'retry') {
    if (!selected) return;
    busy = true;
    error = '';
    try {
      selected =
        kind === 'cancel'
          ? await api.ingestion.cancelImport({ id: selected.id })
          : await api.ingestion.retryImport({
              id: selected.id,
              overrides: Object.entries(overrides).map(
                ([index, sourceRef]) => ({ index: Number(index), sourceRef }),
              ),
            });
      overrides = {};
      await load();
    } catch (cause) {
      error = errorMessage(cause);
    } finally {
      busy = false;
    }
  }
</script>

<header class="page-heading">
  <div>
    <p class="eyebrow">On their way home</p>
    <h1>Downloads<span class="accent">.</span></h1>
    <p class="subtitle">
      Your music keeps downloading when you leave this page.
    </p>
  </div>
  <button
    class="button secondary"
    onclick={() => load()}
    disabled={refreshing || busy}><RefreshCw size={16} /> Refresh</button
  >
</header>
<Notice message={error} />
{#if loading}<div class="loading-state" role="status">
    <span class="spinner"></span> Loading downloads…
  </div>
{:else if !jobs.length && !error}<div class="empty-state">
    <div class="empty-icon"><Download size={30} /></div>
    <h2>A clean slate</h2>
    <p>Choose a few songs in Discover. You'll see their progress here.</p>
  </div>
{:else}<div class="imports-layout">
    <div class="job-list">
      {#each jobs as job}<button
          class:active={selected?.id === job.id}
          class="job-card"
          onclick={() => open(job)}
          ><div class="job-icon">
            {#if job.state === 'completed'}<CheckCircle2
                size={22}
              />{:else}<Download size={22} />{/if}
          </div>
          <span
            ><strong
              >{job.items[0]?.metadata?.title || 'Music import'}{job.items
                .length > 1
                ? ` + ${job.items.length - 1}`
                : ''}</strong
            ><small
              >{job.items.filter((item) => item.state === 'completed').length} of
              {job.items.length} songs added</small
            ></span
          ><span class="status" class:success={job.state === 'completed'}
            >{stateLabel(job.state)}</span
          ></button
        >{/each}
    </div>
    {#if selected}<section class="job-detail">
        <div class="section-heading">
          <h2>Import details</h2>
          <span class="status">{stateLabel(selected.state)}</span>
        </div>
        {#if selected.error}<Notice
            message={selected.error}
          />{/if}{#each selected.items as item, index}<div class="import-item">
            <div>
              <strong>{item.metadata?.title || `Song ${index + 1}`}</strong
              ><span class="muted"
                >{item.metadata?.artist || item.metadataRef?.id} · {stateLabel(
                  item.state,
                )}</span
              >{#if item.reused}<small class="accent"
                  >Already in your library</small
                >{/if}{#if item.error}<small class="error-text"
                  >{item.error}</small
                >{/if}{#each item.warnings as warning}<small class="muted"
                  >{warning}</small
                >{/each}
            </div>
            {#if item.state !== 'completed' && !active(selected) && item.metadata}<button
                class:accent={!!overrides[index]}
                class="icon-button"
                aria-label={`Choose recording for ${item.metadata.title}`}
                disabled={busy}
                onclick={() => (choosing = { track: item.metadata!, index })}
                ><SlidersHorizontal size={18} /></button
              >{/if}
          </div>{/each}
        <div class="dialog-actions">
          {#if active(selected)}<button
              class="button secondary"
              disabled={busy}
              onclick={() => mutate('cancel')}
              ><X size={16} /> Cancel remaining</button
            >{:else if selected.state !== 'completed'}<button
              class="button primary"
              disabled={busy}
              onclick={() => mutate('retry')}
              ><RefreshCw size={16} />{busy
                ? 'Retrying…'
                : 'Retry remaining'}</button
            >{/if}
        </div>
      </section>{:else if jobs.length}<div class="detail-placeholder muted">
        Select a download to see its progress.
      </div>{/if}
  </div>{/if}
{#if total > 20}<div class="pagination">
    <button
      class="button secondary compact"
      disabled={offset === 0 || refreshing}
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
    choose={(ref) => {
      if (choosing) overrides = { ...overrides, [choosing.index]: ref };
      choosing = undefined;
    }}
  />{/if}
