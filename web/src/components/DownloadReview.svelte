<script lang="ts">
  import { onMount } from 'svelte';
  import { Download, SlidersHorizontal, X } from '@lucide/svelte';
  import type { Playlist } from '../gen/wavelength/v1/library_pb';
  import type {
    ExternalTrack,
    ImportJob,
  } from '../gen/wavelength/v1/ingestion_pb';
  import { DownloadSelection, refKey } from '../lib/downloads.svelte';
  import { api } from '../lib/session.svelte';
  import { errorMessage } from '../lib/api';
  import { duration } from '../lib/format';
  import SongCredits from './SongCredits.svelte';
  import Cover from './Cover.svelte';
  import Dropdown from './Dropdown.svelte';
  import Modal from './Modal.svelte';
  import Notice from './Notice.svelte';
  import SourcePicker from './SourcePicker.svelte';
  let {
    basket,
    started,
  }: { basket: DownloadSelection; started: (job: ImportJob) => void } =
    $props();
  let playlists = $state<Playlist[]>([]);
  let playlistTotal = $state(0);
  let loading = $state(true);
  let busy = $state(false);
  let error = $state('');
  let choosing = $state<ExternalTrack>();
  const controller = new AbortController();
  onMount(() => {
    void loadPlaylists();
    return () => controller.abort();
  });
  async function loadPlaylists() {
    loading = true;
    try {
      const result = await api.user.listPlaylists(
        { limit: 50, offset: playlists.length },
        { signal: controller.signal },
      );
      playlists = [...playlists, ...result.playlists];
      playlistTotal = result.total;
    } catch (cause) {
      if (!controller.signal.aborted) error = errorMessage(cause);
    } finally {
      loading = false;
    }
  }
  async function submit() {
    if (!basket.tracks.length || basket.tracks.length > 200 || busy) return;
    busy = true;
    error = '';
    try {
      await basket.lookup(basket.tracks, controller.signal, true);
      if (!basket.tracks.length) {
        error = 'These songs are already in your library.';
        return;
      }
      const job = await api.ingestion.createImport(basket.request());
      basket.clear();
      started(job);
    } catch (cause) {
      error = errorMessage(cause);
    } finally {
      busy = false;
    }
  }
</script>

<Modal
  label="Review download"
  close={() => {
    if (!busy) basket.reviewing = false;
  }}
>
  <div class="section-heading">
    <div>
      <h2>Review download</h2>
      <p class="muted">
        {basket.tracks.length}
        {basket.tracks.length === 1 ? 'song' : 'songs'} · {duration(
          basket.duration,
        )}
      </p>
    </div>
    <button
      class="icon-button"
      aria-label="Close review"
      disabled={busy}
      onclick={() => (basket.reviewing = false)}><X size={18} /></button
    >
  </div>
  <Notice message={error} />{#if basket.tracks.length > 200}<Notice
      message="Choose up to 200 songs per download."
    />{/if}
  <div class="review-tracks">
    {#each basket.tracks as track (refKey(track.ref))}<div class="review-track">
        <Cover
          name={track.album || track.title}
          artwork={track.artworkUrl}
          small
        />
        <div class="track-info">
          <strong>{track.title}</strong><SongCredits {track} /><span
            >{basket.sources[refKey(track.ref)]?.title ||
              'Automatic matching'}</span
          ><button
            class="source-choice"
            disabled={busy}
            aria-label={`${basket.sources[refKey(track.ref)] ? 'Change' : 'Choose'} recording for ${track.title}`}
            onclick={() => (choosing = track)}
            ><SlidersHorizontal size={12} />{basket.sources[refKey(track.ref)]
              ? 'Change recording'
              : 'Choose recording'}</button
          >{#if basket.sources[refKey(track.ref)]}<button
              class="text-button muted small-text"
              disabled={busy}
              onclick={() => basket.automatic(track)}
              >Use automatic matching</button
            >{/if}
        </div>
        <button
          class="icon-button"
          disabled={busy}
          aria-label={`Unselect ${track.title}`}
          onclick={() => basket.remove(track)}><X size={15} /></button
        >
      </div>{/each}
  </div>
  <div class="review-options">
    <label for="download-playlist">Add to playlist</label>
    <Dropdown
      id="download-playlist"
      label="Download playlist"
      value={basket.playlistId}
      disabled={busy || loading}
      onchange={(value) => basket.playlist(value)}
      options={[
        { value: '', label: 'Library only' },
        ...playlists.map((playlist) => ({
          value: playlist.id,
          label: playlist.name,
        })),
      ]}
    />
    {#if playlists.length < playlistTotal}<button
        class="text-button muted"
        disabled={busy || loading}
        onclick={loadPlaylists}>Load more playlists</button
      >{/if}
    <p class="muted small-text">
      Choose a recording above, or leave automatic matching on. Uncertain
      matches will wait for your choice in Downloads.
    </p>
  </div>
  <div class="dialog-actions">
    <button
      class="button secondary"
      disabled={busy}
      onclick={() => (basket.reviewing = false)}>Back</button
    ><button
      class="button primary"
      disabled={busy || !basket.tracks.length || basket.tracks.length > 200}
      onclick={submit}
      ><Download size={14} />{busy
        ? 'Starting…'
        : `Download ${basket.tracks.length} ${basket.tracks.length === 1 ? 'song' : 'songs'}`}</button
    >
  </div>
</Modal>
{#if choosing}<SourcePicker
    track={choosing}
    close={() => (choosing = undefined)}
    choose={(candidate) => {
      if (choosing) basket.choose(choosing, candidate);
      choosing = undefined;
    }}
  />{/if}
