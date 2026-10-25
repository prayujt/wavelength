<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { ArrowRight, Library, Search } from '@lucide/svelte';
  import type { Album, Artist, Track } from '../gen/wavelength/v1/library_pb';
  import type { RealtimeState } from '../lib/realtime.svelte';
  import type { PlayerState } from '../lib/player.svelte';
  import { api } from '../lib/session.svelte';
  import { errorMessage } from '../lib/api';
  import { link } from '../lib/navigation';
  import Cover from '../components/Cover.svelte';
  import Notice from '../components/Notice.svelte';
  import TrackList from '../components/TrackList.svelte';

  let {
    player,
    add,
    initialTab = 'tracks',
    initialTrackID = '',
    realtime,
  }: {
    player: PlayerState;
    add: (track: Track) => void;
    initialTab?: string;
    initialTrackID?: string;
    realtime: RealtimeState;
  } = $props();
  let tab = $state('tracks');
  let query = $state('');
  let tracks = $state<Track[]>([]);
  let albums = $state<Album[]>([]);
  let artists = $state<Artist[]>([]);
  let total = $state(0);
  let offset = $state(0);
  let loading = $state(true);
  let error = $state('');
  const controller = new AbortController();
  let request = 0;
  const pageSize = 50;
  onMount(() => {
    tab = ['albums', 'artists'].includes(initialTab) ? initialTab : 'tracks';
    void load();
    return () => {
      ++request;
      controller.abort();
    };
  });
  $effect(() => {
    if (realtime.libraryVersion) untrack(() => void load(offset));
  });
  async function load(nextOffset = 0) {
    const generation = ++request;
    loading = true;
    error = '';
    try {
      const input = { query, offset: nextOffset, limit: pageSize };
      const options = { signal: controller.signal };
      const focused =
        initialTrackID && !query
          ? await api.library.getTrack({ id: initialTrackID }, options)
          : undefined;
      const result = focused
        ? {
            tracks: focused.track ? [focused.track] : [],
            total: focused.track ? 1 : 0,
          }
        : tab === 'tracks'
          ? await api.library.listTracks(input, options)
          : tab === 'albums'
            ? await api.library.listAlbums(input, options)
            : await api.library.listArtists(input, options);
      if (generation !== request) return;
      tracks = 'tracks' in result ? result.tracks : [];
      albums = 'albums' in result ? result.albums : [];
      artists = 'artists' in result ? result.artists : [];
      total = result.total;
      offset = nextOffset;
    } catch (cause) {
      if (generation === request && !controller.signal.aborted)
        error = errorMessage(cause);
    } finally {
      if (generation === request) loading = false;
    }
  }
</script>

<header class="page-heading">
  <div>
    <h1>Your library</h1>
    <p class="subtitle">Music saved to your collection.</p>
  </div>
  <a class="button primary" href="#discover"
    >Discover music <ArrowRight size={14} /></a
  >
</header>
<div class="library-toolbar">
  <div class="tabs" aria-label="Library category">
    {#each [['tracks', 'Songs'], ['albums', 'Albums'], ['artists', 'Artists']] as [value, label]}<a
        href={link('library', value === 'tracks' ? {} : { tab: value })}
        class:active={tab === value}
        aria-current={tab === value ? 'page' : undefined}>{label}</a
      >{/each}
  </div>
  <form
    class="search-box compact-search"
    onsubmit={(event) => {
      event.preventDefault();
      void load();
    }}
  >
    <Search size={15} /><input
      aria-label="Search your library"
      placeholder="Filter library…"
      bind:value={query}
    />
  </form>
</div>
<Notice message={error} />
{#if initialTrackID}<a class="back-link" href="#library">Show all songs</a>{/if}
{#if loading}<div class="loading-state" role="status">
    <span class="spinner"></span> Loading library…
  </div>
{:else if !error && !total}<div class="empty-state">
    <Library size={27} />
    <h2>{query ? 'No results' : 'No music yet'}</h2>
    <p>
      {query
        ? 'Try another song, artist, or album.'
        : 'Search for music in Discover and download it to your library.'}
    </p>
    {#if !query}<a class="button primary" href="#discover"
        >Discover music <ArrowRight size={14} /></a
      >{/if}
  </div>
{:else if tracks.length}<TrackList {tracks} {player} {add} />
{:else}<div class="card-grid">
    {#each albums as album}<a
        class="music-card"
        href={link(`album/${encodeURIComponent(album.id)}`)}
        ><Cover name={album.name} artwork={album.artworkPath} /><strong
          >{album.name}</strong
        ><span
          >{album.artist} · {album.year || `${album.trackCount} songs`}</span
        ></a
      >{/each}{#each artists as artist}<a
        class="music-card"
        href={link(`artist/${encodeURIComponent(artist.id)}`)}
        ><Cover name={artist.name} artwork={artist.artworkPath} /><strong
          >{artist.name}</strong
        ><span
          >{artist.albumCount}
          {artist.albumCount === 1 ? 'album' : 'albums'}</span
        ></a
      >{/each}
  </div>{/if}
{#if !loading && total}<div class="pagination">
    <span class="muted"
      >{total}
      {total === 1
        ? tab === 'tracks'
          ? 'song'
          : tab.slice(0, -1)
        : tab === 'tracks'
          ? 'songs'
          : tab}</span
    >{#if total > pageSize}<button
        class="button secondary compact"
        disabled={offset === 0}
        onclick={() => load(offset - pageSize)}>Previous</button
      ><span class="muted"
        >{offset + 1}–{Math.min(offset + pageSize, total)}</span
      ><button
        class="button secondary compact"
        disabled={offset + pageSize >= total}
        onclick={() => load(offset + pageSize)}>Next</button
      >{/if}
  </div>{/if}
