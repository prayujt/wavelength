<script lang="ts">
  import { onMount, tick, untrack } from 'svelte';
  import {
    ArrowDownToLine,
    ArrowUpRight,
    Check,
    CornerDownLeft,
    Disc3,
    Music2,
    Search,
    UserRound,
  } from '@lucide/svelte';
  import type { Album, Artist, Track } from '../gen/wavelength/v1/library_pb';
  import type { SearchMusicResponse } from '../gen/wavelength/v1/ingestion_pb';
  import type { RealtimeState } from '../lib/realtime.svelte';
  import type { PlayerState } from '../lib/player.svelte';
  import { DownloadSelection } from '../lib/downloads.svelte';
  import { api } from '../lib/session.svelte';
  import { errorMessage } from '../lib/api';
  import { go, link } from '../lib/navigation';
  import Cover from './Cover.svelte';
  import Modal from './Modal.svelte';
  let {
    player,
    basket,
    realtime,
    close,
  }: {
    player: PlayerState;
    basket: DownloadSelection;
    realtime: RealtimeState;
    close: () => void;
  } = $props();
  let query = $state('');
  let tracks = $state<Track[]>([]);
  let albums = $state<Album[]>([]);
  let artists = $state<Artist[]>([]);
  let external = $state<SearchMusicResponse>();
  let localLoading = $state(false);
  let externalLoading = $state(false);
  let localError = $state('');
  let externalError = $state('');
  let active = $state(0);
  let input: HTMLInputElement;
  const id = $props.id();
  const pages = [
    { name: 'Your library', page: 'library' },
    { name: 'Discover', page: 'discover' },
    { name: 'Downloads', page: 'imports' },
    { name: 'Playlists', page: 'playlists' },
  ];
  type Result = {
    key: string;
    group: string;
    title: string;
    detail: string;
    action: string;
    artwork?: string;
    kind: 'song' | 'artist' | 'album' | 'page';
    saved?: boolean;
    run: () => void | Promise<void>;
  };
  const results = $derived.by<Result[]>(() => {
    const rows: Result[] = [];
    if (!query.trim())
      return pages.map((page) => ({
        key: page.page,
        group: 'Pages',
        title: page.name,
        detail: '',
        action: 'Open',
        kind: 'page' as const,
        run: () => go(page.page),
      }));
    for (const track of tracks)
      rows.push({
        key: `local-${track.id}`,
        group: 'Your library',
        title: track.title,
        detail: `${track.artist} · ${track.album}`,
        action: 'Play',
        artwork: track.artworkPath,
        kind: 'song',
        saved: true,
        run: () => player.play([track]),
      });
    for (const album of albums)
      rows.push({
        key: `album-${album.id}`,
        group: 'Your library',
        title: album.name,
        detail: `${album.artist} · Album`,
        action: 'Open album',
        artwork: album.artworkPath,
        kind: 'album',
        run: () => go(`album/${encodeURIComponent(album.id)}`),
      });
    for (const artist of artists)
      rows.push({
        key: `artist-${artist.id}`,
        group: 'Your library',
        title: artist.name,
        detail: 'Artist',
        action: 'Open artist',
        artwork: artist.artworkPath,
        kind: 'artist',
        run: () => go(`artist/${encodeURIComponent(artist.id)}`),
      });
    for (const track of external?.tracks || []) {
      const localID = basket.localID(track);
      rows.push({
        key: `external-${track.ref?.id}`,
        group: 'Discover',
        title: track.title,
        detail: `${track.artist} · ${track.album}`,
        action: localID ? 'Play' : 'Review download',
        artwork: track.artworkUrl,
        kind: 'song',
        saved: !!localID,
        run: async () => {
          if (localID) {
            const found = await api.library.getTrack({ id: localID });
            if (found.track) await player.play([found.track]);
          } else {
            basket.add([track]);
            basket.reviewing = true;
          }
        },
      });
    }
    for (const album of external?.releases || [])
      rows.push({
        key: `release-${album.ref?.id}`,
        group: 'Discover',
        title: album.title,
        detail: `${album.artist} · Album`,
        action: 'Browse album',
        artwork: album.artworkUrl,
        kind: 'album',
        run: () => {
          window.location.hash = link('discover/release', {
            q: query.trim(),
            provider: album.ref?.provider || '',
            id: album.ref?.id || '',
          });
        },
      });
    for (const artist of external?.artists || [])
      rows.push({
        key: `external-artist-${artist.ref?.id}`,
        group: 'Discover',
        artwork: artist.artworkUrl,
        title: artist.name,
        detail: 'Artist',
        action: 'Browse albums',
        kind: 'artist',
        run: () => {
          window.location.hash = link('discover/artist', {
            q: query.trim(),
            provider: artist.ref?.provider || '',
            id: artist.ref?.id || '',
          });
        },
      });
    return rows;
  });
  onMount(() => {
    input.focus();
  });
  $effect(() => {
    const text = query.trim();
    active = 0;
    tracks = [];
    albums = [];
    artists = [];
    external = undefined;
    localError = '';
    externalError = '';
    if (!text) {
      localLoading = false;
      externalLoading = false;
      return;
    }
    const controller = new AbortController();
    localLoading = true;
    externalLoading = true;
    const localTimer = setTimeout(() => {
      const options = { signal: controller.signal };
      Promise.all([
        api.library.listTracks({ query: text, limit: 6 }, options),
        api.library.listAlbums({ query: text, limit: 3 }, options),
        api.library.listArtists({ query: text, limit: 3 }, options),
      ])
        .then(([songs, records, people]) => {
          if (!controller.signal.aborted) {
            tracks = songs.tracks;
            albums = records.albums;
            artists = people.artists;
          }
        })
        .catch((cause) => {
          if (!controller.signal.aborted) localError = errorMessage(cause);
        })
        .finally(() => {
          if (!controller.signal.aborted) localLoading = false;
        });
    }, 150);
    const externalTimer = setTimeout(() => {
      api.discovery
        .search({ query: text, limit: 5 }, { signal: controller.signal })
        .then(async (found) => {
          await basket.lookup(found.tracks, controller.signal);
          if (!controller.signal.aborted) external = found;
        })
        .catch((cause) => {
          if (!controller.signal.aborted) externalError = errorMessage(cause);
        })
        .finally(() => {
          if (!controller.signal.aborted) externalLoading = false;
        });
    }, 600);
    return () => {
      controller.abort();
      clearTimeout(localTimer);
      clearTimeout(externalTimer);
    };
  });
  $effect(() => {
    if (realtime.resetVersion)
      untrack(() => {
        if (external)
          void basket
            .lookup(external.tracks)
            .catch((cause) => (externalError = errorMessage(cause)));
      });
  });
  async function choose(result: Result) {
    close();
    try {
      await result.run();
    } catch (cause) {
      player.error = errorMessage(cause);
    }
  }
  async function keydown(event: KeyboardEvent) {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      active =
        (active + (event.key === 'ArrowDown' ? 1 : -1) + results.length) %
        Math.max(1, results.length);
      await tick();
      document
        .getElementById(`${id}-${active}`)
        ?.scrollIntoView({ block: 'nearest' });
    } else if (event.key === 'Enter' && results[active]) {
      event.preventDefault();
      void choose(results[active]);
    }
  }
