<script lang="ts">
  import { onMount } from 'svelte';
  import {
    AudioLines,
    ChevronDown,
    ChevronRight,
    Compass,
    Disc3,
    Download,
    ListMusic,
    LogOut,
    Moon,
    Music2,
    PanelLeftClose,
    PanelLeftOpen,
    Search,
    Sun,
    Users,
  } from '@lucide/svelte';
  import type { Track } from '../gen/wavelength/v1/library_pb';
  import { api, session } from '../lib/session.svelte';
  import { PlayerState } from '../lib/player.svelte';
  import { DownloadSelection } from '../lib/downloads.svelte';
  import { DiscoveryState } from '../lib/discovery.svelte';
  import { RealtimeState } from '../lib/realtime.svelte';
  import { errorMessage } from '../lib/api';
  import { duration, initials } from '../lib/format';
  import { go, link, readRoute } from '../lib/navigation';
  import { theme } from '../lib/theme.svelte';
  import LibraryView from '../views/Library.svelte';
  import LibraryDetail from '../views/LibraryDetail.svelte';
  import Discover from '../views/Discover.svelte';
  import DiscoveryDetail from '../views/DiscoveryDetail.svelte';
  import Imports from '../views/Imports.svelte';
  import Playlists from '../views/Playlists.svelte';
  import Notice from './Notice.svelte';
  import Player from './Player.svelte';
  import PlaylistPicker from './PlaylistPicker.svelte';
  import CommandPalette from './CommandPalette.svelte';
  import DownloadReview from './DownloadReview.svelte';
  const player = new PlayerState();
  const basket = new DownloadSelection();
  const discovery = new DiscoveryState();
  const realtime = new RealtimeState();
  let address = $state(window.location.hash);
  const route = $derived(readRoute(address));
  let adding = $state<Track>();
  let error = $state('');
  let listener = $state(
    session.user?.name || session.user?.email || 'Listener',
  );
  let sidebar = $state(true);
  let searchOpen = $state(false);
  const shortcut = /Mac|iPhone|iPad/.test(navigator.platform)
    ? '⌘ K'
    : 'Ctrl K';
  const groups = [
    {
      label: 'Browse',
      items: [
        { label: 'Discover', page: 'discover', tab: '', icon: Compass },
        { label: 'Downloads', page: 'imports', tab: '', icon: Download },
      ],
    },
    {
      label: 'Your library',
      items: [
        { label: 'Songs', page: 'library', tab: '', icon: Music2 },
        { label: 'Albums', page: 'library', tab: 'albums', icon: Disc3 },
        { label: 'Artists', page: 'library', tab: 'artists', icon: Users },
        { label: 'Playlists', page: 'playlists', tab: '', icon: ListMusic },
      ],
    },
  ];
  const pageName = $derived(
    route.page === 'artist'
      ? 'Artist'
      : route.page === 'album'
        ? 'Album'
        : route.page === 'discover'
          ? 'Discover'
          : route.page === 'imports'
            ? 'Downloads'
            : route.page === 'playlists'
              ? 'Playlists'
              : 'Library',
  );
  const libraryTab = $derived(route.query.get('tab') || '');
  function active(page: string, tab: string) {
    if (page === 'library' && route.page === 'artist') return tab === 'artists';
    if (page === 'library' && route.page === 'album') return tab === 'albums';
    return route.page === page && (page !== 'library' || libraryTab === tab);
  }
  function closeMobile() {
    if (matchMedia('(max-width: 760px)').matches) sidebar = false;
  }
  function search() {
    basket.reviewing = false;
    searchOpen = true;
  }
  function keyboard(event: KeyboardEvent) {
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
      event.preventDefault();
      if (!searchOpen && document.querySelector('dialog[open]')) return;
      searchOpen = !searchOpen;
    }
  }
  onMount(() => {
    const stopRealtime = realtime.start(basket);
    if (matchMedia('(max-width: 760px)').matches) sidebar = false;
    const sync = () => {
      address = window.location.hash;
      closeMobile();
    };
    window.addEventListener('hashchange', sync);
    const controller = new AbortController();
    api.user
      .getCurrentUser({}, { signal: controller.signal })
      .then(({ user }) => {
        if (user) listener = user.name || user.email || 'Listener';
      })
      .catch((cause) => {
        if (!controller.signal.aborted) error = errorMessage(cause);
      });
    return () => {
      stopRealtime();
      controller.abort();
      window.removeEventListener('hashchange', sync);
    };
  });
  async function logout() {
    try {
      await session.logout();
    } catch (cause) {
      error = errorMessage(cause);
    }
  }
</script>

