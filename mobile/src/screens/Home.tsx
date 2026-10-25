import React from 'react';
import { ScrollView, View, useWindowDimensions } from 'react-native';
import { LinearGradient } from 'expo-linear-gradient';
import { router } from 'expo-router';
import { useQuery } from '@tanstack/react-query';
import {
  ArrowDownToLine,
  ArrowUpRight,
  Headphones,
  ListMusic,
  Play,
  Plus,
} from 'lucide-react-native';
import { useAPI, useSession } from '../lib/session';
import { usePlaylists, useUser } from '../lib/queries';
import { usePlayer } from '../lib/player';
import { color, colors } from '../lib/theme';
import { AlbumCard, PlaylistCard, TrackRow } from '../components/media';
import { Artwork } from '../components/artwork';
import {
  Button,
  FadeIn,
  Press,
  Screen,
  Section,
  Txt,
  WaveMark,
  Loading,
  Failure,
  Empty,
} from '../components/ui';

export function HomeScreen() {
  const api = useAPI(),
    session = useSession(),
    player = usePlayer(),
    user = useUser(),
    playlists = usePlaylists(),
    width = useWindowDimensions().width;
  const albums = useQuery({
    queryKey: ['library', 'albums', 'home'],
    queryFn: () => api.library.listAlbums({ limit: 12 }),
  });
  const tracks = useQuery({
    queryKey: ['library', 'tracks', 'home'],
    queryFn: () => api.library.listTracks({ limit: 8 }),
  });
  const featured = albums.data?.albums[0],
    tint = colors(featured?.id || 'album-0');
  const hour = new Date().getHours(),
    greeting = hour < 12 ? 'Good morning' : hour < 18 ? 'Good afternoon' : 'Good evening';
  const firstName = user.data?.user?.name.split(' ')[0];
  const cardWidth = Math.min(165, Math.max(140, width * 0.4));
  async function playFeatured() {
    if (!featured) return;
    const result = await api.library.getAlbum({ id: featured.id });
    await player.play(result.tracks);
  }
  return (
    <Screen>
      <FadeIn>
        <View
          style={{
            flexDirection: 'row',
            justifyContent: 'space-between',
            alignItems: 'center',
            paddingHorizontal: 24,
            paddingTop: 14,
            marginBottom: 25,
          }}
        >
          <View style={{ flexDirection: 'row', gap: 8, alignItems: 'center' }}>
            <WaveMark size={25} />
            <Txt weight="display" size={20} style={{ letterSpacing: -0.8 }}>
              wavelength
            </Txt>
            {session.preview && (
              <View
                style={{
                  backgroundColor: color.raised,
                  paddingHorizontal: 7,
                  paddingVertical: 4,
                  borderRadius: 6,
                }}
              >
                <Txt size={8} weight="bold" style={{ color: color.accent, letterSpacing: 1 }}>
                  PREVIEW
                </Txt>
              </View>
            )}
          </View>
          <Press
            label="Open settings"
            onPress={() => router.push('/settings')}
            style={{
              width: 39,
              height: 39,
              borderRadius: 20,
              backgroundColor: color.raised,
              borderWidth: 1,
              borderColor: color.line,
              alignItems: 'center',
              justifyContent: 'center',
            }}
          >
            <Txt weight="bold" size={14} style={{ color: color.accent }}>
              {firstName?.[0] || 'W'}
            </Txt>
          </Press>
        </View>
        <View style={{ paddingHorizontal: 24, gap: 5, marginBottom: 23 }}>
          <Txt muted size={13}>
            {greeting}
            {firstName ? `, ${firstName}` : ''}.
          </Txt>
          <Txt
            accessibilityRole="header"
            weight="heavy"
            size={34}
            style={{ letterSpacing: -1.4, lineHeight: 42 }}
          >
            Find your rhythm.
          </Txt>
        </View>
      </FadeIn>
      {albums.isPending ? (
        <Loading rows={3} />
      ) : albums.error ? (
        <Failure error={albums.error} retry={albums.refetch} />
      ) : featured ? (
        <FadeIn delay={80} style={{ marginHorizontal: 24 }}>
          <Press
            label={`Explore ${featured.name}`}
            onPress={() => router.push({ pathname: '/album/[id]', params: { id: featured.id } })}
            style={{
              borderRadius: 24,
              overflow: 'hidden',
              backgroundColor: tint[0],
              minHeight: 295,
            }}
          >
            <View
              style={{
                position: 'absolute',
                right: -30,
                top: -20,
                transform: [{ rotate: '14deg' }],
              }}
            >
              <Artwork
                title={featured.name}
                id={featured.id}
                path={featured.artworkPath}
                size={240}
                radius={14}
              />
            </View>
            <LinearGradient
              colors={[tint[0] + '30', tint[0] + 'CC', tint[0]]}
              start={{ x: 0.1, y: 0 }}
              end={{ x: 0.4, y: 1 }}
              style={{ position: 'absolute', inset: 0 }}
            />
            <View style={{ padding: 24, minHeight: 295, justifyContent: 'space-between' }}>
              <View
                style={{
                  flexDirection: 'row',
                  justifyContent: 'space-between',
                  alignItems: 'center',
                }}
              >
                <View
                  style={{
                    backgroundColor: '#FFFFFF18',
                    paddingHorizontal: 11,
                    paddingVertical: 6,
                    borderRadius: 20,
                    flexDirection: 'row',
                    gap: 6,
                    alignItems: 'center',
                  }}
                >
                  <Headphones size={12} color={tint[2]} />
                  <Txt size={9} weight="bold" style={{ letterSpacing: 1.2, color: tint[2] }}>
                    FROM YOUR COLLECTION
                  </Txt>
                </View>
                <ArrowUpRight size={20} color={tint[2]} />
              </View>
              <View style={{ marginTop: 60, gap: 11, alignItems: 'flex-start' }}>
                <Txt
                  size={32}
                  weight="heavy"
                  numberOfLines={2}
                  style={{ letterSpacing: -1.3, lineHeight: 37, maxWidth: '85%' }}
                >
                  {featured.name}
                </Txt>
                <Txt size={13} style={{ color: tint[2] }}>
                  {featured.artist} · {featured.trackCount} songs
                </Txt>
                <View style={{ marginTop: 6 }}>
                  <Button
                    title="Press play. Stay awhile."
                    small
                    icon={Play}
                    onPress={playFeatured}
                    style={{ backgroundColor: '#F5F3EA' }}
                  />
                </View>
              </View>
            </View>
          </Press>
        </FadeIn>
      ) : (
        <Empty
          title="Your collection starts here"
          text="Find a favorite, add it to your library, and make yourself at home."
          action="Discover music"
          onPress={() => router.push('/search')}
        />
      )}
      <FadeIn delay={140}>
        <View style={{ flexDirection: 'row', paddingHorizontal: 24, marginTop: 18, gap: 10 }}>
          {[
            { title: 'Your playlists', icon: ListMusic, route: '/library' as const },
            { title: 'Downloads', icon: ArrowDownToLine, route: '/activity' as const },
          ].map(({ title, icon: Icon, route }) => (
            <View key={title} style={{ flex: 1 }}>
              <Press
                label={title}
                onPress={() => router.push(route)}
                style={{
                  backgroundColor: color.surface,
                  borderRadius: 15,
                  minHeight: 58,
                  paddingHorizontal: 15,
                  flexDirection: 'row',
                  alignItems: 'center',
                  gap: 10,
                }}
              >
                <Icon size={18} strokeWidth={1.5} color={color.accent} />
                <Txt size={12} weight="medium">
                  {title}
                </Txt>
              </Press>
            </View>
          ))}
        </View>
      </FadeIn>
      {albums.data && albums.data.albums.length > 0 && (
        <Section
          title="A collection worth keeping"
          eyebrow="YOUR ALBUMS"
          action="See all"
          onPress={() => router.push({ pathname: '/library', params: { tab: 'Albums' } })}
        >
          <ScrollView
            horizontal
            showsHorizontalScrollIndicator={false}
            contentContainerStyle={{ gap: 15, paddingHorizontal: 24 }}
          >
            {albums.data.albums
              .slice(1)
              .concat(albums.data.albums.slice(0, 1))
              .map((album) => (
                <AlbumCard key={album.id} album={album} width={cardWidth} />
              ))}
          </ScrollView>
        </Section>
      )}
      <Section
        title="Made by you"
        eyebrow="YOUR PLAYLISTS"
        action="Create new"
        onPress={() => router.push('/playlist-edit')}
      >
        {playlists.data?.playlists.length ? (
          <ScrollView
            horizontal
            showsHorizontalScrollIndicator={false}
            contentContainerStyle={{ gap: 15, paddingHorizontal: 24 }}
          >
            {playlists.data.playlists.slice(0, 8).map((playlist) => (
              <PlaylistCard key={playlist.id} playlist={playlist} width={cardWidth} />
            ))}
          </ScrollView>
        ) : (
          <View style={{ paddingHorizontal: 24 }}>
            <Press
              label="Create your first playlist"
              onPress={() => router.push('/playlist-edit')}
              style={{
                borderWidth: 1,
                borderStyle: 'dashed',
                borderColor: color.line,
                borderRadius: 18,
                padding: 24,
                flexDirection: 'row',
                alignItems: 'center',
                gap: 15,
              }}
            >
              <Plus size={25} color={color.accent} />
              <View style={{ gap: 4 }}>
                <Txt weight="medium">Make something your own.</Txt>
                <Txt muted size={12}>
                  Start with a song. See where it goes.
                </Txt>
              </View>
            </Press>
          </View>
        )}
      </Section>
      {!!tracks.data?.tracks.length && (
        <Section
          title="Good music, close at hand"
          eyebrow="IN YOUR LIBRARY"
          action="All songs"
          onPress={() => router.push('/library')}
        >
          {tracks.data.tracks.slice(0, 5).map((track, i) => (
            <TrackRow
              key={track.id}
              track={track}
              onPlay={() => player.play(tracks.data!.tracks, i)}
            />
          ))}
        </Section>
      )}
      <View style={{ paddingHorizontal: 24, marginTop: 36 }}>
        <LinearGradient
          colors={['#1F3228', '#14201C']}
          style={{ padding: 24, borderRadius: 20, gap: 12 }}
        >
          <WaveMark size={28} />
          <Txt size={23} weight="display" style={{ letterSpacing: -0.6 }}>
            There’s more out there.
          </Txt>
          <Txt muted size={13} style={{ lineHeight: 21 }}>
            Find your next favorite. Bring it home to your collection.
          </Txt>
          <View style={{ alignItems: 'flex-start', marginTop: 3 }}>
            <Button
              title="Go exploring"
              secondary
              small
              icon={ArrowUpRight}
              onPress={() => router.push('/search')}
            />
          </View>
        </LinearGradient>
      </View>
    </Screen>
  );
}