</script>

<Modal label="Search music" {close}>
  <div class="command-input">
    <Search size={18} /><input
      bind:this={input}
      bind:value={query}
      role="combobox"
      aria-label="Search all music"
      aria-expanded="true"
      aria-controls={`${id}-results`}
      aria-autocomplete="list"
      aria-activedescendant={results[active] ? `${id}-${active}` : undefined}
      placeholder="Search library and new music…"
      onkeydown={keydown}
      autocomplete="off"
    /><kbd>esc</kbd>
  </div>
  <div
    class="command-results"
    role="listbox"
    id={`${id}-results`}
    aria-label="Search results"
  >
    {#each results as result, index (result.key)}{#if index === 0 || results[index - 1].group !== result.group}<div
          class="command-group"
          aria-hidden="true"
        >
          {result.group}<span
            >{result.group === 'Discover'
              ? 'External music catalog'
              : result.group === 'Your library'
                ? 'Ready to play'
                : ''}</span
          >
        </div>{/if}<button
        role="option"
        id={`${id}-${index}`}
        tabindex="-1"
        aria-selected={active === index}
        class:active={active === index}
        onpointermove={() => (active = index)}
        onpointerdown={(event) => event.preventDefault()}
        onclick={() => choose(result)}
        >{#if result.kind === 'song' || result.artwork}<Cover
            name={result.title}
            artwork={result.artwork}
            small
          />{:else if result.kind === 'artist'}<UserRound
            size={18}
          />{:else if result.kind === 'album'}<Disc3
            size={18}
          />{:else}<ArrowUpRight size={18} />{/if}<span
          class="command-result-label"
          ><strong>{result.title}</strong><small>{result.detail}</small></span
        ><span class="command-result-action"
          >{#if result.saved}<span class="library-badge"
              ><Check size={11} /> In library</span
            >{:else if result.kind === 'song'}<span class="muted"
              ><ArrowDownToLine size={12} /> Not downloaded</span
            >{/if}<span>{result.action}</span></span
        >{#if index === active}<CornerDownLeft size={13} />{/if}</button
      >{/each}
    {#if localLoading}<p class="command-message" role="status">
        Searching your library…
      </p>{/if}{#if externalLoading}<p class="command-message" role="status">
        Searching external music…
      </p>{/if}{#if localError}<p class="command-message error-text">
        Library: {localError}
      </p>{/if}{#if externalError}<p class="command-message error-text">
        Discovery: {externalError}
      </p>{/if}{#if query.trim() && !results.length && !localLoading && !externalLoading && !localError && !externalError}<div
        class="command-empty"
      >
        <Music2 size={24} /><strong>No results for “{query}”</strong><span
          >Try another artist, album, or song.</span
        >
      </div>{/if}
  </div>
  <div class="command-footer">
    <span><kbd>↑</kbd><kbd>↓</kbd> Navigate</span><span
      ><kbd>↵</kbd> Select</span
    ><span>Library + Discover</span>
  </div>
</Modal>
