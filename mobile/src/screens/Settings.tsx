import React, { useState } from 'react';
import { View } from 'react-native';
import { router } from 'expo-router';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import Slider from '@react-native-community/slider';
import {
  Bookmark,
  Check,
  ChevronRight,
  Link2,
  LogOut,
  ShieldCheck,
  Trash2,
  Volume2,
} from 'lucide-react-native';
import { useAPI, useSession } from '../lib/session';
import { usePlayer } from '../lib/player';
import { useTracks, useUser } from '../lib/queries';
import { color } from '../lib/theme';
import { clock } from '../core/queue';
import { TrackRow } from '../components/media';
import {
  Button,
  Empty,
  Failure,
  Field,
  Header,
  IconButton,
  Loading,
  Press,
  Screen,
  Section,
  Txt,
  WaveMark,
} from '../components/ui';
import { useFeedback } from '../components/feedback';

export function SettingsScreen() {
  const session = useSession(),
    user = useUser(),
    player = usePlayer(),
    feedback = useFeedback();
  const [reconnecting, setReconnecting] = useState(false),
    [callback, setCallback] = useState(''),
    [volume, setVolume] = useState(player.audio.volume);
  const profile = user.data?.user;
  return (
    <Screen>
      <Header back title="On your wavelength." eyebrow="YOUR SPACE" />
      <View
        style={{
          marginHorizontal: 24,
          backgroundColor: color.surface,
          borderRadius: 22,
          padding: 22,
          gap: 16,
        }}
      >
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: 17 }}>
          <View
            style={{
              width: 61,
              height: 61,
              borderRadius: 31,
              backgroundColor: '#2D4833',
              justifyContent: 'center',
              alignItems: 'center',
            }}
          >
            <Txt weight="display" size={25} style={{ color: color.accent }}>
              {profile?.name[0] || 'W'}
            </Txt>
          </View>
          <View style={{ flex: 1, gap: 5 }}>
            <Txt weight="display" size={21}>
              {profile?.name || 'Your Wavelength account'}
            </Txt>
            <Txt muted size={12}>
              {session.preview ? 'Exploring the preview collection' : profile?.email}
            </Txt>
          </View>
        </View>
        <View
          style={{
            borderTopWidth: 1,
            borderTopColor: color.line,
            paddingTop: 16,
            flexDirection: 'row',
            gap: 8,
            alignItems: 'center',
          }}
        >
          <ShieldCheck size={15} color={color.accent} />
          <Txt size={11} muted>
            {session.preview
              ? 'Preview mode · changes stay on this device'
              : session.grant?.server.replace('https://', '')}
          </Txt>
        </View>
      </View>
      <Section title="A few things worth keeping">
        <View
          style={{
            marginHorizontal: 24,
            borderRadius: 20,
            backgroundColor: color.surface,
            paddingVertical: 6,
          }}
        >
          {[
            {
              title: 'Saved bookmarks',
              caption: 'Pick up where you left off',
              icon: Bookmark,
              route: '/bookmarks' as const,
            },
            {
              title: 'Shared links',
              caption: 'Music you’ve sent out into the world',
              icon: Link2,
              route: '/shares' as const,
            },
          ].map(({ title, caption, icon: Icon, route }) => (
            <Press
              key={title}
              label={title}
              onPress={() => router.push(route)}
              style={{ flexDirection: 'row', alignItems: 'center', padding: 18, gap: 14 }}
            >
              <Icon size={22} color={color.accent} strokeWidth={1.5} />
              <View style={{ flex: 1, gap: 5 }}>
                <Txt size={14} weight="medium">
                  {title}
                </Txt>
                <Txt size={11} muted>
                  {caption}
                </Txt>
              </View>
              <ChevronRight size={17} color={color.faint} />
            </Press>
          ))}
        </View>
      </Section>
      <Section title="Just the right level">
        <View style={{ paddingHorizontal: 24 }}>
          <View style={{ backgroundColor: color.surface, padding: 20, borderRadius: 18 }}>
            <View style={{ flexDirection: 'row', gap: 10, alignItems: 'center' }}>
              <Volume2 size={19} color={color.accent} />
              <Txt size={13}>Playback volume</Txt>
              <Txt size={12} muted style={{ marginLeft: 'auto' }}>
                {Math.round(volume * 100)}%
              </Txt>
            </View>
            <Slider
              accessibilityLabel="Playback volume"
              minimumValue={0}
              maximumValue={1}
              value={volume}
              onValueChange={(value) => {
                setVolume(value);
                player.setVolume(value);
              }}
              minimumTrackTintColor={color.accent}
              maximumTrackTintColor={color.line}
              thumbTintColor={color.text}
              style={{ marginTop: 16, height: 30 }}
            />
            <Txt size={11} muted style={{ lineHeight: 19, marginTop: 7 }}>
              Playback follows your device’s audio controls and pauses when headphones disconnect.
            </Txt>
          </View>
        </View>
      </Section>
      {!session.preview && (
        <Section title="Your connection">
          <View style={{ paddingHorizontal: 24, gap: 16 }}>
            <Txt muted size={12} style={{ lineHeight: 21 }}>
              Sign in again to refresh your account’s permissions. Your playlists and library stay
              with your account.
            </Txt>
            <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: 7 }}>
              {[
                ['wavelength:read', 'Library'],
                ['wavelength:write', 'Playlists'],
                ['wavelength:stream', 'Playback'],
                ['wavelength:ingest', 'Downloads'],
              ].map(([scope, name]) => (
                <View
                  key={scope}
                  style={{
                    backgroundColor: color.surface,
                    borderRadius: 20,
                    flexDirection: 'row',
                    paddingVertical: 8,
                    paddingHorizontal: 12,
                    gap: 5,
                    alignItems: 'center',
                  }}
                >
                  {session.grant?.scope.split(' ').includes(scope) && (
                    <Check size={12} color={color.accent} />
                  )}
                  <Txt size={11} muted>
                    {name}
                  </Txt>
                </View>
              ))}
            </View>
            <Button
              title="Reconnect account"
              secondary
              icon={ShieldCheck}
              onPress={async () => {
                setReconnecting(true);
                await session.login(session.account?.server || session.grant!.server);
              }}
            />
            {reconnecting && (
              <>
                <Field
                  label="Browser return link"
                  value={callback}
                  onChange={setCallback}
                  placeholder="Paste if your browser could not return"
                  secure
                />
                <Button
                  title="Finish reconnecting"
                  secondary
                  disabled={!callback}
                  onPress={async () => {
                    await session.complete(callback);
                    setReconnecting(false);
                    feedback.notify('Connection refreshed');
                  }}
                />
              </>
            )}
          </View>
        </Section>
      )}
      <View style={{ margin: 24, marginTop: 36 }}>
        <Button
          title={session.preview ? 'Leave the preview' : 'Sign out'}
          icon={LogOut}
          secondary
          onPress={async () => {
            await player.clear();
            await session.logout();
            router.replace('/welcome');
          }}
        />
      </View>
      <View style={{ alignItems: 'center', paddingVertical: 25, gap: 11 }}>
        <WaveMark size={29} />
        <Txt size={12} muted>
          Less noise. More music.
        </Txt>
        <Txt size={10} style={{ color: color.faint }}>
          Wavelength · 0.1.0
        </Txt>
      </View>
    </Screen>
  );
}

