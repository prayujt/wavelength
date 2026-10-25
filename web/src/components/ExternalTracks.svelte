<script lang="ts">
  import { Check, Download, Clock3, Play } from '@lucide/svelte';
  import type { ExternalTrack } from '../gen/wavelength/v1/ingestion_pb';
  import type { PlayerState } from '../lib/player.svelte';
  import { DownloadSelection, refKey } from '../lib/downloads.svelte';
  import { api } from '../lib/session.svelte';
  import { go } from '../lib/navigation';
  import { errorMessage } from '../lib/api';
  import { duration } from '../lib/format';
  import SongCredits from './SongCredits.svelte';
  import Cover from './Cover.svelte';
  import Notice from './Notice.svelte';
  import TrackMenu from './TrackMenu.svelte';
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
  let error = $state('');
  async function act(track: ExternalTrack) {
    error = '';
    try {
      if (basket.localID(track)) {
        const result = await api.library.getTrack({
          id: basket.localID(track),
        });
        if (result.track) await player.play([result.track]);
      } else if (basket.queued[refKey(track.ref)])
        go(`imports/${encodeURIComponent(basket.queued[refKey(track.ref)])}`);
      else await basket.download([track]);
    } catch (cause) {
      error = errorMessage(cause);
    }
  }
</script>

<Notice message={error} dismiss={() => (error = '')} />
<div class="discovery-tracks">
  {#each tracks as track (refKey(track.ref))}
    {@const owned = !!basket.localID(track)}
    {@const queued = basket.queued[refKey(track.ref)]}
    <div class="discovery-track" class:in-library={owned}>
      <input
        type="checkbox"
        aria-label={`Select ${track.title}`}
        checked={basket.has(track)}
        disabled={checking || owned || !!queued}
        onchange={() => basket.toggle(track)}
      />
      <div class="track-artwork">
        <Cover
          name={track.album || track.title}
          artwork={track.artworkUrl}
          small
        />
        <button
          class="cover-action"
          disabled={checking}
          aria-label={`${owned ? 'Play' : queued ? 'View download for' : 'Download'} ${track.title}`}
          title={owned
            ? 'Play song'
            : queued
              ? 'View download'
              : 'Download song'}
          onclick={() => act(track)}
          >{#if owned}<Play
              size={15}
              fill="currentColor"
            />{:else if queued}<Clock3 size={15} />{:else}<Download
              size={15}
            />{/if}</button
        >
      </div>
      <div class="track-info">
        <strong>{track.title}</strong><SongCredits {track} />
      </div>
      <span class="track-duration muted">{duration(track.durationSeconds)}</span
      >
      {#if owned}<span class="library-badge"
          ><Check size={12} /> In library</span
        >{:else}<button
          class="download-button"
          aria-label={`${queued ? 'View download for' : 'Download'} ${track.title}`}
          disabled={checking}
          onclick={() => act(track)}
          >{#if queued}<Clock3 size={14} /><span>Queued</span>{:else}<Download
              size={14}
            /><span>Download</span>{/if}</button
        >{/if}
      <TrackMenu
        id={basket.localID(track)}
        title={track.title}
        downloadOptions={() => {
          basket.add([track]);
          basket.reviewing = true;
        }}
      />
    </div>
  {/each}
</div>
