<script lang="ts">
  import { Clock3, Download, LoaderCircle, Play } from '@lucide/svelte';
  import type { MusicRelease } from '../gen/wavelength/v1/ingestion_pb';
  import type { DownloadSelection } from '../lib/downloads.svelte';
  import type { PlayerState } from '../lib/player.svelte';
  import { api } from '../lib/session.svelte';
  import { errorMessage } from '../lib/api';
  import { go, link } from '../lib/navigation';
  import { refKey } from '../lib/downloads.svelte';
  import Cover from './Cover.svelte';
  import LibraryStatus from './LibraryStatus.svelte';
  let {
    album,
    basket,
    player,
    context,
    checking = false,
  }: {
    album: MusicRelease;
    basket: DownloadSelection;
    player: PlayerState;
    context: Record<string, string>;
    checking?: boolean;
  } = $props();
  const release = $derived(basket.release(album));
  const saved = $derived(
    release?.tracks.filter((track) => basket.localID(track)).length || 0,
  );
  const full = $derived(
    !!release?.tracks.length && saved === release.tracks.length,
  );
  const remaining = $derived(
    release?.tracks.filter((track) => !basket.localID(track)) || [],
  );
  const queued = $derived(
    remaining.length > 0 &&
      remaining.every((track) => basket.queued[refKey(track.ref)]),
  );
  const href = $derived(
    link('discover/release', {
      ...context,
      provider: album.ref?.provider || '',
      id: album.ref?.id || '',
    }),
  );
  let busy = $state(false);
  async function act() {
    if (!release || busy) return;
    busy = true;
    try {
      if (full) {
        const found = await Promise.all(
          release.tracks.map((track) =>
            api.library.getTrack({ id: basket.localID(track) }),
          ),
        );
        await player.play(
          found.flatMap((result) => (result.track ? [result.track] : [])),
        );
      } else if (queued)
        go(
          `imports/${encodeURIComponent(basket.queued[refKey(remaining[0].ref)])}`,
        );
      else await basket.download(release.tracks);
    } catch (cause) {
      player.error = errorMessage(cause);
    } finally {
      busy = false;
    }
  }
</script>

<div class="music-card external-release">
  <div class="card-artwork">
    <a {href} aria-label={`Open ${album.title}`}
      ><Cover name={album.title} artwork={album.artworkUrl} /></a
    >
    <button
      class="cover-action"
      aria-label={`${full ? 'Play' : queued ? 'View download for' : 'Download'} album ${album.title}`}
      title={full
        ? 'Play album'
        : queued
          ? 'View download'
          : 'Download missing songs'}
      disabled={checking || !release || busy}
      onclick={act}
    >
      {#if busy}<LoaderCircle size={19} class="spinner" />{:else if full}<Play
          size={19}
          fill="currentColor"
        />{:else if queued}<Clock3 size={19} />{:else}<Download
          size={19}
        />{/if}
    </button>
  </div>
  <a {href}><strong>{album.title}</strong></a>
  <span>{album.artist}</span>
  {#if release}<LibraryStatus
      {saved}
      total={release.tracks.length}
      {checking}
    />{:else}<span class="library-summary"
      >{checking ? 'Checking library…' : 'Open album to check library'}</span
    >{/if}
</div>
