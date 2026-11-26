import { usePageBottomInset } from '../lib/layout';
import React, { useMemo, useState } from 'react';
import { FlatList, View } from 'react-native';
import { router } from 'expo-router';
import { ChevronRight, ListMusic, Plus } from 'lucide-react-native';
import type { Playlist } from '../gen/wavelength/v1/library_pb';
import { usePlaylists } from '../lib/queries';
import { color } from '../lib/theme';
import { PlaylistArtwork } from '../components/artwork';
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
        paddingVertical: 8,
        marginHorizontal: 24,
      }}
    >
      <PlaylistArtwork playlist={playlist} size={50} radius={8} />
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

export function PlaylistsScreen() {
  const bottom = usePageBottomInset();
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
        testID="playlists-scroll"
        data={items}
        keyExtractor={(p) => p.id}
        renderItem={({ item }) => <PlaylistRow playlist={item} />}
        showsVerticalScrollIndicator={false}
        contentContainerStyle={{ paddingBottom: bottom }}
        refreshing={playlists.isRefetching}
        onRefresh={() => {
          void playlists.refetch();
        }}
        ListHeaderComponent={
          <View>
            <Header
              title="Playlists"
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
              title={search ? 'No matching playlists' : 'No playlists yet'}
              text={
                search ? 'Try another playlist name.' : 'Create a playlist to save songs together.'
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
