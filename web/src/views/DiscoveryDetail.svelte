<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { create } from '@bufbuild/protobuf';
  import { ArrowLeft, Download, Play } from '@lucide/svelte';
  import {
    ProviderRefSchema,
    type MusicArtist,
    type MusicRelease,
  } from '../gen/wavelength/v1/ingestion_pb';
  import type { RealtimeState } from '../lib/realtime.svelte';
  import type { PlayerState } from '../lib/player.svelte';
  import { DownloadSelection, refKey } from '../lib/downloads.svelte';
  import { api } from '../lib/session.svelte';
  import { errorMessage } from '../lib/api';
  import { duration } from '../lib/format';
  import { link } from '../lib/navigation';
  import Cover from '../components/Cover.svelte';
  import Notice from '../components/Notice.svelte';
  import ExternalTracks from '../components/ExternalTracks.svelte';
  import LibraryStatus from '../components/LibraryStatus.svelte';
  import ExternalRelease from '../components/ExternalRelease.svelte';
  let {
    kind,
    provider,
    id,
    basket,
    player,
    realtime,
    searchContext,
  }: {
    kind: string;
    provider: string;
    id: string;
    basket: DownloadSelection;
    player: PlayerState;
    realtime: RealtimeState;
    searchContext: Record<string, string>;
  } = $props();
  let artist = $state<MusicArtist>();
  let release = $state<MusicRelease>();
  let loading = $state(true);
  let checking = $state(false);
  let error = $state('');
  const saved = $derived(
    release?.tracks.filter((track) => basket.localID(track)).length || 0,
  );
  onMount(() => {
    const controller = new AbortController();
    const load = async () => {
      try {
        const ref = create(ProviderRefSchema, { provider, id });
        if (kind === 'artist')
          artist = (
            await api.discovery.getArtist(
              { ref },
              { signal: controller.signal },
            )
          ).artist;
        else {
          release = (
            await api.discovery.getRelease(
              { ref },
              { signal: controller.signal },
            )
          ).release;
          if (release?.ref)
            basket.releases = {
              ...basket.releases,
              [refKey(release.ref)]: release,
            };
          checking = true;
          await basket.lookup(release?.tracks || [], controller.signal);
        }
      } catch (cause) {
        if (!controller.signal.aborted) error = errorMessage(cause);
      } finally {
        loading = false;
        checking = false;
      }
    };
    void load();
    return () => controller.abort();
  });
  $effect(() => {
    if (realtime.resetVersion)
      untrack(() => {
        if (release)
          void basket
            .lookup(release.tracks)
            .catch((cause) => (error = errorMessage(cause)));
      });
  });
  $effect(() => {
    if (!artist?.releases.length) return;
    const albums = artist.releases;
    realtime.resetVersion; // Recheck saved songs after reconnecting.
    const pending = new AbortController();
    checking = true;
    untrack(
      () =>
        void basket
          .lookupReleases(albums, pending.signal)
          .catch((cause) => {
            if (!pending.signal.aborted) error = errorMessage(cause);
          })
          .finally(() => {
            if (!pending.signal.aborted) checking = false;
          }),
    );
    return () => pending.abort();
  });
  async function playSaved() {
    try {
      const results = await Promise.all(
        (release?.tracks || [])
          .filter((track) => basket.localID(track))
          .map((track) => api.library.getTrack({ id: basket.localID(track) })),
      );
      await player.play(
        results.flatMap((result) => (result.track ? [result.track] : [])),
      );
    } catch (cause) {
      error = errorMessage(cause);
    }
  }
</script>

<a class="back-link" href={link('discover', searchContext)}
  ><ArrowLeft size={14} /> Discover</a
><Notice message={error} />
{#if loading}<div class="loading-state" role="status">
    <span class="spinner"></span> Loading {kind === 'artist'
      ? 'artist'
      : 'album'}…
  </div>{:else if artist || release}<header class="entity-header">
    <Cover
      name={artist?.name || release?.title || ''}
      artwork={release?.artworkUrl ||
        artist?.artworkUrl ||
        artist?.releases[0]?.artworkUrl}
    />
    <div>
      <p class="entity-type">
        {kind === 'artist' ? 'Artist' : 'Album'} · Discover
      </p>
      <h1>{artist?.name || release?.title}</h1>
      {#if release}<p class="entity-artist">{release.artist}</p>
        <p class="entity-meta">
          {release.tracks.length} songs · {duration(
            release.tracks.reduce(
              (sum, track) => sum + track.durationSeconds,
              0,
            ),
          )}
        </p>
        <div class="entity-actions">
          <button
            class="button primary"
            disabled={checking || saved === release.tracks.length}
            onclick={() => release && basket.add(release.tracks)}
            ><Download size={14} />{saved
              ? 'Select remaining songs'
              : 'Select album'}</button
          >{#if saved}<button class="button secondary" onclick={playSaved}
              ><Play size={14} /> Play saved songs</button
            >{/if}<LibraryStatus
            {saved}
            total={release.tracks.length}
            {checking}
          />
        </div>{:else}<p class="entity-meta">
          Albums available to browse and download
        </p>{/if}
    </div>
  </header>
  {#if release}<ExternalTracks
      tracks={release.tracks}
      {basket}
      {player}
      {checking}
    />{:else}<div class="section-heading"><h2>Albums</h2></div>
    <div class="card-grid">
      {#each artist?.releases || [] as item}<ExternalRelease
          album={item}
          {basket}
          {player}
          context={searchContext}
          {checking}
        />{/each}
    </div>
    {#if !artist?.releases.length}<p class="muted">
        No albums returned for this artist.
      </p>{/if}{/if}{/if}
