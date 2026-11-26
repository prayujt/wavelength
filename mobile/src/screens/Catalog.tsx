import { usePageBottomInset } from '../lib/layout';
import React, { useState } from 'react';
import { FlatList, StyleSheet, View, useWindowDimensions } from 'react-native';
import { useInfiniteQuery } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { Download, RefreshCw, Library, Plus } from 'lucide-react-native';
import type { Album, Artist, Playlist, Track } from '../gen/wavelength/v1/library_pb';
import { useAPI } from '../lib/session';
import { usePlayer } from '../lib/player';
import { color } from '../lib/theme';
import { DeviceLibraryLink } from '../components/device';
import { AlbumCard, ArtistCard, PlaylistCard, TrackRow } from '../components/media';
import {
  Empty,
  Failure,
  Header,
  IconButton,
  Loading,
  Press,
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
    queryFn: async ({ pageParam, signal }) => {
      if (filter === 'Songs') {
        const r = await api.library.listTracks({ query, offset: pageParam, limit: 80 }, { signal });
        return { items: r.tracks as Item[], total: r.total };
      }
      if (filter === 'Albums') {
        const r = await api.library.listAlbums({ query, offset: pageParam, limit: 60 }, { signal });
        return { items: r.albums as Item[], total: r.total };
      }
      if (filter === 'Artists') {
        const r = await api.library.listArtists(
          { query, offset: pageParam, limit: 60 },
          { signal },
        );
        return { items: r.artists as Item[], total: r.total };
      }
      const r = await api.user.listPlaylists({ limit: 500 }, { signal });
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
  const columns = filter === 'Songs' ? 1 : width >= 940 ? 4 : width >= 620 ? 3 : 2,
    cardWidth = (width - 48 - (columns - 1) * 16) / columns;
  const total = data.data?.pages[0].total;
  return (
    <Screen scroll={false}>
      <FlatList
        testID="catalog-scroll"
        key={filter + columns}
        data={ordered}
        keyExtractor={(item) => item.id}
        numColumns={columns}
        showsVerticalScrollIndicator={false}
        keyboardShouldPersistTaps="handled"
        contentContainerStyle={{ paddingBottom: bottom }}
        columnWrapperStyle={
          columns > 1 ? { paddingHorizontal: 24, gap: 16, marginBottom: 26 } : undefined
        }
        ListHeaderComponent={
          <View>
            <Header
              title="Your library"
              right={
                <View style={{ flexDirection: 'row' }}>
                  <IconButton
                    icon={Download}
                    label="Server downloads"
                    onPress={() =>
                      router.navigate({ pathname: '/activity', params: { section: 'downloads' } })
                    }
                  />
                  <IconButton
                    icon={Plus}
                    label="Create playlist"
                    onPress={() => router.push('/playlist-edit')}
                  />
                </View>
              }
            />
            <SearchField
              value={search}
              onChange={setSearch}
              placeholder={`Search your ${filter.toLowerCase()}`}
            />
            <View style={s.filters}>
              {filters.map((name) => (
                <Press
                  key={name}
                  label={name}
                  selected={name === filter}
                  onPress={() => setFilter(name)}
                  style={[s.filter, name === filter && s.selected]}
                >
                  <Txt
                    size={13}
                    weight="bold"
                    style={{
                      textAlign: 'center',
                      color: name === filter ? color.accent : color.muted,
                    }}
                  >
                    {name}
                  </Txt>
                </Press>
              ))}
            </View>
            <DeviceLibraryLink />
            <View
              style={{
                flexDirection: 'row',
                justifyContent: 'space-between',
                alignItems: 'center',
                paddingHorizontal: 24,
                marginTop: 6,
                marginBottom: 4,
              }}
            >
              <Txt muted size={11}>
                {total !== undefined
                  ? `${total} ${total === 1 ? filter.toLowerCase().replace(/s$/, '') : filter.toLowerCase()}`
                  : ''}
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
                  ? 'No matches'
                  : filter === 'Playlists'
                    ? 'No playlists yet'
                    : `No ${filter.toLowerCase()} yet`
              }
              text={
                query
                  ? 'Try a different search.'
                  : filter === 'Playlists'
                    ? 'Create a playlist to save songs together.'
                    : 'Find music in Search and add it to your library.'
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

const s = StyleSheet.create({
  filters: {
    marginHorizontal: 24,
    marginTop: 12,
    borderRadius: 10,
    borderWidth: 1,
    borderColor: color.line,
    backgroundColor: color.surface,
    padding: 3,
    gap: 2,
    flexDirection: 'row',
  },
  filter: {
    flex: 1,
    minHeight: 44,
    justifyContent: 'center',
    alignItems: 'center',
    paddingHorizontal: 2,
    borderRadius: 7,
  },
  selected: { backgroundColor: color.raised },
});
