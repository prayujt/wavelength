<script lang="ts">
  import { Check, Play, SlidersHorizontal } from '@lucide/svelte';
  import type { ExternalTrack } from '../gen/wavelength/v1/ingestion_pb';
  import type { PlayerState } from '../lib/player.svelte';
  import { DownloadSelection, refKey } from '../lib/downloads.svelte';
  import { api } from '../lib/session.svelte';
  import { errorMessage } from '../lib/api';
  import { duration } from '../lib/format';
  import Cover from './Cover.svelte';
  import Notice from './Notice.svelte';
  import SourcePicker from './SourcePicker.svelte';
  let {
    tracks,
    basket,
    player,
    checking = false,
  }: {
    tracks: ExternalTrack[];
    basket: DownloadSelection;
    player: PlayerState;
    checking?: boolean;
  } = $props();
  let choosing = $state<ExternalTrack>();
  let error = $state('');
  async function play(track: ExternalTrack) {
    try {
      const result = await api.library.getTrack({ id: basket.localID(track) });
      if (result.track) await player.play([result.track]);
    } catch (cause) {
      error = errorMessage(cause);
    }
  }
</script>

<Notice message={error} dismiss={() => (error = '')} />
<div class="discovery-tracks">
  {#each tracks as track (refKey(track.ref))}<div
      class="discovery-track"
      class:in-library={!!basket.localID(track)}
    >
      <input
        type="checkbox"
        aria-label={`Select ${track.title}`}
        checked={basket.has(track)}
        disabled={checking || !!basket.localID(track)}
        onchange={() => basket.toggle(track)}
      /><Cover
        name={track.album || track.title}
        artwork={track.artworkUrl}
        small
      />
      <div class="track-info">
        <strong>{track.title}</strong><span>{track.artist} · {track.album}</span
        >
      </div>
      <span class="track-duration muted">{duration(track.durationSeconds)}</span
      >{#if basket.localID(track)}<span class="library-badge"
          ><Check size={12} /> In library</span
        ><button
          class="icon-button"
          aria-label={`Play ${track.title}`}
          onclick={() => play(track)}><Play size={16} /></button
        >{:else}<button
          class="recording-button"
          aria-label={`Choose recording for ${track.title}`}
          onclick={() => (choosing = track)}
          disabled={checking}
          ><SlidersHorizontal size={14} /><span
            >{basket.sources[refKey(track.ref)]
              ? 'Selected'
              : 'Recording'}</span
          ></button
        >{/if}
    </div>{/each}
</div>
{#if choosing}<SourcePicker
    track={choosing}
    close={() => (choosing = undefined)}
    choose={(candidate) => {
      if (choosing) basket.choose(choosing, candidate);
      choosing = undefined;
    }}
  />{/if}
