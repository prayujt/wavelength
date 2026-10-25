<script lang="ts">
  import { onMount } from 'svelte';
  import { Plus, X } from '@lucide/svelte';
  import type { Playlist, Track } from '../gen/wavelength/v1/library_pb';
  import { api } from '../lib/session.svelte';
  import { errorMessage } from '../lib/api';
  import Modal from './Modal.svelte';
  import Notice from './Notice.svelte';
  let { track, close }: { track: Track; close: () => void } = $props();
  let playlists = $state<Playlist[]>([]);
  let total = $state(0);
  let busy = $state(false);
  let loading = $state(true);
  let error = $state('');
  let name = $state('');
  let controller: AbortController;
  onMount(() => {
    controller = new AbortController();
    void load();
    return () => controller.abort();
  });
  async function load() {
    loading = true;
    try {
      const result = await api.user.listPlaylists(
        { limit: 50, offset: playlists.length },
        { signal: controller.signal },
      );
      playlists = [...playlists, ...result.playlists];
      total = result.total;
    } catch (cause) {
      if (!controller.signal.aborted) error = errorMessage(cause);
    } finally {
      loading = false;
    }
  }
  async function add(id?: string) {
    busy = true;
    error = '';
    try {
      if (id) await api.user.updatePlaylist({ id, addTrackIds: [track.id] });
      else
        await api.user.createPlaylist({
          name: name.trim(),
          trackIds: [track.id],
        });
      close();
    } catch (cause) {
      error = errorMessage(cause);
    } finally {
      busy = false;
    }
  }
</script>

<Modal
  label="Add to playlist"
  close={() => {
    if (!busy) close();
  }}
>
  <div class="section-heading">
    <div>
      <p class="eyebrow">Keep it close</p>
      <h2>Add to playlist</h2>
      <p class="muted">{track.title}</p>
    </div>
    <button
      class="icon-button"
      aria-label="Close playlists"
      disabled={busy}
      onclick={close}><X size={20} /></button
    >
  </div>
  <Notice message={error} />
  <div class="playlist-choices">
    {#each playlists as playlist}<button
        class="button secondary"
        disabled={busy}
        onclick={() => add(playlist.id)}
        >{playlist.name}<Plus size={17} /></button
      >{/each}
  </div>
  {#if loading}<p class="muted" role="status">
      Loading playlists…
    </p>{:else if playlists.length < total}<button
      class="text-button accent"
      onclick={load}>More playlists</button
    >{/if}
  <form
    class="new-playlist-form"
    onsubmit={(event) => {
      event.preventDefault();
      void add();
    }}
  >
    <label
      >Or create a new playlist<input
        placeholder="Playlist name"
        bind:value={name}
        required
        maxlength="255"
      /></label
    ><button class="button primary" disabled={busy || !name.trim()}
      ><Plus size={16} /> Create & add</button
    >
  </form>
</Modal>
