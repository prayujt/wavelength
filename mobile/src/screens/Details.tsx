import { usePageBottomInset } from '../lib/layout';
import React, { useEffect } from 'react';
import { FlatList, View, useWindowDimensions } from 'react-native';
import { router, useLocalSearchParams } from 'expo-router';
import { useQuery } from '@tanstack/react-query';
import { create } from '@bufbuild/protobuf';
import { ArrowLeft, Edit3, Play, Plus, Share2, Shuffle } from 'lucide-react-native';
import { LinearGradient } from 'expo-linear-gradient';
import { TrackSchema, type Track } from '../gen/wavelength/v1/library_pb';
import { useAPI } from '../lib/session';
import { usePlayer } from '../lib/player';
import { useTracks, useUser } from '../lib/queries';
import { color, colors } from '../lib/theme';
import { duration, shuffled } from '../core/queue';
import { Artwork } from '../components/artwork';
import { AlbumCard, TrackRow, useMediaMenu } from '../components/media';
import { DeviceDownload } from '../components/device';
import {
  Button,
  Empty,
  FadeIn,
  Failure,
  IconButton,
  Loading,
  Press,
  Screen,
  Section,
  Txt,
} from '../components/ui';

function CollectionHero({
  title,
  subtitle,
  id,
  path,
  kind,
  extra,
  children,
}: {
  title: string;
  subtitle: string;
  id: string;
  path?: string;
  kind: string;
  extra?: string;
  children?: React.ReactNode;
}) {
  const width = useWindowDimensions().width,
    tint = colors(id),
    size = Math.min(240, width * 0.57);
  return (
    <FadeIn>
      <View style={{ paddingHorizontal: 24, paddingTop: 10, gap: 24 }}>
        <LinearGradient
          colors={[tint[0] + 'A0', color.bg]}
          style={{ position: 'absolute', top: -100, left: 0, right: 0, height: 450 }}
        />
        <View style={{ marginLeft: -10 }}>
          <IconButton
            icon={ArrowLeft}
            label="Go back"
            onPress={() => (router.canGoBack() ? router.back() : router.replace('/library'))}
          />
        </View>
        <View style={{ alignItems: 'center', paddingBottom: 5 }}>
          <View style={{ boxShadow: '0px 16px 36px #00000055' }}>
            <Artwork
              title={title}
              id={id}
              path={path}
              size={size}
              radius={kind === 'ARTIST' ? size / 2 : 14}
            />
          </View>
        </View>
        <View style={{ gap: 9 }}>
          <Txt size={10} weight="bold" style={{ color: color.accent, letterSpacing: 1.7 }}>
            {kind}
          </Txt>
          <Txt
            accessibilityRole="header"
            size={33}
            weight="heavy"
            style={{ letterSpacing: -1.2, lineHeight: 41 }}
          >
            {title}
          </Txt>
          <Txt size={14} muted>
            {subtitle}
          </Txt>
          {extra && (
            <Txt size={12} muted>
              {extra}
            </Txt>
          )}
        </View>
        {children}
      </View>
    </FadeIn>
  );
}
function CollectionActions({
  tracks,
  onShare,
  edit,
  downloading = tracks,
}: {
  tracks: Track[];
  onShare: () => unknown;
  edit?: () => unknown;
  downloading?: Track[];
}) {
  const player = usePlayer();
  return (
    <View
      style={{
        flexDirection: 'row',
        justifyContent: 'space-between',
        alignItems: 'center',
        paddingHorizontal: 24,
        marginVertical: 22,
      }}
    >
      <View style={{ flexDirection: 'row', gap: 5 }}>
        <DeviceDownload tracks={downloading} />
        {edit && <IconButton icon={Edit3} label="Edit playlist" onPress={edit} />}
        <IconButton
          icon={Share2}
          label="Share collection"
          onPress={onShare}
          disabled={!tracks.length}
        />
        <IconButton
          icon={Shuffle}
          label="Shuffle collection"
          onPress={() => player.play(shuffled(tracks))}
          disabled={!tracks.length}
        />
      </View>
      <Button
        title="Play"
        icon={Play}
        onPress={() => player.play(tracks)}
        disabled={!tracks.length}
      />
    </View>
  );
}
export function AlbumScreen() {
  const { id } = useLocalSearchParams<{ id: string }>(),
    api = useAPI(),
    player = usePlayer(),
    menu = useMediaMenu();
  const data = useQuery({ queryKey: ['album', id], queryFn: () => api.library.getAlbum({ id }) });
  if (data.isPending)
    return (
      <Screen>
        <Loading rows={7} />
      </Screen>
    );
  if (data.error || !data.data.album)
    return (
      <Screen>
        <Failure
          error={data.error || new Error('This album is no longer in the library.')}
          retry={data.refetch}
        />
      </Screen>
    );
  const { album, tracks } = data.data;
  return (
    <TrackCollection
      tracks={tracks}
      onPlay={(i) => player.play(tracks, i)}
      header={
        <>
          <CollectionHero
            id={album.id}
            title={album.name}
            path={album.artworkPath}
            subtitle={album.artist}
            kind="ALBUM"
            extra={`${album.year || 'Year unknown'} · ${tracks.length} songs · ${duration(album.durationSeconds)}`}
          >
            <Press
              label={`View artist ${album.artist}`}
              onPress={() =>
                router.push({ pathname: '/artist/[id]', params: { id: album.artistId } })
              }
              style={{ alignSelf: 'flex-start', paddingVertical: 5 }}
            >
              <Txt size={12} style={{ color: color.accent }}>
                More from {album.artist} →
              </Txt>
            </Press>
          </CollectionHero>
          <CollectionActions
            tracks={tracks}
            onShare={() =>
              menu.share(
                tracks.map((t) => t.id),
                album.name,
              )
            }
          />
        </>
      }
      footer={
        <View style={{ paddingHorizontal: 24, marginTop: 28 }}>
          <Txt muted size={11}>
            {album.genre || 'Music'} · From your Wavelength collection
          </Txt>
        </View>
      }
    />
  );
}

