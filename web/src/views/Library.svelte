<script lang="ts">
  import { onMount } from 'svelte';
  import { ArrowLeft, ArrowRight, Library, Play, Search } from '@lucide/svelte';
  import type { Album, Artist, Track } from '../gen/wavelength/v1/library_pb';
  import type { PlayerState } from '../lib/player.svelte';
  import { api } from '../lib/session.svelte';
  import { errorMessage } from '../lib/api';
  import Cover from '../components/Cover.svelte';
  import Notice from '../components/Notice.svelte';
  import TrackList from '../components/TrackList.svelte';

  let {
    player,
    add,
    discover,
  }: {
    player: PlayerState;
    add: (track: Track) => void;
    discover: () => void;
  } = $props();
  let tab = $state<'tracks' | 'albums' | 'artists'>('tracks');
  let query = $state('');
  let tracks = $state<Track[]>([]);
  let albums = $state<Album[]>([]);
  let artists = $state<Artist[]>([]);
  let total = $state(0);
  let offset = $state(0);
  let loading = $state(true);
  let error = $state('');
  let detail = $state('');
  let request = 0;
  const pageSize = 50;
  onMount(() => {
    void load();
    return () => {
      ++request;
    };
  });

  async function load(nextOffset = 0) {
    const id = ++request;
    loading = true;
    error = '';
    detail = '';
    try {
      const input = { query, offset: nextOffset, limit: pageSize };
      const result =
        tab === 'tracks'
          ? await api.library.listTracks(input)
          : tab === 'albums'
            ? await api.library.listAlbums(input)
            : await api.library.listArtists(input);
      if (id !== request) return;
      tracks = 'tracks' in result ? result.tracks : [];
      albums = 'albums' in result ? result.albums : [];
      artists = 'artists' in result ? result.artists : [];
      total = result.total;
      offset = nextOffset;
    } catch (cause) {
      if (id === request) error = errorMessage(cause);
    } finally {
      if (id === request) loading = false;
    }
  }
  async function openAlbum(album: Album) {
    const id = ++request;
    loading = true;
    error = '';
    try {
      const result = await api.library.getAlbum({ id: album.id });
      if (id !== request) return;
      tracks = result.tracks;
      albums = [];
      artists = [];
      detail = album.name;
      total = tracks.length;
    } catch (cause) {
      if (id === request) error = errorMessage(cause);
    } finally {
      if (id === request) loading = false;
    }
  }
  async function openArtist(artist: Artist) {
    const id = ++request;
    loading = true;
    error = '';
    try {
      const result = await api.library.getArtist({ id: artist.id });
      if (id !== request) return;
      albums = result.albums;
      artists = [];
      tracks = [];
      detail = artist.name;
      total = albums.length;
    } catch (cause) {
      if (id === request) error = errorMessage(cause);
    } finally {
      if (id === request) loading = false;
    }
  }
</script>

<header class="page-heading">
  <div>
    <p class="eyebrow">Made for listening</p>
    <h1>Your library<span class="accent">.</span></h1>
    <p class="subtitle">Good music. A little space to make it yours.</p>
  </div>
  <button class="button primary" onclick={discover}
    >Discover music <ArrowRight size={16} /></button
  >
</header>
<form
  class="search-box"
  onsubmit={(event) => {
    event.preventDefault();
    void load();
  }}
>
  <Search size={19} /><input
    aria-label="Search your library"
    placeholder="Search your library"
    bind:value={query}
  /><kbd>↵</kbd>
</form>
<div class="section-heading">
  <div class="tabs" aria-label="Library category">
    {#each ['tracks', 'albums', 'artists'] as item}<button
        class:active={tab === item}
        aria-pressed={tab === item}
        onclick={() => {
          tab = item as typeof tab;
          void load();
        }}
        >{item === 'tracks'
          ? 'Songs'
          : item === 'albums'
            ? 'Albums'
            : 'Artists'}</button
      >{/each}
  </div>
  <span class="muted small-text"
    >{total}
    {detail
      ? total === 1
        ? 'item'
        : 'items'
      : tab === 'tracks'
        ? total === 1
          ? 'song'
          : 'songs'
        : total === 1
          ? tab.slice(0, -1)
          : tab}</span
  >
</div>
{#if detail}<div class="detail-heading">
    <button
      class="icon-button"
      aria-label="Back to library"
      onclick={() => load()}><ArrowLeft size={19} /></button
    >
    <h2>{detail}</h2>
    {#if tracks.length}<button
        class="button primary compact"
        onclick={() => player.play(tracks)}
        ><Play size={15} fill="currentColor" /> Play all</button
      >{/if}
  </div>{/if}
<Notice message={error} />
{#if loading}<div class="loading-state" role="status">
    <span class="spinner"></span> Finding your music…
  </div>
{:else if !error && !total}<div class="empty-state">
    <div class="empty-icon"><Library size={30} /></div>
    <h2>{query ? 'No matches this time' : 'Your collection starts here'}</h2>
    <p>
      {query
        ? 'Try another song, artist, or album.'
        : 'Find an artist you love, choose a few songs, and bring them home.'}
    </p>
    {#if !query}<button class="button primary" onclick={discover}
        >Find some music <ArrowRight size={16} /></button
      >{/if}
  </div>
{:else if tracks.length}<TrackList {tracks} {player} {add} />
{:else}<div class="card-grid">
    {#each albums as album}<button
        class="music-card"
        onclick={() => openAlbum(album)}
        ><Cover name={album.name} /><strong>{album.name}</strong><span
          >{album.artist || 'Unknown artist'} · {album.trackCount} songs</span
        ></button
      >{/each}{#each artists as artist}<button
        class="music-card"
        onclick={() => openArtist(artist)}
        ><Cover name={artist.name} /><strong>{artist.name}</strong><span
          >{artist.albumCount}
          {artist.albumCount === 1 ? 'album' : 'albums'}</span
        ></button
      >{/each}
  </div>{/if}
{#if !loading && !detail && total > pageSize}<div class="pagination">
    <button
      class="button secondary compact"
      disabled={offset === 0}
      onclick={() => load(offset - pageSize)}>Previous</button
    ><span class="muted"
      >{offset + 1}–{Math.min(offset + pageSize, total)} of {total}</span
    ><button
      class="button secondary compact"
      disabled={offset + pageSize >= total}
      onclick={() => load(offset + pageSize)}>Next</button
    >
  </div>{/if}
