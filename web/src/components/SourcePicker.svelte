<script lang="ts">
  import { onMount } from 'svelte';
  import { Check, X } from '@lucide/svelte';
  import type {
    ExternalTrack,
    SourceCandidate,
  } from '../gen/wavelength/v1/ingestion_pb';
  import { api } from '../lib/session.svelte';
  import { duration } from '../lib/format';
  import { errorMessage } from '../lib/api';
  import Notice from './Notice.svelte';
  import Modal from './Modal.svelte';
  let {
    track,
    choose,
    close,
  }: {
    track: ExternalTrack;
    choose: (candidate: SourceCandidate) => void;
    close: () => void;
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
      <p class="entity-type">Choose a recording</p>
      <h2 id="source-title">{track.title}</h2>
      <p class="muted">{track.artist} · {duration(track.durationSeconds)}</p>
    </div>
    <button class="icon-button" aria-label="Close recordings" onclick={close}
      ><X size={20} /></button
    >
  </div>
  <Notice message={error} />
  {#if loading}<div class="loading-state" role="status">
      <span class="spinner"></span> Looking for recordings…
    </div>{:else if !candidates.length && !error}<p class="muted">
      No recordings found. Try again later.
    </p>{/if}
  <div class="source-list">
    {#each candidates as candidate}<button
        class="source-item"
        disabled={!candidate.ref}
        onclick={() => candidate.ref && choose(candidate)}
        ><span class="source-score" class:suitable={candidate.suitable}
          >{Math.round(candidate.score * 100)}<small>match</small></span
        ><span
          ><strong>{candidate.title}</strong><small
            >{candidate.uploader} · {duration(candidate.durationSeconds)}</small
          ><small
            >{candidate.reason ||
              (candidate.suitable
                ? 'Matches the requested song'
                : 'Check this recording before choosing')}</small
          ></span
        ><Check size={17} /></button
      >{/each}
  </div>
  <p class="small-text muted">
    Match scores compare the title, artist, and duration. They are a guide for
    choosing a recording.
  </p>
</Modal>
