<script lang="ts">
  import { onMount } from 'svelte';
  import {
    ArrowLeft,
    ListMusic,
    Play,
    Plus,
    Save,
    Trash2,
  } from '@lucide/svelte';
  import type { Playlist, Track } from '../gen/wavelength/v1/library_pb';
  import type { PlayerState } from '../lib/player.svelte';
  import { api } from '../lib/session.svelte';
  import { errorMessage } from '../lib/api';
  import Cover from '../components/Cover.svelte';
  import Notice from '../components/Notice.svelte';
  import Modal from '../components/Modal.svelte';
  import TrackList from '../components/TrackList.svelte';
  let { player, add }: { player: PlayerState; add: (track: Track) => void } =
    $props();
  let playlists = $state<Playlist[]>([]);
  let selected = $state<Playlist>();
  let entries = $state<{ track: Track; index: number }[]>([]);
  let loading = $state(true);
  let busy = $state(false);
  let error = $state('');
  let offset = $state(0);
  let total = $state(0);
  let name = $state('');
  let newName = $state('');
  let comment = $state('');
  let shared = $state(false);
  let creating = $state(false);
  let deleting = $state(false);
  let controller: AbortController;
  let request = 0;
  onMount(() => {
    controller = new AbortController();
    void load();
    return () => {
      ++request;
      controller.abort();
    };
  });
  async function load(nextOffset = offset) {
    loading = true;
    error = '';
    try {
      const response = await api.user.listPlaylists(
        { limit: 50, offset: nextOffset },
        { signal: controller.signal },
      );
      playlists = response.playlists;
      total = response.total;
      offset = nextOffset;
    } catch (cause) {
      if (!controller.signal.aborted) error = errorMessage(cause);
    } finally {
      loading = false;
    }
  }
  async function open(id: string) {
    const generation = ++request;
    loading = true;
    error = '';
    try {
      const { playlist } = await api.user.getPlaylist(
        { id },
        { signal: controller.signal },
      );
      if (!playlist || generation !== request) return;
      selected = playlist;
      name = playlist.name;
      comment = playlist.comment;
      shared = playlist.public;
      const tracks = await Promise.all(
        playlist.trackIds.map(async (id, index) => {
          const { track } = await api.library.getTrack(
            { id },
            { signal: controller.signal },
          );
          if (!track) throw new Error('A playlist track is unavailable.');
          return { track, index };
        }),
      );
      if (generation === request) entries = tracks;
    } catch (cause) {
      if (generation === request && !controller.signal.aborted) {
        entries = [];
        error = errorMessage(cause);
      }
    } finally {
      if (generation === request) loading = false;
    }
  }
  async function create() {
    if (!newName.trim()) return;
    busy = true;
    error = '';
    try {
      const { playlist } = await api.user.createPlaylist({
        name: newName.trim(),
      });
      creating = false;
      await load(0);
      if (playlist) await open(playlist.id);
    } catch (cause) {
      error = errorMessage(cause);
    } finally {
      busy = false;
    }
  }
  async function update(removeIndex?: number) {
    if (!selected || !name.trim()) return;
    busy = true;
    error = '';
    try {
      await api.user.updatePlaylist({
        id: selected.id,
        name: name.trim(),
        comment,
        public: shared,
        removeIndices: removeIndex === undefined ? [] : [removeIndex],
      });
      await open(selected.id);
    } catch (cause) {
      error = errorMessage(cause);
    } finally {
      busy = false;
    }
  }
  async function remove() {
    if (!selected) return;
    busy = true;
    try {
      await api.user.deletePlaylist({ id: selected.id });
      deleting = false;
      selected = undefined;
      await load(0);
    } catch (cause) {
      error = errorMessage(cause);
    } finally {
      busy = false;
    }
  }
</script>

<header class="page-heading">
  <div>
    <p class="eyebrow">For every kind of day</p>
    <h1>Your playlists<span class="accent">.</span></h1>
    <p class="subtitle">A soundtrack for whatever comes next.</p>
  </div>
  <button
    class="button primary"
    onclick={() => {
      newName = '';
      creating = true;
    }}
    disabled={busy}><Plus size={17} /> New playlist</button
  >
