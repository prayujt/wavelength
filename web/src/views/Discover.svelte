<script lang="ts">
  import { onMount, tick, untrack } from 'svelte';
  import { Search } from '@lucide/svelte';
  import type { DiscoveryState } from '../lib/discovery.svelte';
  import type { RealtimeState } from '../lib/realtime.svelte';
  import type { PlayerState } from '../lib/player.svelte';
  import { DownloadSelection } from '../lib/downloads.svelte';
  import { api } from '../lib/session.svelte';
  import { errorMessage } from '../lib/api';
  import { link } from '../lib/navigation';
  import Cover from '../components/Cover.svelte';
  import Notice from '../components/Notice.svelte';
  import ExternalTracks from '../components/ExternalTracks.svelte';
  import LibraryStatus from '../components/LibraryStatus.svelte';
  let {
    basket,
    player,
    initialQuery = '',
    initialCategory = 'all',
    discovery,
    realtime,
  }: {
    basket: DownloadSelection;
    player: PlayerState;
    initialQuery?: string;
    initialCategory?: string;
    discovery: DiscoveryState;
    realtime: RealtimeState;
  } = $props();
  const context = $derived({
    q: discovery.searched,
    category: discovery.category,
  });
  let loading = $state(false);
  let checking = $state(false);
  let error = $state('');
  let controller: AbortController | undefined;
  const saved = $derived(
    discovery.results?.tracks.filter((track) => basket.localID(track)).length ||
      0,
  );
  onMount(() => {
    discovery.category = ['all', 'tracks', 'releases', 'artists'].includes(
      initialCategory,
    )
      ? initialCategory
      : 'all';
    if (initialQuery && initialQuery !== discovery.searched) {
      discovery.query = initialQuery;
      void search(false);
    } else if (!discovery.results && discovery.query.trim()) void search(false);
    else void tick().then(() => window.scrollTo(0, discovery.scroll));
    return () => {
      discovery.scroll = window.scrollY;
      controller?.abort();
    };
  });
  $effect(() => {
    if (realtime.resetVersion)
      untrack(() => {
        if (discovery.results)
          void basket
            .lookup(discovery.results.tracks)
            .catch((cause) => (error = errorMessage(cause)));
      });
  });
  function syncAddress() {
    history.replaceState(
      history.state,
      '',
      link('discover', { q: discovery.searched, category: discovery.category }),
    );
  }
  function category(value: string) {
    discovery.category = value;
    syncAddress();
  }
  async function search(resetCategory = true) {
    if (!discovery.query.trim()) return;
    controller?.abort();
    controller = new AbortController();
    const current = controller;
    const term = discovery.query.trim();
    loading = true;
    error = '';
    try {
      const result = await api.discovery.search(
        { query: term, limit: 15 },
        { signal: current.signal },
      );
      if (current.signal.aborted) return;
      discovery.results = result;
      discovery.searched = term;
      if (resetCategory) discovery.category = 'all';
      discovery.scroll = 0;
      syncAddress();
      loading = false;
      checking = true;
      await basket.lookup(result.tracks, current.signal);
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
    bind:value={discovery.query}
  /><button
    class="button primary compact"
    disabled={loading || !discovery.query.trim()}>Search</button
  >
</form>
<Notice message={error} />
{#if loading}<div class="loading-state" role="status">
    <span class="spinner"></span> Searching music…
  </div>{:else if !discovery.results}<div class="empty-state">
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
          class:active={discovery.category === value}
          aria-pressed={discovery.category === value}
          onclick={() => category(value)}>{label}</button
        >{/each}
    </div>
    {#if checking}<span class="muted small-text">Checking library…</span>{/if}
  </div>
  {#if ['all', 'artists'].includes(discovery.category) && discovery.results.artists.length}<section
      class="result-section"
    >
      <h2>Artists</h2>
      <div class="artist-chips">
        {#each discovery.results.artists as item}<a
            class="artist-chip"
            href={link('discover/artist', {
              ...context,
              provider: item.ref?.provider || '',
              id: item.ref?.id || '',
            })}
            ><Cover name={item.name} artwork={item.artworkUrl} small /><strong
              >{item.name}</strong
            ></a
          >{/each}
      </div>
    </section>{/if}
  {#if ['all', 'tracks'].includes(discovery.category) && discovery.results.tracks.length}<section
      class="result-section"
    >
      <div class="section-heading">
        <h2>Songs</h2>
        <div class="entity-actions">
          <LibraryStatus
            {saved}
            total={discovery.results.tracks.length}
            {checking}
          />
          <button
            class="text-button accent"
            disabled={checking || saved === discovery.results.tracks.length}
            onclick={() => basket.add(discovery.results?.tracks || [])}
            >Select missing songs</button
          >
        </div>
      </div>
      <ExternalTracks
        tracks={discovery.results.tracks}
        {basket}
        {player}
        {checking}
      />
    </section>{/if}
  {#if ['all', 'releases'].includes(discovery.category) && discovery.results.releases.length}<section
      class="result-section"
    >
      <div class="section-heading">
        <h2>Albums</h2>
        {#if discovery.category === 'all' && discovery.results.releases.length > 6}<button
            class="text-button accent"
            onclick={() => category('releases')}>Show all albums</button
          >{/if}
      </div>
      <div class="card-grid">
        {#each discovery.category === 'all' ? discovery.results.releases.slice(0, 6) : discovery.results.releases as item}<a
            class="music-card"
            href={link('discover/release', {
              ...context,
              provider: item.ref?.provider || '',
              id: item.ref?.id || '',
            })}
            ><Cover name={item.title} artwork={item.artworkUrl} /><strong
              >{item.title}</strong
            ><span>{item.artist}</span></a
          >{/each}
      </div>
    </section>{/if}
  {#if !discovery.results.tracks.length && !discovery.results.artists.length && !discovery.results.releases.length}<div
      class="empty-state"
    >
      <h2>No results</h2>
      <p>Try another artist, album, or song.</p>
    </div>{/if}
{/if}