<svelte:window onkeydown={keyboard} />
<div class="app-shell" class:sidebar-collapsed={!sidebar}>
  {#if sidebar}<button
      class="navigation-backdrop"
      aria-label="Close navigation"
      onclick={() => (sidebar = false)}
    ></button>
    <aside class="sidebar">
      <div class="sidebar-heading">
        <a class="workspace-brand" href="#library" onclick={closeMobile}
          ><AudioLines size={24} /><strong>Wavelength</strong></a
        ><button
          class="icon-button"
          aria-label="Hide sidebar"
          onclick={() => (sidebar = false)}><PanelLeftClose size={16} /></button
        >
      </div>
      <button class="sidebar-search" onclick={search}
        ><Search size={15} /><span>Search music</span><kbd>{shortcut}</kbd
        ></button
      >
      <nav class="sidebar-nav" aria-label="Main navigation">
        {#each groups as group}<div class="nav-group">
            <p class="nav-section-label">{group.label}</p>
            {#each group.items as item}<a
                href={link(item.page, item.tab ? { tab: item.tab } : {})}
                class:active={active(item.page, item.tab)}
                aria-current={active(item.page, item.tab) ? 'page' : undefined}
                onclick={closeMobile}
                ><item.icon size={17} strokeWidth={1.7} /><span
                  >{item.label}</span
                ></a
              >{/each}
          </div>{/each}
      </nav>
      <div class="sidebar-bottom">
        <div class="account">
          <span class="avatar">{initials(listener)}</span>
          <div><strong>{listener}</strong><span>Signed in</span></div>
          <button
            class="icon-button"
            aria-label="Sign out"
            title="Sign out"
            disabled={session.signingOut}
            onclick={logout}><LogOut size={16} /></button
          >
        </div>
      </div>
    </aside>{/if}
  <main class="main">
    <header class="topbar">
      <div class="breadcrumb">
        {#if !sidebar}<button
            class="icon-button"
            aria-label="Show sidebar"
            onclick={() => (sidebar = true)}><PanelLeftOpen size={17} /></button
          >{/if}<a href="#library" class="breadcrumb-workspace"
          ><AudioLines size={15} /> Wavelength</a
        ><ChevronRight size={12} /><span class="breadcrumb-current"
          >{pageName}</span
        >
      </div>
      <div class="header-actions">
        <button
          class="icon-button"
          aria-label="Open search"
          title={`Search (${shortcut})`}
          onclick={search}><Search size={16} /></button
        ><button
          class="icon-button"
          aria-label="Toggle color theme"
          onclick={() => theme.toggle()}
          >{#if theme.dark}<Sun size={16} />{:else}<Moon
              size={16}
            />{/if}</button
        ><button
          class="icon-button mobile-account"
          aria-label="Sign out"
          disabled={session.signingOut}
          onclick={logout}><LogOut size={16} /></button
        >
      </div>
    </header>
    <div class="content" class:downloads-page={route.page === 'imports'}>
      <Notice
        message={error}
        dismiss={() => (error = '')}
      />{#key address}{#if route.page === 'library'}<LibraryView
            {player}
            add={(track) => (adding = track)}
            initialTab={libraryTab}
            initialTrackID={route.query.get('track') || ''}
            {realtime}
          />{:else if route.page === 'artist' || route.page === 'album'}<LibraryDetail
            kind={route.page}
            id={route.id}
            {player}
            add={(track) => (adding = track)}
            {realtime}
          />{:else if route.page === 'discover' && ['artist', 'release'].includes(route.id)}<DiscoveryDetail
            kind={route.id}
            provider={route.query.get('provider') || ''}
            id={route.query.get('id') || ''}
            {basket}
            {player}
            {realtime}
            searchContext={{
              q: route.query.get('q') || '',
              category: route.query.get('category') || 'all',
            }}
          />{:else if route.page === 'discover'}<Discover
            {basket}
            {player}
            initialQuery={route.query.get('q') || ''}
            initialCategory={route.query.get('category') || 'all'}
            {discovery}
            {realtime}
          />{:else if route.page === 'imports'}<Imports
            {player}
            initialID={route.id}
            {realtime}
          />{:else if route.page === 'playlists'}<Playlists
            {player}
            add={(track) => (adding = track)}
          />{:else}<div class="empty-state">
            <h1>Page not found</h1>
            <a class="button secondary" href="#library">Open library</a>
          </div>{/if}{/key}
      {#if basket.tracks.length}<div class="selection-bar">
          <div>
            <strong
              >{basket.tracks.length}
              {basket.tracks.length === 1 ? 'song' : 'songs'} selected</strong
            ><span>{duration(basket.duration)} · Ready for review</span>
          </div>
          <div class="entity-actions">
            <button
              class="button secondary compact"
              onclick={() => basket.clear()}>Clear</button
            ><button
              class="button primary"
              onclick={() => (basket.reviewing = true)}
              >Review download <ChevronRight size={14} /></button
            >
          </div>
        </div>{/if}
    </div>
  </main>
  <Player {player} />
</div>
{#if adding}<PlaylistPicker
    track={adding}
    close={() => (adding = undefined)}
  />{/if}
{#if searchOpen}<CommandPalette
    {player}
    {basket}
    {realtime}
    openDiscover={(query) => {
      discovery.query = query;
      discovery.category = 'all';
      go('discover', { q: query, category: 'all' });
    }}
    close={() => (searchOpen = false)}
  />{/if}
{#if basket.reviewing}<DownloadReview
    {basket}
    started={(job) => go(`imports/${encodeURIComponent(job.id)}`)}
  />{/if}