export function BookmarksScreen() {
  const api = useAPI(),
    player = usePlayer(),
    cache = useQueryClient(),
    feedback = useFeedback();
  const data = useQuery({
    queryKey: ['bookmarks'],
    queryFn: () => api.user.listBookmarks({ limit: 500 }),
  });
  const tracks = useTracks(data.data?.bookmarks.map((b) => b.trackId) || []);
  return (
    <Screen>
      <Header
        back
        title="Right where you left it."
        eyebrow="SAVED BOOKMARKS"
        subtitle="Some moments are worth coming back to."
      />
      {data.isPending || tracks.isPending ? (
        <Loading />
      ) : data.error || tracks.error ? (
        <Failure
          error={data.error || tracks.error}
          retry={() => {
            void data.refetch();
            void tracks.refetch();
          }}
        />
      ) : !data.data.bookmarks.length ? (
        <Empty
          icon={Bookmark}
          title="Keep your place."
          text="Save a bookmark from the player or a song’s menu. Come back to the exact moment later."
        />
      ) : (
        data.data.bookmarks.map((bookmark) => {
          const track = tracks.data?.find((t) => t.id === bookmark.trackId);
          return (
            <View key={bookmark.trackId}>
              {track ? (
                <TrackRow
                  track={track}
                  subtitle={`${track.artist} · Saved at ${clock(Number(bookmark.positionMs) / 1000)}`}
                  onPlay={() => player.play([track], 0, Number(bookmark.positionMs) / 1000)}
                  trailing={
                    <IconButton
                      icon={Trash2}
                      label={`Delete bookmark for ${track.title}`}
                      size={18}
                      onPress={async () => {
                        await api.user.deleteBookmark({ trackId: bookmark.trackId });
                        void cache.invalidateQueries({ queryKey: ['bookmarks'] });
                        feedback.notify('Bookmark removed');
                      }}
                    />
                  }
                />
              ) : (
                <View
                  style={{
                    marginHorizontal: 24,
                    padding: 15,
                    flexDirection: 'row',
                    gap: 12,
                    alignItems: 'center',
                  }}
                >
                  <Txt muted style={{ flex: 1 }}>
                    This song is no longer in the library.
                  </Txt>
                  <IconButton
                    icon={Trash2}
                    label="Remove unavailable bookmark"
                    onPress={async () => {
                      await api.user.deleteBookmark({ trackId: bookmark.trackId });
                      void cache.invalidateQueries({ queryKey: ['bookmarks'] });
                    }}
                  />
                </View>
              )}
            </View>
          );
        })
      )}
    </Screen>
  );
}

