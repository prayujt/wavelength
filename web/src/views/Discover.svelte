<script lang="ts">
  import { onMount } from 'svelte';
  import { Search } from '@lucide/svelte';
  import type { SearchMusicResponse } from '../gen/wavelength/v1/ingestion_pb';
  import type { PlayerState } from '../lib/player.svelte';
  import { DownloadSelection } from '../lib/downloads.svelte';
  import { api } from '../lib/session.svelte';
  import { errorMessage } from '../lib/api';
  import { link } from '../lib/navigation';
  import Cover from '../components/Cover.svelte';
  import Notice from '../components/Notice.svelte';
  import ExternalTracks from '../components/ExternalTracks.svelte';
  let {
    basket,
    player,
    initialQuery = '',
  }: {
    basket: DownloadSelection;
    player: PlayerState;
    initialQuery?: string;
  } = $props();
  let query = $state('');
  let results = $state<SearchMusicResponse>();
  let category = $state('all');
  let loading = $state(false);
  let checking = $state(false);
  let error = $state('');
  let controller: AbortController | undefined;
  onMount(() => {
    query = initialQuery;
    if (query) void search();
    return () => controller?.abort();
  });
  async function search() {
    if (!query.trim()) return;
    controller?.abort();
    controller = new AbortController();
    const current = controller;
    loading = true;
    error = '';
    try {
      results = await api.discovery.search(
        { query: query.trim(), limit: 15 },
        { signal: current.signal },
      );
      category = 'all';
      loading = false;
      checking = true;
      await basket.lookup(results.tracks, current.signal);
    } catch (cause) {
      if (!current.signal.aborted) error = errorMessage(cause);
    } finally {
      if (current === controller) {
        loading = false;
        checking = false;
      }
    }
  }
</script>

<header class="page-heading">
  <div>
    <h1>Discover</h1>
    <p class="subtitle">
      Search for music, choose songs, and download them to your library.
    </p>
  </div>
  <a class="button secondary" href="#imports">View downloads</a>
</header>
<form
  class="search-box"
  onsubmit={(event) => {
    event.preventDefault();
    void search();
  }}
>
  <Search size={17} /><input
    aria-label="Search for new music"
    placeholder="Search artists, albums, or songs"
    bind:value={query}
  /><button class="button primary compact" disabled={loading || !query.trim()}
    >Search</button
  >
</form>
<Notice message={error} />
{#if loading}<div class="loading-state" role="status">
    <span class="spinner"></span> Searching music…
  </div>{:else if !results}<div class="empty-state">
    <Search size={27} />
    <h2>Find music to download</h2>
    <p>
      Search an artist or song above. Open an album to choose individual songs
      or select the whole album.
    </p>
  </div>{:else}
  <div class="section-heading">
    <div class="tabs" aria-label="Discovery category">
      {#each [['all', 'All'], ['tracks', 'Songs'], ['releases', 'Albums'], ['artists', 'Artists']] as [value, label]}<button
          class:active={category === value}
          aria-pressed={category === value}
          onclick={() => (category = value)}>{label}</button
        >{/each}
    </div>
    {#if checking}<span class="muted small-text">Checking library…</span>{/if}
  </div>
  {#if ['all', 'artists'].includes(category) && results.artists.length}<section
      class="result-section"
    >
      <h2>Artists</h2>
      <div class="artist-chips">
        {#each results.artists as item}<a
            class="artist-chip"
            href={link('discover/artist', {
              provider: item.ref?.provider || '',
              id: item.ref?.id || '',
            })}><Cover name={item.name} small /><strong>{item.name}</strong></a
          >{/each}
      </div>
    </section>{/if}
  {#if ['all', 'tracks'].includes(category) && results.tracks.length}<section
      class="result-section"
    >
      <div class="section-heading">
        <h2>Songs</h2>
        <button
          class="text-button accent"
          disabled={checking}
          onclick={() => basket.add(results?.tracks || [])}
          >Select missing songs</button
        >
      </div>
      <ExternalTracks tracks={results.tracks} {basket} {player} {checking} />
    </section>{/if}
  {#if ['all', 'releases'].includes(category) && results.releases.length}<section
      class="result-section"
    >
      <div class="section-heading">
        <h2>Albums</h2>
        {#if category === 'all' && results.releases.length > 6}<button
            class="text-button accent"
            onclick={() => (category = 'releases')}>Show all albums</button
          >{/if}
      </div>
      <div class="card-grid">
        {#each category === 'all' ? results.releases.slice(0, 6) : results.releases as item}<a
            class="music-card"
            href={link('discover/release', {
              provider: item.ref?.provider || '',
              id: item.ref?.id || '',
            })}
            ><Cover name={item.title} artwork={item.artworkUrl} /><strong
              >{item.title}</strong
            ><span>{item.artist}</span></a
          >{/each}
      </div>
    </section>{/if}
  {#if !results.tracks.length && !results.artists.length && !results.releases.length}<div
      class="empty-state"
    >
      <h2>No results</h2>
      <p>Try another artist, album, or song.</p>
    </div>{/if}
{/if}
