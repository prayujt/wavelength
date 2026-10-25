<script lang="ts">
  import { onMount } from 'svelte';
  import {
    AudioLines,
    Compass,
    Download,
    Library,
    ListMusic,
    LogOut,
  } from '@lucide/svelte';
  import type { Track } from '../gen/wavelength/v1/library_pb';
  import { api, session } from '../lib/session.svelte';
  import { PlayerState } from '../lib/player.svelte';
  import { errorMessage } from '../lib/api';
  import { initials } from '../lib/format';
  import LibraryView from '../views/Library.svelte';
  import Discover from '../views/Discover.svelte';
  import Imports from '../views/Imports.svelte';
  import Playlists from '../views/Playlists.svelte';
  import Notice from './Notice.svelte';
  import Player from './Player.svelte';
  import PlaylistPicker from './PlaylistPicker.svelte';

  const navigation = [
    { id: 'library', label: 'Your library', icon: Library },
    { id: 'discover', label: 'Discover', icon: Compass },
    { id: 'imports', label: 'Downloads', icon: Download },
    { id: 'playlists', label: 'Playlists', icon: ListMusic },
  ];
  const player = new PlayerState();
  let view = $state('library');
  let adding = $state<Track>();
  let error = $state('');
  let listener = $state(
    session.user?.name || session.user?.email || 'Listener',
  );
  const navigate = (id: string) => {
    window.location.hash = id;
  };
  onMount(() => {
    const sync = () => {
      const hash = window.location.hash.slice(1);
      view = navigation.some((item) => item.id === hash) ? hash : 'library';
    };
    sync();
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

<div class="app-shell">
  <aside class="sidebar">
    <a class="brand" href="#library"
      ><AudioLines size={27} /><span>wavelength</span></a
    >
    <p class="nav-label">Your space</p>
    <nav aria-label="Main navigation">
      {#each navigation as item}<a
          href={`#${item.id}`}
          class:active={view === item.id}
          aria-current={view === item.id ? 'page' : undefined}
          ><item.icon size={19} /><span>{item.label}</span
          >{#if view === item.id}<span class="nav-dot"></span>{/if}</a
        >{/each}
    </nav>
    <div class="sidebar-note">
      <span class="mini-wave" aria-hidden="true">▂ ▅ ▃ ▇ ▄ ▂</span>
      <p>A collection that<br />sounds like you.</p>
    </div>
    <div class="account">
      <span class="avatar">{initials(listener)}</span>
      <div><strong>{listener}</strong><span>Your account</span></div>
      <button
        class="icon-button"
        aria-label="Sign out"
        title="Sign out"
        disabled={session.signingOut}
        onclick={logout}><LogOut size={17} /></button
      >
    </div>
  </aside>
  <main class="main-content">
    <div class="topbar">
      <span>YOUR PERSONAL MUSIC SPACE</span><span
        ><span class="online-dot"></span> Wavelength</span
      >
      <button
        class="icon-button mobile-account"
        aria-label="Sign out"
        disabled={session.signingOut}
        onclick={logout}><LogOut size={17} /></button
      >
    </div>
    <Notice
      message={error}
      dismiss={() => (error = '')}
    />{#if view === 'library'}<LibraryView
        {player}
        add={(track) => (adding = track)}
        discover={() => navigate('discover')}
      />{:else if view === 'discover'}<Discover
        imported={() => navigate('imports')}
      />{:else if view === 'imports'}<Imports
      />{:else if view === 'playlists'}<Playlists
        {player}
        add={(track) => (adding = track)}
      />{/if}
  </main>
  <Player {player} />
</div>
{#if adding}<PlaylistPicker
    track={adding}
    close={() => (adding = undefined)}
  />{/if}
