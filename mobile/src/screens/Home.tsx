import React from 'react';
import { ScrollView, View, useWindowDimensions } from 'react-native';
import { LinearGradient } from 'expo-linear-gradient';
import { router } from 'expo-router';
import { useQuery } from '@tanstack/react-query';
import { ArrowDownToLine, ArrowUpRight, Sparkles, ListMusic, Plus } from 'lucide-react-native';
import { useAPI, useSession } from '../lib/session';
import { usePlaylists, useUser } from '../lib/queries';
import { color } from '../lib/theme';
import { AlbumCard, PlaylistCard } from '../components/media';
import { Button, FadeIn, Press, Screen, Section, Txt, WaveMark } from '../components/ui';

export function HomeScreen() {
  const api = useAPI(),
    session = useSession(),
    user = useUser(),
    playlists = usePlaylists(),
    width = useWindowDimensions().width;
  const albums = useQuery({
    queryKey: ['library', 'albums', 'home'],
    queryFn: () => api.library.listAlbums({ limit: 12 }),
  });
  const hour = new Date().getHours(),
    greeting = hour < 12 ? 'Good morning' : hour < 18 ? 'Good afternoon' : 'Good evening';
  const firstName = user.data?.user?.name.split(' ')[0];
  const cardWidth = Math.min(165, Math.max(140, width * 0.4));
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
      <View style={{ marginHorizontal: 24, marginBottom: 4 }}>
        <Press
          label="Open play feed"
          onPress={() => router.push('/queue')}
          style={{ backgroundColor: color.raised, borderRadius: 22, padding: 20, gap: 12 }}
        >
          <View
            style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' }}
          >
            <Sparkles color={color.accent} size={26} />
            <ArrowUpRight color={color.accent} size={22} />
          </View>
          <Txt weight="display" size={25}>
            Your play feed
          </Txt>
          <Txt muted size={14} style={{ lineHeight: 21 }}>
            Your queue, recommendations and the next good song.
          </Txt>
        </Press>
      </View>
      <Section
        title="Your playlists"
        eyebrow="YOUR PLAYLISTS"
        action="See all"
        onPress={() => router.navigate('/playlists')}
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
      <FadeIn delay={140}>
        <View style={{ flexDirection: 'row', paddingHorizontal: 24, marginTop: 18, gap: 10 }}>
          {[
            { title: 'Your playlists', icon: ListMusic, route: '/playlists' as const },
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
          onPress={() => router.push({ pathname: '/library/catalog', params: { tab: 'Albums' } })}
        >
          <ScrollView
            horizontal
            showsHorizontalScrollIndicator={false}
            contentContainerStyle={{ gap: 15, paddingHorizontal: 24 }}
          >
            {albums.data.albums.map((album) => (
              <AlbumCard key={album.id} album={album} width={cardWidth} />
            ))}
          </ScrollView>
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
