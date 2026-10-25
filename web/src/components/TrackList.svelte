<script lang="ts">
  import { Clock3, ListPlus, Play, Plus, X } from '@lucide/svelte';
  import type { Track } from '../gen/wavelength/v1/library_pb';
  import type { PlayerState } from '../lib/player.svelte';
  import { duration } from '../lib/format';
  import Cover from './Cover.svelte';
  let {
    tracks,
    player,
    add,
    remove,
  }: {
    tracks: Track[];
    player: PlayerState;
    add?: (track: Track) => void;
    remove?: (index: number) => void;
  } = $props();
</script>

<div class="track-list">
  <div class="track-row table-heading">
    <span>#</span><span>Title</span><span class="album-column">Album</span><span
      class="track-duration"><Clock3 size={14} aria-label="Duration" /></span
    ><span></span>
  </div>
  {#each tracks as track, index (`${track.id}-${index}`)}
    <div class:current={player.track?.id === track.id} class="track-row">
      <button
        class="track-play icon-button"
        aria-label={`Play ${track.title}`}
        onclick={() => player.play(tracks, index)}
        ><span>{index + 1}</span><Play size={15} fill="currentColor" /></button
      >
      <div class="track-title">
        <Cover name={track.album || track.title} small />
        <div>
          <button
            class="text-button song-title"
            onclick={() => player.play(tracks, index)}>{track.title}</button
          ><span class="muted">{track.artist || 'Unknown artist'}</span>
        </div>
      </div>
      <span class="album-column muted truncate">{track.album || '—'}</span>
      <span class="track-duration muted">{duration(track.durationSeconds)}</span
      >
      <div class="row-actions">
        <button
          class="icon-button"
          aria-label={`Queue ${track.title}`}
          title="Add to queue"
          onclick={() => player.enqueue(track)}><ListPlus size={17} /></button
        >{#if add}<button
            class="icon-button"
            aria-label={`Add ${track.title} to playlist`}
            onclick={() => add(track)}><Plus size={17} /></button
          >{/if}{#if remove}<button
            class="icon-button"
            aria-label={`Remove ${track.title}`}
            onclick={() => remove(index)}><X size={16} /></button
          >{/if}
      </div>
    </div>
  {/each}
</div>