export function ArtistScreen() {
  const { id } = useLocalSearchParams<{ id: string }>(),
    api = useAPI(),
    player = usePlayer(),
    width = useWindowDimensions().width;
  const data = useQuery({ queryKey: ['artist', id], queryFn: () => api.library.getArtist({ id }) });
  if (data.isPending)
    return (
      <Screen>
        <Loading rows={7} />
      </Screen>
    );
  if (data.error || !data.data.artist)
    return (
      <Screen>
        <Failure
          error={data.error || new Error('This artist is no longer in your library.')}
          retry={data.refetch}
        />
      </Screen>
    );
  const { artist, albums } = data.data;
  async function playArtist() {
    const tracks: Track[] = [];
    for (const album of albums)
      tracks.push(...(await api.library.getAlbum({ id: album.id })).tracks);
    await player.play(tracks);
  }
  return (
    <Screen>
      <CollectionHero
        kind="ARTIST"
        id={artist.id}
        title={artist.name}
        path={artist.artworkPath}
        subtitle={`${albums.length} ${albums.length === 1 ? 'album' : 'albums'} in your collection`}
      />
      <View style={{ margin: 24, alignItems: 'flex-start' }}>
        <Button title="Play artist" icon={Play} onPress={playArtist} disabled={!albums.length} />
      </View>
      <Section title="Albums in your library">
        <View style={{ paddingHorizontal: 24, flexDirection: 'row', flexWrap: 'wrap', gap: 17 }}>
          {albums.map((album) => (
            <View key={album.id} style={{ marginBottom: 16 }}>
              <AlbumCard album={album} width={(width - 65) / 2} />
            </View>
          ))}
        </View>
      </Section>
      <View style={{ padding: 24 }}>
        <Button
          title="Find more in Discover"
          secondary
          onPress={() => router.push({ pathname: '/search', params: { q: artist.name } })}
        />
      </View>
    </Screen>
  );
}
export function PlaylistScreen() {
  const { id } = useLocalSearchParams<{ id: string }>(),
    api = useAPI(),
    player = usePlayer(),
    user = useUser(),
    menu = useMediaMenu();
  const data = useQuery({
    queryKey: ['playlist', id],
    queryFn: () => api.user.getPlaylist({ id }),
  });
  const tracks = useTracks(data.data?.playlist?.trackIds || []);
  const hydrate = player.hydrate;
  useEffect(() => {
    hydrate(tracks.data || []);
  }, [tracks.data, hydrate]);
  if (data.isPending)
    return (
      <Screen>
        <Loading rows={7} />
      </Screen>
    );
  if (data.error || !data.data.playlist)
    return (
      <Screen>
        <Failure
          error={data.error || new Error('This playlist is no longer available.')}
          retry={data.refetch}
        />
      </Screen>
    );
  const playlist = data.data.playlist,
    items = tracks.data || [],
    own = user.data?.user?.id === playlist.ownerId;
  const queued = tracks.isFetching
    ? playlist.trackIds.map(
        (id) => items.find((track) => track.id === id) || create(TrackSchema, { id }),
      )
    : items;
  return (
    <TrackCollection
      tracks={items}
      onPlay={(i) => {
        if (!tracks.isFetching) return player.play(queued, i);
        const id = items[i].id,
          occurrence = items.slice(0, i).filter((track) => track.id === id).length;
        let seen = -1;
        const index = playlist.trackIds.findIndex(
          (trackId) => trackId === id && ++seen === occurrence,
        );
        return player.play(queued, index);
      }}
      header={
        <>
          <CollectionHero
            kind={playlist.public ? 'SHARED PLAYLIST' : 'YOUR PLAYLIST'}
            id={playlist.id}
            title={playlist.name}
            subtitle={playlist.comment || 'A soundtrack, entirely your own.'}
            extra={`${playlist.trackIds.length} songs · ${duration(items.reduce((sum, t) => sum + t.durationSeconds, 0))}`}
          />
          <CollectionActions
            tracks={queued}
            downloading={tracks.isFetching ? [] : items}
            onShare={() => menu.share(playlist.trackIds, playlist.name)}
            edit={
              own ? () => router.push({ pathname: '/playlist-edit', params: { id } }) : undefined
            }
          />
          {tracks.isFetching && items.length > 0 && items.length < playlist.trackIds.length && (
            <View style={{ paddingHorizontal: 24, paddingBottom: 14 }}>
              <Txt muted size={12}>
                Loading the rest of your playlist…
              </Txt>
            </View>
          )}
          {!tracks.isFetching && !tracks.error && items.length < playlist.trackIds.length && (
            <View style={{ paddingHorizontal: 24, paddingBottom: 14 }}>
              <Txt muted size={12}>
                Some songs are no longer available in the library.
              </Txt>
            </View>
          )}
        </>
      }
      empty={
        tracks.isPending ? (
          <Loading />
        ) : tracks.error ? (
          <Failure error={tracks.error} retry={tracks.refetch} />
        ) : (
          <Empty
            icon={Plus}
            title="Every good playlist starts somewhere"
            text="Add songs using their more menu, or discover something new."
            action="Find some music"
            onPress={() => router.push('/search')}
          />
        )
      }
    />
  );
}
function TrackCollection({
  tracks,
  header,
  footer,
  empty,
  onPlay,
}: {
  tracks: Track[];
  header: React.ReactElement;
  footer?: React.ReactElement;
  empty?: React.ReactElement;
  onPlay(index: number): unknown;
}) {
  const bottom = usePageBottomInset();
  return (
    <Screen scroll={false}>
      <FlatList
        testID="collection-scroll"
        bounces={false}
        overScrollMode="never"
        data={tracks}
        keyExtractor={(track, index) => `${track.id}:${index}`}
        renderItem={({ item, index }) => (
          <TrackRow track={item} index={index} onPlay={() => onPlay(index)} />
        )}
        ListHeaderComponent={header}
        ListFooterComponent={footer}
        ListEmptyComponent={empty}
        contentContainerStyle={{ paddingBottom: bottom }}
        showsVerticalScrollIndicator={false}
        initialNumToRender={12}
        maxToRenderPerBatch={12}
        windowSize={9}
      />
    </Screen>
  );
}
