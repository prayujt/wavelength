<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { create } from '@bufbuild/protobuf';
  import { ArrowLeft, Download, Play } from '@lucide/svelte';
  import {
    ProviderRefSchema,
    type MusicArtist,
    type MusicRelease,
  } from '../gen/wavelength/v1/ingestion_pb';
  import type { DiscoveryState } from '../lib/discovery.svelte';
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
    discovery,
  }: {
    kind: string;
    provider: string;
    id: string;
    basket: DownloadSelection;
    player: PlayerState;
    realtime: RealtimeState;
    searchContext: Record<string, string>;
    discovery: DiscoveryState;
  } = $props();
  let artist = $state<MusicArtist>();
  let release = $state<MusicRelease>();
  let loading = $state(true);
  let checking = $state(false);
  let error = $state('');
  let releaseLimit = $state(12);
  const visibleReleases = $derived(
    artist?.releases.slice(0, releaseLimit) || [],
  );
  const saved = $derived(
    release?.tracks.filter((track) => basket.localID(track)).length || 0,
  );
  const full = $derived(
    !!release?.tracks.length && saved === release.tracks.length,
  );
  onMount(() => {
    const controller = new AbortController();
    const load = async () => {
      try {
        const ref = create(ProviderRefSchema, { provider, id });
        if (kind === 'artist') {
          artist =
            discovery.artists.get(refKey(ref)) ||
            (
              await api.discovery.getArtist(
                { ref },
                { signal: controller.signal },
              )
            ).artist;
          if (artist) discovery.artists.set(refKey(ref), artist);
        } else {
          release =
            basket.releases[refKey(ref)] ||
            (
              await api.discovery.getRelease(
                { ref },
                { signal: controller.signal },
              )
            ).release;
          loading = false;
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
    if (!visibleReleases.length) return;
    const albums = visibleReleases;
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
      {#if release}<p class="entity-artist">
          <a
            href={release.artistRef
              ? link('discover/artist', {
                  ...searchContext,
                  provider: release.artistRef.provider,
                  id: release.artistRef.id,
                })
              : link('discover', { q: release.artist, category: 'artists' })}
            >{release.artist}</a
          >
        </p>
        <p class="entity-meta">
          {release.tracks.length} songs · {duration(
            release.tracks.reduce(
              (sum, track) => sum + track.durationSeconds,
              0,
            ),
          )}
        </p>
        <div class="entity-actions">
          {#if full}<button
              class="button primary"
              disabled={checking}
              onclick={playSaved}><Play size={14} /> Play</button
            >{:else}<button
              class="button primary"
              disabled={checking || saved === release.tracks.length}
              onclick={() => release && basket.add(release.tracks)}
              ><Download size={14} />{saved
                ? 'Select remaining songs'
                : 'Select album'}</button
            >{#if saved}<button class="button secondary" onclick={playSaved}
                ><Play size={14} /> Play saved songs</button
              >{/if}{/if}<LibraryStatus
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
      {#each visibleReleases as item}<ExternalRelease
          album={item}
          {basket}
          {player}
          context={searchContext}
          {checking}
        />{/each}
    </div>
    {#if artist && releaseLimit < artist.releases.length}<div
        class="pagination"
      >
        <button
          class="button secondary"
          disabled={checking}
          onclick={() => (releaseLimit += 12)}>Show more albums</button
        ><span>{visibleReleases.length} of {artist.releases.length} albums</span
        >
      </div>{/if}
    {#if !artist?.releases.length}<p class="muted">
        No albums returned for this artist.
      </p>{/if}{/if}{/if}
