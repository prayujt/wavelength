<script lang="ts">
  import { Clock3, ListPlus, Play, Plus, X } from '@lucide/svelte';
  import type { Track } from '../gen/wavelength/v1/library_pb';
  import type { PlayerState } from '../lib/player.svelte';
  import { duration } from '../lib/format';
  import { link } from '../lib/navigation';
  import { reveal } from '../lib/motion';
  import SongCredits from './SongCredits.svelte';
  import Cover from './Cover.svelte';
  import TrackMenu from './TrackMenu.svelte';
  let {
    tracks,
    player,
    add,
    remove,
    addedIDs = new Set<string>(),
  }: {
    tracks: Track[];
    player: PlayerState;
    add?: (track: Track) => void;
    remove?: (index: number) => void;
    addedIDs?: Set<string>;
  } = $props();
  const rows = $derived.by(() => {
    const occurrences = new Map<string, number>();
    return tracks.map((track) => {
      const occurrence = occurrences.get(track.id) || 0;
      occurrences.set(track.id, occurrence + 1);
      return { track, key: `${track.id}-${occurrence}` };
    });
  });
</script>

<div
  class="track-list"
  style:--action-count={2 + Number(!!add) + Number(!!remove)}
>
  <div class="track-row table-heading">
    <span>#</span><span>Title</span><span class="album-column">Album</span><span
      class="track-duration"><Clock3 size={14} aria-label="Duration" /></span
    ><span></span>
  </div>
  {#each rows as { track, key }, index (key)}
    <div
      use:reveal={addedIDs.has(track.id)}
      data-track-id={track.id}
      class:current={player.track?.id === track.id}
      class="track-row"
    >
      <button
        class="track-play icon-button"
        aria-label={`Play ${track.title}`}
        onclick={() => player.play(tracks, index)}
        ><span>{index + 1}</span><Play size={15} fill="currentColor" /></button
      >
      <div class="track-title">
        <Cover
          name={track.album || track.title}
          artwork={track.artworkPath}
          small
        />
        <div>
          <button
            class="text-button song-title"
            onclick={() => player.play(tracks, index)}>{track.title}</button
          ><SongCredits {track} mobileAlbum />
        </div>
      </div>
      {#if track.album}<a
          class="album-column muted truncate"
          href={link(`album/${encodeURIComponent(track.albumId)}`)}
          >{track.album}</a
        >{:else}<span class="album-column muted">—</span>{/if}
      <span class="track-duration muted">{duration(track.durationSeconds)}</span
      >
      <div class="row-actions">
        <TrackMenu id={track.id} title={track.title} />
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
