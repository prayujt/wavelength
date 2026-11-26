import { ScrollView, StyleSheet, View, useWindowDimensions } from 'react-native';
import { router } from 'expo-router';
import { useQuery } from '@tanstack/react-query';
import { Clock3, Disc3, Download, Music2, Settings, Users } from 'lucide-react-native';
import type { Track } from '../gen/wavelength/v1/library_pb';
import { useAPI, useSession } from '../lib/session';
import { usePlaylists } from '../lib/queries';
import { usePlayer } from '../lib/player';
import { color } from '../lib/theme';
import { AlbumCard, PlaylistCard, TrackRow } from '../components/media';
import { Equalizer } from '../components/equalizer';
import { Header, IconButton, Press, Screen, Section, Txt } from '../components/ui';

const libraryLinks = [
  { tab: 'Songs', icon: Music2 },
  { tab: 'Albums', icon: Disc3 },
  { tab: 'Artists', icon: Users },
] as const;

export function HomeScreen() {
  const player = usePlayer();
  const api = useAPI();
  const { preview } = useSession();
  const playlists = usePlaylists();
  const { width } = useWindowDimensions();
  const albums = useQuery({
    queryKey: ['library', 'albums', 'home'],
    queryFn: ({ signal }) => api.library.listAlbums({ limit: 12 }, { signal }),
  });
  const listening = useQuery({
    queryKey: ['listening', 'home'],
    queryFn: ({ signal }) => api.listening.listListeningSessions({ limit: 24 }, { signal }),
  });
  const recent = new Map<string, Track>();
  for (const { track } of listening.data?.sessions ?? []) {
    if (track?.id && !recent.has(track.id)) recent.set(track.id, track);
    if (recent.size === 5) break;
  }
  const suggestions = useQuery({
    queryKey: ['library', 'tracks', 'home'],
    queryFn: ({ signal }) => api.library.listTracks({ limit: 5 }, { signal }),
    enabled: !listening.isPending && recent.size === 0,
  });
  const tracks = recent.size ? [...recent.values()] : (suggestions.data?.tracks ?? []);
  const cardWidth = Math.min(172, Math.max(128, width * 0.37));

  return (
    <Screen>
      <Header
        title="Home"
        right={
          <View style={s.headerActions}>
            {preview && (
              <Txt muted size={11} weight="bold">
                Preview
              </Txt>
            )}
            <IconButton
              icon={Clock3}
              label="Listening history"
              onPress={() =>
                router.navigate({ pathname: '/activity', params: { section: 'listening' } })
              }
            />
            <IconButton
              icon={Settings}
              label="Open settings"
              onPress={() => router.push('/settings')}
            />
          </View>
        }
      />
      <View style={s.shortcuts}>
        {libraryLinks.map(({ tab, icon: Icon }) => (
          <View key={tab} style={s.shortcutCell}>
            <Press
              label={`Browse ${tab.toLowerCase()}`}
              onPress={() => router.navigate({ pathname: '/library', params: { tab } })}
              style={s.shortcut}
            >
              <Icon size={19} color={color.accent} strokeWidth={1.8} />
              <Txt size={14} weight="bold">
                {tab}
              </Txt>
            </Press>
          </View>
        ))}
        <View style={s.shortcutCell}>
          <Press
            label="Downloads"
            onPress={() =>
              router.navigate({ pathname: '/activity', params: { section: 'downloads' } })
            }
            style={s.shortcut}
          >
            <Download size={19} color={color.accent} strokeWidth={1.8} />
            <Txt size={14} weight="bold">
              Downloads
            </Txt>
          </Press>
        </View>
      </View>
      <Press
        label={player.playFeedEnabled ? 'Turn off play feed from Home' : 'Start play feed'}
        selected={player.playFeedEnabled}
        onPress={player.togglePlayFeed}
        style={s.feed}
      >
        <Equalizer color={player.playFeedEnabled ? color.accent : color.muted} />
        <Txt size={13} weight="bold" style={{ flex: 1 }}>
          Play feed
        </Txt>
        <Txt muted size={12} style={player.playFeedEnabled ? { color: color.accent } : undefined}>
          {player.playFeedEnabled ? 'On' : 'Start'}
        </Txt>
      </Press>
      {tracks.length > 0 && (
        <Section
          title={recent.size ? 'Recently played' : 'Songs in your library'}
          action={recent.size ? 'History' : 'See all songs'}
          onPress={() =>
            recent.size
              ? router.navigate({ pathname: '/activity', params: { section: 'listening' } })
              : router.navigate({ pathname: '/library', params: { tab: 'Songs' } })
          }
        >
          {tracks.map((track, index) => (
            <TrackRow key={track.id} track={track} onPlay={() => player.play(tracks, index)} />
          ))}
        </Section>
      )}
      {albums.data?.albums.length ? (
        <Section
          title="Albums"
          action="See all albums"
          onPress={() => router.navigate({ pathname: '/library', params: { tab: 'Albums' } })}
        >
          <ScrollView
            horizontal
            showsHorizontalScrollIndicator={false}
            contentContainerStyle={s.shelf}
          >
            {albums.data.albums.map((album) => (
              <AlbumCard key={album.id} album={album} width={cardWidth} />
            ))}
          </ScrollView>
        </Section>
      ) : null}
      <Section
        title="Playlists"
        action="See all playlists"
        onPress={() => router.navigate('/playlists')}
      >
        {playlists.data?.playlists.length ? (
          <ScrollView
            horizontal
            showsHorizontalScrollIndicator={false}
            contentContainerStyle={s.shelf}
          >
            {playlists.data.playlists.slice(0, 8).map((playlist) => (
              <PlaylistCard key={playlist.id} playlist={playlist} width={cardWidth} />
            ))}
          </ScrollView>
        ) : playlists.isSuccess ? (
          <Press
            label="Create playlist"
            onPress={() => router.push('/playlist-edit')}
            style={[s.shortcut, { marginHorizontal: 24 }]}
          >
            <Txt weight="bold" size={14}>
              Create playlist
            </Txt>
          </Press>
        ) : null}
      </Section>
    </Screen>
  );
}

const s = StyleSheet.create({
  headerActions: { flexDirection: 'row', alignItems: 'center', gap: 2 },
  shortcuts: { paddingHorizontal: 24, flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  shortcutCell: { width: '48%', flexGrow: 1 },
  shortcut: {
    minHeight: 52,
    borderRadius: 10,
    backgroundColor: color.surface,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
    paddingHorizontal: 13,
  },
  feed: {
    minHeight: 44,
    marginHorizontal: 24,
    marginTop: 12,
    paddingHorizontal: 3,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: color.line,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
  },
  shelf: { gap: 14, paddingHorizontal: 24 },
});
