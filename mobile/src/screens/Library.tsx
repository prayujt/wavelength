import React, { useMemo, useState } from 'react';
import { FlatList, ScrollView, View, useWindowDimensions } from 'react-native';
import { router } from 'expo-router';
import { useQuery } from '@tanstack/react-query';
import { ArrowDownToLine, ChevronRight, Disc3, ListMusic, Plus } from 'lucide-react-native';
import type { Playlist } from '../gen/wavelength/v1/library_pb';
import { useAPI } from '../lib/session';
import { usePlaylists } from '../lib/queries';
import { color } from '../lib/theme';
import { Artwork } from '../components/artwork';
import { AlbumCard } from '../components/media';
import { DeviceLibraryLink } from '../components/device';
import {
  Empty,
  Failure,
  Header,
  IconButton,
  Loading,
  Press,
  Screen,
  SearchField,
  Section,
  Txt,
} from '../components/ui';

function PlaylistRow({ playlist }: { playlist: Playlist }) {
  return (
    <Press
      label={`Open playlist ${playlist.name}`}
      onPress={() => router.push({ pathname: '/playlist/[id]', params: { id: playlist.id } })}
      style={{
        flexDirection: 'row',
        alignItems: 'center',
        gap: 14,
        paddingVertical: 12,
        marginHorizontal: 24,
      }}
    >
      <Artwork title={playlist.name} id={playlist.id} size={60} radius={12} />
      <View style={{ flex: 1, gap: 6 }}>
        <Txt size={16} weight="medium" numberOfLines={1}>
          {playlist.name}
        </Txt>
        <Txt muted size={12}>
          {playlist.trackIds.length} songs
        </Txt>
      </View>
      <ChevronRight color={color.faint} size={18} />
    </Press>
  );
}

export function LibraryScreen() {
  const api = useAPI(),
    playlists = usePlaylists(),
    { width } = useWindowDimensions();
  const albums = useQuery({
    queryKey: ['library', 'albums', 'home'],
    queryFn: () => api.library.listAlbums({ limit: 12 }),
  });
  return (
    <Screen>
      <Header
        title="Your library"
        subtitle="Your favorites, a little closer."
        right={
          <IconButton
            icon={Plus}
            label="Create playlist"
            active
            onPress={() => router.push('/playlist-edit')}
          />
        }
      />
      <DeviceLibraryLink />
      <Section
        title="Your playlists"
        action="See all"
        onPress={() => router.navigate('/playlists')}
      >
        {playlists.isPending ? (
          <Loading rows={2} />
        ) : playlists.error ? (
          <Failure error={playlists.error} retry={playlists.refetch} />
        ) : playlists.data?.playlists.length ? (
          playlists.data.playlists
            .slice(0, 4)
            .map((playlist) => <PlaylistRow key={playlist.id} playlist={playlist} />)
        ) : (
          <Empty
            icon={ListMusic}
            title="Make it yours"
            text="Keep your favorites together for any mood."
            action="Create a playlist"
            onPress={() => router.push('/playlist-edit')}
          />
        )}
      </Section>
      {!!albums.data?.albums.length && (
        <Section
          title="From your collection"
          action="All albums"
          onPress={() => router.push({ pathname: '/library/catalog', params: { tab: 'Albums' } })}
        >
          <ScrollView
            horizontal
            showsHorizontalScrollIndicator={false}
            contentContainerStyle={{ gap: 16, paddingHorizontal: 24 }}
          >
            {albums.data.albums.slice(0, 8).map((album) => (
              <AlbumCard key={album.id} album={album} width={Math.min(160, width * 0.4)} />
            ))}
          </ScrollView>
        </Section>
      )}
      <View
        style={{
          marginHorizontal: 24,
          marginTop: 24,
          borderRadius: 18,
          backgroundColor: color.surface,
          overflow: 'hidden',
        }}
      >
        {[
          {
            title: 'Browse all music',
            detail: 'Albums, artists and songs',
            icon: Disc3,
            route: '/library/catalog' as const,
          },
          {
            title: 'Server downloads',
            detail: 'Imports and download activity',
            icon: ArrowDownToLine,
            route: '/activity' as const,
          },
        ].map(({ title, detail, icon: Icon, route }) => (
          <Press
            key={route}
            label={title}
            onPress={() => router.push(route)}
            style={{ flexDirection: 'row', alignItems: 'center', gap: 14, padding: 18 }}
          >
            <Icon size={22} color={color.accent} />
            <View style={{ flex: 1, gap: 5 }}>
              <Txt weight="medium">{title}</Txt>
              <Txt muted size={12}>
                {detail}
              </Txt>
            </View>
            <ChevronRight size={18} color={color.faint} />
          </Press>
        ))}
      </View>
    </Screen>
  );
}

export function PlaylistsScreen() {
  const playlists = usePlaylists(),
    [search, setSearch] = useState('');
  const items = useMemo(
    () =>
      (playlists.data?.playlists || []).filter((p) =>
        p.name.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()),
      ),
    [playlists.data, search],
  );
  return (
    <Screen scroll={false}>
      <FlatList
        data={items}
        keyExtractor={(p) => p.id}
        renderItem={({ item }) => <PlaylistRow playlist={item} />}
        showsVerticalScrollIndicator={false}
        contentContainerStyle={{ paddingBottom: 100 }}
        refreshing={playlists.isRefetching}
        onRefresh={() => {
          void playlists.refetch();
        }}
        ListHeaderComponent={
          <View>
            <Header
              title="Playlists"
              subtitle="A soundtrack for every part of your day."
              right={
                <IconButton
                  icon={Plus}
                  label="Create playlist"
                  active
                  onPress={() => router.push('/playlist-edit')}
                />
              }
            />
            <SearchField value={search} onChange={setSearch} placeholder="Find a playlist" />
            <View style={{ marginHorizontal: 24, paddingVertical: 18 }}>
              <Txt muted size={12}>
                {playlists.data ? `${playlists.data.playlists.length} playlists` : 'Your playlists'}
              </Txt>
            </View>
          </View>
        }
        ListEmptyComponent={
          playlists.isPending ? (
            <Loading />
          ) : playlists.error ? (
            <Failure error={playlists.error} retry={playlists.refetch} />
          ) : (
            <Empty
              icon={ListMusic}
              title={search ? 'No matching playlists' : 'Make it yours'}
              text={
                search ? 'Try another playlist name.' : 'Keep your favorites together for any mood.'
              }
              action={search ? undefined : 'Create a playlist'}
              onPress={() => router.push('/playlist-edit')}
            />
          )
        }
      />
    </Screen>
  );
}