</header>
<Notice message={error} />
{#if loading}<div class="loading-state" role="status">
    <span class="spinner"></span> Loading playlists…
  </div>
{:else if selected}<div class="detail-heading">
    <button
      class="icon-button"
      aria-label="Back to playlists"
      onclick={() => {
        selected = undefined;
        void load();
      }}><ArrowLeft size={20} /></button
    >
    <h2>{selected.name}</h2>
    <span class="count"
      >{selected.trackIds.length}
      {selected.trackIds.length === 1 ? 'song' : 'songs'}</span
    >{#if entries.length}<button
        class="button primary compact"
        onclick={() => player.play(entries.map((entry) => entry.track))}
        ><Play size={15} fill="currentColor" /> Play all</button
      >{/if}
  </div>
  <form
    class="playlist-editor"
    onsubmit={(event) => {
      event.preventDefault();
      void update();
    }}
  >
    <label
      >Name<input
        aria-label="Playlist name"
        bind:value={name}
        required
        maxlength="255"
      /></label
    ><label
      >Description<input
        aria-label="Playlist description"
        bind:value={comment}
        maxlength="2000"
      /></label
    ><label class="checkbox-label"
      ><input type="checkbox" bind:checked={shared} /> Visible to other listeners</label
    ><button class="button secondary compact" disabled={busy || !name.trim()}
      ><Save size={15} /> Save changes</button
    ><button
      type="button"
      class="icon-button danger"
      aria-label="Delete playlist"
      disabled={busy}
      onclick={() => (deleting = true)}><Trash2 size={18} /></button
    >
  </form>
  {#if entries.length}<TrackList
      tracks={entries.map((entry) => entry.track)}
      {player}
      {add}
      remove={(index) => {
        if (!busy) void update(entries[index].index);
      }}
    />{:else if !error}<div class="empty-state">
      <ListMusic size={30} />
      <h2>Make room for your favorites</h2>
      <p>Use the + beside a song in your library to add it here.</p>
    </div>{/if}
{:else if !playlists.length && !error}<div class="empty-state">
    <div class="empty-icon"><ListMusic size={30} /></div>
    <h2>A mix with your name on it</h2>
    <p>Create a playlist, then add songs from your library.</p>
    <button
      class="button primary"
      onclick={() => {
        newName = '';
        creating = true;
      }}><Plus size={16} /> Create your first playlist</button
    >
  </div>
{:else}<div class="card-grid">
    {#each playlists as playlist}<button
        class="music-card"
        onclick={() => open(playlist.id)}
        ><Cover name={playlist.name} /><strong>{playlist.name}</strong><span
          >{playlist.trackIds.length}
          {playlist.trackIds.length === 1 ? 'song' : 'songs'} · {playlist.public
            ? 'Shared'
            : 'Only you'}</span
        ></button
      >{/each}
  </div>
  {#if total > 50}<div class="pagination">
      <button
        class="button secondary compact"
        disabled={offset === 0}
        onclick={() => load(offset - 50)}>Previous</button
      ><span>{offset + 1}–{Math.min(offset + 50, total)} of {total}</span
      ><button
        class="button secondary compact"
        disabled={offset + 50 >= total}
        onclick={() => load(offset + 50)}>Next</button
      >
    </div>{/if}{/if}
{#if creating}<Modal
    label="New playlist"
    close={() => {
      if (!busy) creating = false;
    }}
    ><p class="eyebrow">Make it yours</p>
    <h2>New playlist</h2>
    <form
      onsubmit={(event) => {
        event.preventDefault();
        void create();
      }}
    >
      <label
        >Playlist name<input
          bind:value={newName}
          placeholder="Late night, long drive…"
          required
          maxlength="255"
        /></label
      ><Notice message={error} />
      <div class="dialog-actions">
        <button
          type="button"
          class="button secondary"
          disabled={busy}
          onclick={() => (creating = false)}>Cancel</button
        ><button class="button primary" disabled={busy || !newName.trim()}
          >{busy ? 'Creating…' : 'Create playlist'}</button
        >
      </div>
    </form></Modal
  >{/if}
{#if deleting}<Modal
    label="Delete playlist"
    close={() => {
      if (!busy) deleting = false;
    }}
    ><h2>Delete “{selected?.name}”?</h2>
    <p class="muted">
      This removes the playlist. Its songs stay in your library.
    </p>
    <Notice message={error} />
    <div class="dialog-actions">
      <button
        class="button secondary"
        disabled={busy}
        onclick={() => (deleting = false)}>Keep playlist</button
      ><button class="button destructive" disabled={busy} onclick={remove}
        >{busy ? 'Deleting…' : 'Delete playlist'}</button
      >
    </div></Modal
  >{/if}