export function SharesScreen() {
  const api = useAPI(),
    cache = useQueryClient(),
    feedback = useFeedback();
  const data = useQuery({
    queryKey: ['shares'],
    queryFn: () => api.sharing.listAccessLinks({ kind: 'share', limit: 100 }),
  });
  return (
    <Screen>
      <Header
        back
        title="Good music travels."
        eyebrow="YOUR SHARED LINKS"
        subtitle="A little part of your collection, sent out into the world."
      />
      {data.isPending ? (
        <Loading />
      ) : data.error ? (
        <Failure error={data.error} retry={data.refetch} />
      ) : !data.data.links.length ? (
        <Empty
          icon={Link2}
          title="Something worth passing on."
          text="Share a song, an album, or a playlist to create a listening link. Manage those links here."
        />
      ) : (
        data.data.links.map((link) => (
          <View
            key={link.id}
            style={{
              marginHorizontal: 24,
              padding: 19,
              marginBottom: 13,
              backgroundColor: color.surface,
              borderRadius: 18,
              borderWidth: 1,
              borderColor: color.line,
              gap: 12,
            }}
          >
            <View style={{ flexDirection: 'row', alignItems: 'center', gap: 12 }}>
              <View
                style={{
                  backgroundColor: color.raised,
                  width: 44,
                  height: 44,
                  borderRadius: 14,
                  alignItems: 'center',
                  justifyContent: 'center',
                }}
              >
                <Link2 size={20} color={color.accent} />
              </View>
              <View style={{ flex: 1, gap: 5 }}>
                <Txt weight="medium" numberOfLines={1}>
                  {link.name || 'Shared music'}
                </Txt>
                <Txt muted size={11}>
                  {link.trackIds.length} songs ·{' '}
                  {link.expiresAt
                    ? `Expires ${new Date(Number(link.expiresAt.seconds) * 1000).toLocaleDateString()}`
                    : 'No expiry'}
                </Txt>
              </View>
            </View>
            <Txt muted size={12} style={{ lineHeight: 20 }}>
              Anyone holding this link can listen. Revoking it stops access immediately.
            </Txt>
            <View style={{ alignItems: 'flex-start' }}>
              <Button
                title="Revoke link"
                secondary
                small
                icon={Trash2}
                onPress={async () => {
                  if (
                    !(await feedback.confirm(
                      'Revoke this listening link?',
                      'Anyone using this link will lose access immediately.',
                      'Revoke link',
                    ))
                  )
                    return;
                  await api.sharing.revokeAccessLink({ id: link.id });
                  void cache.invalidateQueries({ queryKey: ['shares'] });
                  feedback.notify('Listening link revoked');
                }}
              />
            </View>
          </View>
        ))
      )}
    </Screen>
  );
}
