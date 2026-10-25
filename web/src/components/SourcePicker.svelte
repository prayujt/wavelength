<script lang="ts">
  import { onMount } from 'svelte';
  import { Check, ExternalLink, X } from '@lucide/svelte';
  import type {
    ExternalTrack,
    SourceCandidate,
  } from '../gen/wavelength/v1/ingestion_pb';
  import { api } from '../lib/session.svelte';
  import { duration } from '../lib/format';
  import { errorMessage } from '../lib/api';
  import SongCredits from './SongCredits.svelte';
  import Notice from './Notice.svelte';
  import Modal from './Modal.svelte';
  let {
    track,
    choose,
    close,
    busy = false,
    error: submitError = '',
    action = 'Choose recording',
  }: {
    track: ExternalTrack;
    choose: (candidate: SourceCandidate) => void;
    close: () => void;
    busy?: boolean;
    error?: string;
    action?: string;
  } = $props();
  let candidates = $state<SourceCandidate[]>([]);
  let loading = $state(true);
  let error = $state('');
  onMount(() => {
    const controller = new AbortController();
    api.discovery
      .findSources({ metadataRef: track.ref }, { signal: controller.signal })
      .then((result) => {
        candidates = result.candidates;
      })
      .catch((cause) => {
        if (!controller.signal.aborted) error = errorMessage(cause);
      })
      .finally(() => (loading = false));
    return () => controller.abort();
  });
</script>

<Modal label={`Choose a recording for ${track.title}`} {close}>
  <div class="section-heading">
    <div>
      <p class="entity-type">
        {action === 'Replace audio' ? 'Replace audio' : 'Choose a recording'}
      </p>
      <h2 id="source-title">{track.title}</h2>
      <p class="muted">
        <SongCredits {track} /> · {duration(track.durationSeconds)}
      </p>
    </div>
    <button
      class="icon-button"
      aria-label="Close recordings"
      disabled={busy}
      onclick={close}><X size={20} /></button
    >
  </div>
  <Notice message={error} />
  <Notice message={submitError} />
  {#if loading}<div class="loading-state" role="status">
      <span class="spinner"></span> Looking for recordings…
    </div>{:else if !candidates.length && !error}<p class="muted">
      No recordings found. Try again later.
    </p>{/if}
  <div class="source-list">
    {#each candidates as candidate}<div class="source-row">
        <button
          class="source-item"
          disabled={busy || !candidate.ref}
          onclick={() => candidate.ref && choose(candidate)}
          ><span class="source-score" class:suitable={candidate.suitable}
            >{Math.round(candidate.score * 100)}<small>rank</small></span
          ><span
            ><strong>{candidate.title}</strong><small
              >{candidate.uploader} · {duration(
                candidate.durationSeconds,
              )}</small
            ><small
              >{candidate.reason ||
                (candidate.suitable
                  ? 'Matches the requested song'
                  : 'Check this recording before choosing')}</small
            ></span
          ><Check size={17} /></button
        >{#if candidate.ref?.provider === 'youtube' && /^[A-Za-z0-9_-]{11}$/.test(candidate.ref.id)}<a
            class="icon-button source-link"
            href={`https://www.youtube.com/watch?v=${candidate.ref.id}`}
            target="_blank"
            rel="noopener noreferrer"
            aria-label={`Open ${candidate.title} on YouTube`}
            title="Open on YouTube"><ExternalLink size={16} /></a
          >{/if}
      </div>{/each}
  </div>
  <p class="small-text muted">
    Scores rank recording matches using metadata and, when enabled, source
    evidence. They do not measure audio quality or guarantee the right
    recording.
  </p>
</Modal>
