<script lang="ts">
  import { onMount } from 'svelte';
  import { ArrowLeft, Play } from '@lucide/svelte';
  import type { Album, Artist, Track } from '../gen/wavelength/v1/library_pb';
  import type { PlayerState } from '../lib/player.svelte';
  import { api } from '../lib/session.svelte';
  import { errorMessage } from '../lib/api';
  import { duration } from '../lib/format';
  import { link } from '../lib/navigation';
  import Cover from '../components/Cover.svelte';
  import Notice from '../components/Notice.svelte';
  import TrackList from '../components/TrackList.svelte';
  let {
    kind,
    id,
    player,
    add,
  }: {
    kind: 'artist' | 'album';
    id: string;
    player: PlayerState;
    add: (track: Track) => void;
  } = $props();
  let artist = $state<Artist>();
  let album = $state<Album>();
  let albums = $state<Album[]>([]);
  let tracks = $state<Track[]>([]);
  let loading = $state(true);
  let error = $state('');
  onMount(() => {
    const controller = new AbortController();
    const load = async () => {
      try {
        if (kind === 'artist') {
          const result = await api.library.getArtist(
            { id },
            { signal: controller.signal },
          );
          artist = result.artist;
          albums = result.albums;
        } else {
          const result = await api.library.getAlbum(
            { id },
            { signal: controller.signal },
          );
          album = result.album;
          tracks = result.tracks;
        }
      } catch (cause) {
        if (!controller.signal.aborted) error = errorMessage(cause);
      } finally {
        loading = false;
      }
    };
    void load();
    return () => controller.abort();
  });
</script>

<a
  class="back-link"
  href={link('library', { tab: kind === 'artist' ? 'artists' : 'albums' })}
  ><ArrowLeft size={14} /> {kind === 'artist' ? 'Artists' : 'Albums'}</a
>
<Notice message={error} />
{#if loading}<div class="loading-state" role="status">
    <span class="spinner"></span> Loading {kind}…
  </div>
{:else if artist || album}<header class="entity-header">
    <Cover
      name={artist?.name || album?.name || ''}
      artwork={artist?.artworkPath || album?.artworkPath}
    />
    <div>
      <p class="entity-type">{kind === 'artist' ? 'Artist' : 'Album'}</p>
      <h1>{artist?.name || album?.name}</h1>
      {#if album}<a
          class="entity-artist"
          href={link(`artist/${encodeURIComponent(album.artistId)}`)}
          >{album.artist}</a
        >
        <p class="entity-meta">
          {album.year ? `${album.year} · ` : ''}{tracks.length} songs · {duration(
            album.durationSeconds,
          )}{album.genre ? ` · ${album.genre}` : ''}
        </p>
        <button
          class="button primary"
          disabled={!tracks.length}
          onclick={() => player.play(tracks)}
          ><Play size={14} fill="currentColor" /> Play album</button
        >{:else}<p class="entity-meta">
          {albums.length}
          {albums.length === 1 ? 'album' : 'albums'} in your library
        </p>{/if}
    </div>
  </header>
  {#if album}<TrackList {tracks} {player} {add} />{:else}<div
      class="section-heading"
    >
      <h2>Albums</h2>
    </div>
    <div class="card-grid">
      {#each albums as item}<a
          class="music-card"
          href={link(`album/${encodeURIComponent(item.id)}`)}
          ><Cover name={item.name} artwork={item.artworkPath} /><strong
            >{item.name}</strong
          ><span>{item.year || `${item.trackCount} songs`}</span></a
        >{/each}
    </div>{/if}{/if}
