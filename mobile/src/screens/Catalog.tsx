import { usePageBottomInset } from '../lib/layout';
import React, { useState } from 'react';
import { FlatList, ScrollView, View, useWindowDimensions } from 'react-native';
import { useInfiniteQuery } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { RefreshCw, Library, Plus } from 'lucide-react-native';
import type { Album, Artist, Playlist, Track } from '../gen/wavelength/v1/library_pb';
import { useAPI } from '../lib/session';
import { usePlayer } from '../lib/player';
import { DeviceLibraryLink } from '../components/device';
import { AlbumCard, ArtistCard, PlaylistCard, TrackRow } from '../components/media';
import {
  Empty,
  Failure,
  Header,
  IconButton,
  Loading,
  Pill,
  Screen,
  SearchField,
  Txt,
  useDebounced,
} from '../components/ui';

const filters = ['Songs', 'Albums', 'Artists', 'Playlists'] as const;
type Filter = (typeof filters)[number];
type Item = Track | Album | Artist | Playlist;
export function CatalogScreen() {
  const bottom = usePageBottomInset();
  const params = useLocalSearchParams<{ tab?: string }>(),
    api = useAPI(),
    player = usePlayer();
  const filter: Filter = filters.includes(params.tab as Filter) ? (params.tab as Filter) : 'Songs';
  const setFilter = (tab: Filter) => router.setParams({ tab });
  const [search, setSearch] = useState('');
  const query = useDebounced(search.trim()),
    width = useWindowDimensions().width;
  const data = useInfiniteQuery({
    queryKey: [filter === 'Playlists' ? 'playlists' : 'library', filter, query],
    initialPageParam: 0,
    queryFn: async ({ pageParam }) => {
      if (filter === 'Songs') {
        const r = await api.library.listTracks({ query, offset: pageParam, limit: 80 });
        return { items: r.tracks as Item[], total: r.total };
      }
      if (filter === 'Albums') {
        const r = await api.library.listAlbums({ query, offset: pageParam, limit: 60 });
        return { items: r.albums as Item[], total: r.total };
      }
      if (filter === 'Artists') {
        const r = await api.library.listArtists({ query, offset: pageParam, limit: 60 });
        return { items: r.artists as Item[], total: r.total };
      }
      const r = await api.user.listPlaylists({ limit: 500 });
      const items = r.playlists.filter((p) => p.name.toLowerCase().includes(query.toLowerCase()));
      return { items, total: items.length };
    },
    getNextPageParam: (last, pages) => {
      const loaded = pages.reduce((n, p) => n + p.items.length, 0);
      return last.items.length && loaded < last.total ? loaded : undefined;
    },
  });
  const items = data.data?.pages.flatMap((p) => p.items) || [];
  const ordered = items;
  const columns = filter === 'Songs' ? 1 : width >= 740 ? 3 : 2,
    cardWidth = (width - 48 - (columns - 1) * 16) / columns;
  return (
    <Screen scroll={false}>
      <FlatList
        testID="catalog-scroll"
        key={columns}
        data={ordered}
        keyExtractor={(item) => item.id}
        numColumns={columns}
        showsVerticalScrollIndicator={false}
        contentContainerStyle={{ paddingBottom: bottom }}
        columnWrapperStyle={
          columns > 1 ? { paddingHorizontal: 24, gap: 16, marginBottom: 26 } : undefined
        }
        ListHeaderComponent={
          <View>
            <Header
              title="All music"
              subtitle="Browse your full collection."
              back
              right={
                <IconButton
                  icon={Plus}
                  label="Create playlist"
                  active
                  onPress={() => router.push('/playlist-edit')}
                />
              }
            />
            <ScrollView
              horizontal
              showsHorizontalScrollIndicator={false}
              contentContainerStyle={{
                paddingHorizontal: 24,
                gap: 8,
                paddingVertical: 6,
                marginBottom: 16,
              }}
            >
              {filters.map((f) => (
                <Pill key={f} title={f} active={f === filter} onPress={() => setFilter(f)} />
              ))}
            </ScrollView>
            <SearchField
              value={search}
              onChange={setSearch}
              placeholder={`Search your ${filter.toLowerCase()}`}
            />
            <DeviceLibraryLink />
            <View
              style={{
                flexDirection: 'row',
                justifyContent: 'space-between',
                alignItems: 'center',
                paddingHorizontal: 24,
                marginTop: 17,
                marginBottom: 14,
              }}
            >
              <Txt muted size={11}>
                {data.data
                  ? `${data.data.pages[0].total} ${filter.toLowerCase()}`
                  : 'Your collection'}
              </Txt>
              <IconButton
                icon={RefreshCw}
                label="Refresh library"
                size={18}
                disabled={data.isFetching}
                onPress={data.refetch}
              />
            </View>
          </View>
        }
        renderItem={({ item, index }) =>
          filter === 'Songs' ? (
            <TrackRow track={item as Track} onPlay={() => player.play(ordered as Track[], index)} />
          ) : filter === 'Albums' ? (
            <AlbumCard album={item as Album} width={cardWidth} />
          ) : filter === 'Artists' ? (
            <ArtistCard artist={item as Artist} width={cardWidth} />
          ) : (
            <PlaylistCard playlist={item as Playlist} width={cardWidth} />
          )
        }
        ListEmptyComponent={
          data.isPending ? (
            <Loading />
          ) : data.error ? (
            <Failure error={data.error} retry={data.refetch} />
          ) : (
            <Empty
              icon={Library}
              title={
                query
                  ? 'Nothing here by that name'
                  : filter === 'Playlists'
                    ? 'A good place to start'
                    : 'Room for something good'
              }
              text={
                query
                  ? 'Try another name or explore the catalog in Discover.'
                  : filter === 'Playlists'
                    ? 'Put a few favorites together. Make a soundtrack for anything.'
                    : 'Find a favorite in Discover and bring it into your collection.'
              }
              action={filter === 'Playlists' ? 'Create a playlist' : 'Discover music'}
              onPress={() => router.push(filter === 'Playlists' ? '/playlist-edit' : '/search')}
            />
          )
        }
        onEndReached={() => {
          if (data.hasNextPage && !data.isFetchingNextPage) void data.fetchNextPage();
        }}
        onEndReachedThreshold={0.5}
        ListFooterComponent={data.isFetchingNextPage ? <Loading rows={2} /> : null}
        refreshing={data.isRefetching && !data.isFetchingNextPage}
        onRefresh={() => {
          void data.refetch();
        }}
      />
    </Screen>
  );
}
