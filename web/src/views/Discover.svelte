<script lang="ts">
  import { onDestroy } from 'svelte';
  import {
    ArrowLeft,
    ArrowRight,
    Check,
    Download,
    Search,
    SlidersHorizontal,
    Sparkles,
  } from '@lucide/svelte';
  import type {
    ExternalTrack,
    MusicArtist,
    MusicRelease,
    ProviderRef,
    SearchMusicResponse,
  } from '../gen/wavelength/v1/ingestion_pb';
  import { api } from '../lib/session.svelte';
  import { duration } from '../lib/format';
  import { errorMessage } from '../lib/api';
  import Cover from '../components/Cover.svelte';
  import Notice from '../components/Notice.svelte';
  import SourcePicker from '../components/SourcePicker.svelte';
  import Modal from '../components/Modal.svelte';

  let { imported }: { imported: () => void } = $props();
  let query = $state('');
  let results = $state<SearchMusicResponse>();
  let artist = $state<MusicArtist>();
  let release = $state<MusicRelease>();
  let selected = $state<ExternalTrack[]>([]);
  let sources = $state<Record<string, ProviderRef>>({});
  let choosing = $state<ExternalTrack>();
  let loading = $state(false);
  let importing = $state(false);
  let reviewing = $state(false);
  let error = $state('');
  let requestID = '';
  let controller: AbortController | undefined;
  const tracks = $derived(
    release?.tracks || (artist ? [] : results?.tracks || []),
  );
  const releases = $derived(
    release ? [] : artist?.releases || results?.releases || [],
  );
  const key = (track: ExternalTrack) =>
    `${track.ref?.provider}:${track.ref?.id}`;
  onDestroy(() => controller?.abort());

  async function browse(action: () => Promise<void>) {
    controller?.abort();
    controller = new AbortController();
    loading = true;
    error = '';
    const active = controller;
    try {
      await action();
    } catch (cause) {
      if (!active.signal.aborted) error = errorMessage(cause);
    } finally {
      if (controller === active) loading = false;
    }
  }
  function search() {
    if (!query.trim()) return;
    void browse(async () => {
      const result = await api.discovery.search(
        { query: query.trim(), limit: 15 },
        { signal: controller?.signal },
      );
      results = result;
      artist = undefined;
      release = undefined;
    });
  }
  function openArtist(item: MusicArtist) {
    void browse(async () => {
      const response = await api.discovery.getArtist(
        { ref: item.ref },
        { signal: controller?.signal },
      );
      artist = response.artist;
      release = undefined;
    });
  }
  function openRelease(item: MusicRelease) {
    void browse(async () => {
      const response = await api.discovery.getRelease(
        { ref: item.ref },
        { signal: controller?.signal },
      );
      release = response.release;
    });
  }
  function toggle(track: ExternalTrack) {
    requestID = '';
    selected = selected.some((item) => key(item) === key(track))
      ? selected.filter((item) => key(item) !== key(track))
      : [...selected, track];
  }
  async function importSelected() {
    if (!selected.length) return;
    importing = true;
    error = '';
    requestID ||= crypto.randomUUID();
    try {
      await api.ingestion.createImport({
        requestId: requestID,
        items: selected.map((track) => ({
          metadataRef: track.ref,
          sourceRef: sources[key(track)],
        })),
      });
      selected = [];
      requestID = '';
      imported();
    } catch (cause) {
      error = errorMessage(cause);
    } finally {
      importing = false;
    }
  }
</script>

<header class="page-heading">
  <div>
    <p class="eyebrow">There's more out there</p>
    <h1>Find your next<span class="accent">.</span></h1>
    <p class="subtitle">
      Look up an artist, dig into an album, bring a few songs home.
    </p>
  </div>
  <span class="pill"><Sparkles size={14} /> Discover</span>
</header>
<form
  class="search-box"
  onsubmit={(event) => {
    event.preventDefault();
    search();
  }}
>
  <Search size={20} /><input
    aria-label="Search for new music"
    placeholder="An artist, a song, an album…"
    bind:value={query}
  /><button class="button primary compact" disabled={loading || !query.trim()}
    >Search <ArrowRight size={15} /></button
  >
</form>
<p class="search-caption">
  Searches music outside your library. Choose what you want to add.
