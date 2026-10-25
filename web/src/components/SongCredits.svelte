<script lang="ts">
  import type { Track } from '../gen/wavelength/v1/library_pb';
  import type { ExternalTrack } from '../gen/wavelength/v1/ingestion_pb';
  import { link, readRoute } from '../lib/navigation';
  let {
    track,
    mobileAlbum = false,
    artistID = '',
    albumID = '',
  }: {
    track: Track | ExternalTrack;
    mobileAlbum?: boolean;
    artistID?: string;
    albumID?: string;
  } = $props();
  const artist = $derived(
    artistID || ('artistId' in track ? track.artistId : ''),
  );
  const album = $derived(albumID || ('albumId' in track ? track.albumId : ''));
  const artistRef = $derived(
    'artistRef' in track ? track.artistRef : undefined,
  );
  const releaseRef = $derived(
    'releaseRef' in track ? track.releaseRef : undefined,
  );
  function externalLink(
    kind: 'artist' | 'release',
    ref: { provider: string; id: string },
  ) {
    const route = readRoute(window.location.hash);
    const context: Record<string, string> =
      route.page === 'discover'
        ? {
            q: route.query.get('q') || '',
            category: route.query.get('category') || 'all',
          }
        : {};
    return link(`discover/${kind}`, {
      ...context,
      provider: ref.provider,
      id: ref.id,
    });
  }
  const artistHref = $derived(
    artist
      ? link(`artist/${encodeURIComponent(artist)}`)
      : artistRef
        ? externalLink('artist', artistRef)
        : link('discover', { q: track.artist, category: 'artists' }),
  );
  const albumHref = $derived(
    album
      ? link(`album/${encodeURIComponent(album)}`)
      : releaseRef
        ? externalLink('release', releaseRef)
        : link('discover', {
            q: `${track.artist} ${track.album}`,
            category: 'releases',
          }),
  );
</script>

<span class="song-credits"
  ><a href={artistHref}>{track.artist || 'Unknown artist'}</a
  >{#if track.album}<span class:mobile-album={mobileAlbum}>
      · <a href={albumHref}>{track.album}</a></span
    >{/if}</span
>