</p>
<Notice message={error} />
{#if loading}<div class="loading-state" role="status">
    <span class="spinner"></span> Looking for your next listen…
  </div>
{:else if !results}<div class="discovery-intro">
    <span class="eyebrow">Follow your curiosity</span>
    <h2>Start with someone<br />you love listening to.</h2>
    <p>
      Find their albums, pick your songs, and let Wavelength take care of the
      rest.
    </p>
    <div class="record-decoration" aria-hidden="true"><div></div></div>
  </div>
{:else}
  {#if artist || release}<div class="detail-heading">
      <button
        class="icon-button"
        aria-label="Back to search results"
        onclick={() => {
          if (release) release = undefined;
          else artist = undefined;
        }}><ArrowLeft size={20} /></button
      >
      <h2>{release?.title || artist?.name}</h2>
    </div>{/if}
  {#if !artist && !release && results.artists.length}<section
      class="result-section"
    >
      <h2>Artists</h2>
      <div class="artist-chips">
        {#each results.artists as item}<button
            class="artist-chip"
            onclick={() => openArtist(item)}
            ><Cover name={item.name} small /><strong>{item.name}</strong
            ><ArrowRight size={16} /></button
          >{/each}
      </div>
    </section>{/if}
  {#if releases.length}<section class="result-section">
      <h2>Albums</h2>
      <div class="card-grid">
        {#each releases as item}<button
            class="music-card"
            onclick={() => openRelease(item)}
            ><Cover name={item.title} artwork={item.artworkUrl} /><strong
              >{item.title}</strong
            ><span>{item.artist}</span></button
          >{/each}
      </div>
    </section>{/if}
  {#if tracks.length}<section class="result-section">
      <div class="section-heading">
        <h2>Songs <span class="count">{tracks.length}</span></h2>
        <button
          class="text-button accent"
          disabled={importing}
          onclick={() => {
            requestID = '';
            selected = [
              ...new Map(
                [...selected, ...tracks].map((track) => [key(track), track]),
              ).values(),
            ];
          }}>Select all</button
        >
      </div>
      <div class="discovery-tracks">
        {#each tracks as track}<div class="discovery-track">
            <input
              type="checkbox"
              aria-label={`Select ${track.title}`}
              checked={selected.some((item) => key(item) === key(track))}
              disabled={importing}
              onchange={() => toggle(track)}
            /><Cover
              name={track.album || track.title}
              artwork={track.artworkUrl}
              small
            />
            <div class="track-info">
              <strong>{track.title}</strong><span
                >{track.artist} · {track.album}</span
              >
            </div>
            <span class="muted">{duration(track.durationSeconds)}</span><button
              class:accent={!!sources[key(track)]}
              class="icon-button"
              aria-label={`Choose recording for ${track.title}`}
              disabled={importing}
              onclick={() => (choosing = track)}
              >{#if sources[key(track)]}<Check
                  size={18}
                />{:else}<SlidersHorizontal size={18} />{/if}</button
            >
          </div>{/each}
      </div>
    </section>{/if}
  {#if !tracks.length && !releases.length && (!results.artists.length || artist)}<div
      class="empty-state"
    >
      <Search size={30} />
      <h2>Nothing found here</h2>
      <p>Try a different search or open another album.</p>
    </div>{/if}
{/if}
{#if selected.length}<div class="selection-bar">
    <div>
      <strong
        >{selected.length}
        {selected.length === 1 ? 'song' : 'songs'} selected</strong
      ><span
        >Strong matches are chosen automatically. Use the sliders to choose a
        recording.</span
      >
    </div>
    <button
      class="button secondary compact"
      disabled={importing}
      onclick={() => (reviewing = true)}>Review</button
    >
    <button
      class="button primary"
      disabled={importing || selected.length > 200}
      onclick={importSelected}
      ><Download size={17} />{importing
        ? 'Starting…'
        : 'Add to library'}</button
    >
  </div>{/if}
{#if reviewing}<Modal label="Selected songs" close={() => (reviewing = false)}>
    <div class="section-heading">
      <h2>Selected songs</h2>
      <button
        class="text-button accent"
        disabled={importing}
        onclick={() => {
          selected = [];
          requestID = '';
          reviewing = false;
        }}>Clear selection</button
      >
    </div>
    {#if selected.length > 200}<Notice
        message="Choose up to 200 songs per import. Remove some songs to continue."
      />{/if}
    <div class="selected-songs">
      {#each selected as track}<div class="import-item">
          <div>
            <strong>{track.title}</strong><span class="muted"
              >{track.artist} · {track.album}</span
            >
          </div>
          <button
            class="text-button muted"
            disabled={importing}
            aria-label={`Unselect ${track.title}`}
            onclick={() => toggle(track)}>Remove</button
          >
        </div>{/each}
    </div>
    <div class="dialog-actions">
      <button class="button primary" onclick={() => (reviewing = false)}
        >Done</button
      >
    </div>
  </Modal>{/if}
{#if choosing}<SourcePicker
    track={choosing}
    close={() => (choosing = undefined)}
    choose={(ref) => {
      if (!choosing) return;
      requestID = '';
      sources = { ...sources, [key(choosing)]: ref };
      if (!selected.some((item) => key(item) === key(choosing!)))
        selected = [...selected, choosing];
      choosing = undefined;
    }}
  />{/if}
