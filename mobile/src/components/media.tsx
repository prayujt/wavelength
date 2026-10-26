import React, { createContext, useContext, useState } from 'react';
import { Modal, Pressable, Share, StyleSheet, View, ScrollView } from 'react-native';
import { router } from 'expo-router';
import * as Clipboard from 'expo-clipboard';
import { useQueryClient } from '@tanstack/react-query';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import {
  ArrowUpRight,
  Bookmark,
  Check,
  ChevronRight,
  CirclePlus,
  Copy,
  Disc3,
  Download,
  Trash2,
  ListEnd,
  ListPlus,
  MoreHorizontal,
  Play,
  Share2,
  SkipForward,
  UserRound,
  X,
  type LucideIcon,
} from 'lucide-react-native';
import type { Album, Artist, Playlist, Track } from '../gen/wavelength/v1/library_pb';
import { clock } from '../core/queue';
import { useAPI } from '../lib/session';
import { usePlayer } from '../lib/player';
import { usePlaylists } from '../lib/queries';
import { color } from '../lib/theme';
import { Artwork } from './artwork';
import { Equalizer } from './equalizer';
import { Button, IconButton, Press, Txt } from './ui';
import { useFeedback } from './feedback';
import { useDevice } from './device';

type Menu = { open(track: Track): void; share(ids: string[], name: string): Promise<void> };
const MenuContext = createContext<Menu>({ open() {}, async share() {} });
export const useMediaMenu = () => useContext(MenuContext);

export function MediaMenuProvider({ children }: { children: React.ReactNode }) {
  const [track, setTrack] = useState<Track | null>(null),
    [adding, setAdding] = useState(false);
  const [shared, setShared] = useState<{ url: string; name: string } | null>(null);
  const api = useAPI(),
    device = useDevice(),
    player = usePlayer(),
    feedback = useFeedback(),
    cache = useQueryClient();
  const playlists = usePlaylists(),
    insets = useSafeAreaInsets();
  function close() {
    setTrack(null);
    setAdding(false);
  }
  async function share(ids: string[], name: string) {
    if (
      !(await feedback.confirm(
        'Share this music?',
        'Anyone with the link can listen for 30 days. You can revoke the link from Shared links in Settings.',
        'Create link',
      ))
    )
      return;
    const result = await api.sharing.createShare({ trackIds: ids, name });
    void cache.invalidateQueries({ queryKey: ['shares'] });
    setShared({ url: result.url, name });
  }
  const actions: { title: string; icon: LucideIcon; run(): unknown }[] = track
    ? [
        ...(device.enabled
          ? [
              {
                title: device.items.some((item) => item.track.id === track.id)
                  ? 'Remove download from device'
                  : 'Download to device',
                icon: device.items.some((item) => item.track.id === track.id) ? Trash2 : Download,
                run() {
                  const saved = device.items.some((item) => item.track.id === track.id);
                  close();
                  return saved ? device.remove(track.id) : device.save([track]);
                },
              },
            ]
          : []),
        {
          title: 'Play next',
          icon: SkipForward,
          run() {
            player.add([track], true);
            feedback.notify('Playing next');
            close();
          },
        },
        {
          title: 'Add to queue',
          icon: ListEnd,
          run() {
            player.add([track]);
            feedback.notify('Added to your queue');
            close();
          },
        },
        {
          title: 'Add to playlist',
          icon: ListPlus,
          run() {
            setAdding(true);
          },
        },
        {
          title: 'Save a bookmark',
          icon: Bookmark,
          async run() {
            await api.user.saveBookmark({
              trackId: track.id,
              positionMs: BigInt(
                Math.round((player.track?.id === track.id ? player.audio.currentTime : 0) * 1000),
              ),
              comment: '',
            });
            void cache.invalidateQueries({ queryKey: ['bookmarks'] });
            feedback.notify('Bookmark saved');
            close();
          },
        },
        {
          title: 'Share a listening link',
          icon: Share2,
          async run() {
            close();
            await share([track.id], track.title);
          },
        },
        ...(track.albumId
          ? [
              {
                title: 'Go to album',
                icon: Disc3,
                run() {
                  close();
                  router.push({ pathname: '/album/[id]', params: { id: track.albumId } });
                },
              },
            ]
          : []),
        ...(track.artistId
          ? [
              {
                title: 'Go to artist',
                icon: UserRound,
                run() {
                  close();
                  router.push({ pathname: '/artist/[id]', params: { id: track.artistId } });
                },
              },
            ]
          : []),
        {
          title: 'Choose a different recording',
          icon: ArrowUpRight,
          async run() {
            const result = await api.ingestion.lookupImports({ trackIds: [track.id] });
            const ref = result.tracks.find((t) => t.trackId === track.id)?.metadataRef;
            if (!ref) throw new Error('This song has no saved catalog reference to replace.');
            close();
            router.push({
              pathname: '/sources',
              params: { provider: ref.provider, id: ref.id, replace: track.id, title: track.title },
            });
          },
        },
      ]
    : [];
  return (
    <MenuContext.Provider
      value={{
        open(value) {
          setTrack(value);
          setAdding(false);
        },
        share,
      }}
    >
      {children}
      <Modal transparent animationType="slide" visible={!!track} onRequestClose={close}>
        <View style={s.modal}>
          <Pressable
            onPress={close}
            accessibilityLabel="Close song menu"
            style={StyleSheet.absoluteFill}
          />
          <View
            accessibilityViewIsModal
            style={[s.sheet, { paddingBottom: Math.max(insets.bottom, 20), maxHeight: '85%' }]}
          >
            <View style={s.handle} />
            <View style={s.sheetHeader}>
              {track && (
                <Artwork
                  id={track.albumId || track.album}
                  title={track.album}
                  path={track.artworkPath}
                  size={58}
                />
              )}
              <View style={{ flex: 1, gap: 5 }}>
                <Txt weight="bold" size={16} numberOfLines={1}>
                  {track?.title}
                </Txt>
                <Txt muted size={13} numberOfLines={1}>
                  {track?.artist}
                </Txt>
              </View>
              <IconButton icon={X} label="Close menu" onPress={close} />
            </View>
            <ScrollView showsVerticalScrollIndicator={false}>
              {adding ? (
                <View style={{ paddingHorizontal: 8 }}>
                  <Txt size={20} weight="display" style={{ padding: 16 }}>
                    Add to a playlist
                  </Txt>
                  {playlists.data?.playlists.map((p) => (
                    <Press
                      key={p.id}
                      label={`Add to ${p.name}`}
                      onPress={async () => {
                        await api.user.updatePlaylist({ id: p.id, addTrackIds: [track!.id] });
                        void cache.invalidateQueries({ queryKey: ['playlists'] });
                        void cache.invalidateQueries({ queryKey: ['playlist', p.id] });
                        feedback.notify(`Added to ${p.name}`);
                        close();
                      }}
                      style={s.menuRow}
                    >
                      <ListPlus size={22} color={color.accent} />
                      <Txt style={{ flex: 1 }}>{p.name}</Txt>
                      <ChevronRight size={18} color={color.faint} />
                    </Press>
                  ))}
                  <Press
                    label="Create a new playlist"
                    onPress={() => {
                      const id = track!.id;
                      close();
                      router.push({ pathname: '/playlist-edit', params: { add: id } });
                    }}
                    style={s.menuRow}
                  >
                    <CirclePlus size={22} color={color.accent} />
                    <Txt>Create a new playlist</Txt>
                  </Press>
                </View>
              ) : (
                actions.map((action) => (
                  <Press
                    key={action.title}
                    label={action.title}
                    onPress={action.run}
                    style={s.menuRow}
                  >
                    <action.icon size={22} color={color.muted} strokeWidth={1.6} />
                    <Txt style={{ flex: 1 }}>{action.title}</Txt>
                  </Press>
                ))
              )}
            </ScrollView>
          </View>
        </View>
      </Modal>
      <Modal
        transparent
        animationType="fade"
        visible={!!shared}
        onRequestClose={() => setShared(null)}
      >
        <View style={[s.modal, { justifyContent: 'center', padding: 24 }]}>
          <Pressable
            accessibilityLabel="Close shared link"
            onPress={() => setShared(null)}
            style={StyleSheet.absoluteFill}
          />
          <View
            accessibilityViewIsModal
            style={{
              backgroundColor: color.surface,
              borderRadius: 24,
              padding: 24,
              gap: 18,
              width: '100%',
              maxWidth: 440,
              alignSelf: 'center',
            }}
          >
            <Txt weight="display" size={25}>
              Pass a good thing on.
            </Txt>
            <Txt muted size={13} style={{ lineHeight: 21 }}>
              Anyone with this link can listen to {shared?.name}. It expires in 30 days, and you can
              revoke it in Shared links.
            </Txt>
            <View style={{ backgroundColor: color.bg, padding: 16, borderRadius: 12 }}>
              <Txt selectable size={12} style={{ color: color.accent, lineHeight: 20 }}>
                {shared?.url}
              </Txt>
            </View>
            <Button
              title="Copy listening link"
              icon={Copy}
              onPress={async () => {
                await Clipboard.setStringAsync(shared!.url);
                feedback.notify('Listening link copied');
              }}
            />
            <Button
              title="Open share sheet"
              secondary
              icon={Share2}
              onPress={() =>
                Share.share({ message: shared!.url, url: shared!.url, title: shared!.name })
              }
            />
            <Button title="Done" secondary onPress={() => setShared(null)} />
          </View>
        </View>
      </Modal>
    </MenuContext.Provider>
  );
}

export function TrackRow({
  track,
  index,
  onPlay,
  subtitle,
  trailing,
  compact,
  onPress,
}: {
  track: Track;
  index?: number;
  onPlay?: () => unknown;
  subtitle?: string;
  trailing?: React.ReactNode;
  compact?: boolean;
  onPress?: () => unknown;
}) {
  const player = usePlayer(),
    device = useDevice(),
    menu = useMediaMenu(),
    active = player.track?.id === track.id;
  return (
    <View style={[s.track, compact && { paddingHorizontal: 0 }]}>
      <View style={{ flex: 1 }}>
        <Press
          label={`Play ${track.title}`}
          onPress={onPress || onPlay || (() => player.play([track]))}
          style={s.trackMain}
        >
          {index === undefined ? (
            <Artwork
              title={track.album}
              id={track.albumId || track.album}
              path={track.artworkPath}
              size={50}
              radius={9}
            />
          ) : (
            <View style={s.number}>
              {active && player.playing ? (
                <Equalizer />
              ) : (
                <Txt muted size={13} style={active ? { color: color.accent } : undefined}>
                  {index + 1}
                </Txt>
              )}
            </View>
          )}
          <View style={{ flex: 1, gap: 5 }}>
            <Txt
              size={14}
              weight="medium"
              numberOfLines={1}
              style={active ? { color: color.accent } : undefined}
            >
              {track.title || 'Loading song…'}
            </Txt>
            <Txt muted size={12} numberOfLines={1}>
              {subtitle || track.artist}
              {device.enabled && device.items.some((item) => item.track.id === track.id)
                ? ' · Downloaded'
                : ''}
            </Txt>
          </View>
        </Press>
      </View>
      {trailing || (
        <>
          <Txt muted size={11}>
            {clock(track.durationSeconds)}
          </Txt>
          <IconButton
            icon={MoreHorizontal}
            label={`More options for ${track.title}`}
            onPress={() => menu.open(track)}
            size={20}
          />
        </>
      )}
    </View>
  );
}
export function AlbumCard({ album, width = 155 }: { album: Album; width?: number }) {
  return (
    <Press
      label={`Open album ${album.name}`}
      onPress={() => router.push({ pathname: '/album/[id]', params: { id: album.id } })}
      style={{ width, gap: 11 }}
    >
      <Artwork title={album.name} id={album.id} path={album.artworkPath} size={width} radius={14} />
      <View style={{ gap: 5 }}>
        <Txt weight="bold" size={13} numberOfLines={1}>
          {album.name}
        </Txt>
        <Txt muted size={12} numberOfLines={1}>
          {album.artist}
        </Txt>
      </View>
    </Press>
  );
}
export function ArtistCard({ artist, width = 130 }: { artist: Artist; width?: number }) {
  return (
    <Press
      label={`Open artist ${artist.name}`}
      onPress={() => router.push({ pathname: '/artist/[id]', params: { id: artist.id } })}
      style={{ width, gap: 12, alignItems: 'center' }}
    >
      <Artwork
        title={artist.name}
        id={artist.id}
        path={artist.artworkPath}
        size={width}
        radius={width / 2}
        label={false}
      />
      <Txt weight="medium" size={13} numberOfLines={1}>
        {artist.name}
      </Txt>
    </Press>
  );
}
export function PlaylistCard({ playlist, width = 155 }: { playlist: Playlist; width?: number }) {
  return (
    <Press
      label={`Open playlist ${playlist.name}`}
      onPress={() => router.push({ pathname: '/playlist/[id]', params: { id: playlist.id } })}
      style={{ width, gap: 10 }}
    >
      <Artwork title={playlist.name} id={playlist.id} size={width} radius={14} />
      <View style={{ gap: 5 }}>
        <Txt weight="bold" size={13} numberOfLines={1}>
          {playlist.name}
        </Txt>
        <Txt muted size={12}>
          {playlist.trackIds.length} songs
        </Txt>
      </View>
    </Press>
  );
}
export function LibraryBadge({
  count,
  total,
  onPress,
}: {
  count: number;
  total: number;
  onPress?: () => unknown;
}) {
  return (
    <Press
      label={count === total && total > 0 ? 'In your library' : 'Download'}
      onPress={onPress}
      style={{ flexDirection: 'row', gap: 5, alignItems: 'center', paddingVertical: 8 }}
    >
      {count === total && total > 0 ? (
        <Check size={16} color={color.accent} />
      ) : (
        <CirclePlus size={18} color={color.accent} />
      )}
      <Txt size={11} style={{ color: color.accent }}>
        {count === total && total > 0
          ? 'In library'
          : count > 0
            ? `${count}/${total} saved`
            : 'Add'}
      </Txt>
    </Press>
  );
}
export function PlayCollection({ tracks }: { tracks: Track[] }) {
  const player = usePlayer();
  return (
    <View style={{ flexDirection: 'row', gap: 12, alignItems: 'center' }}>
      <Button
        title="Play"
        icon={Play}
        onPress={() => player.play(tracks)}
        disabled={!tracks.length}
      />
      <Button
        title="Shuffle"
        secondary
        onPress={async () => {
          await player.play(tracks);
          player.toggleShuffle();
        }}
        disabled={!tracks.length}
      />
    </View>
  );
}
const s = StyleSheet.create({
  track: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: 24,
    minHeight: 72,
    gap: 8,
  },
  trackMain: { flexDirection: 'row', alignItems: 'center', gap: 13, minHeight: 70 },
  number: { width: 28, alignItems: 'center' },
  modal: { flex: 1, backgroundColor: color.overlay, justifyContent: 'flex-end' },
  sheet: {
    backgroundColor: color.surface,
    borderTopLeftRadius: 28,
    borderTopRightRadius: 28,
    maxWidth: 600,
    width: '100%',
    alignSelf: 'center',
  },
  handle: {
    width: 36,
    height: 4,
    borderRadius: 2,
    backgroundColor: color.faint,
    alignSelf: 'center',
    marginTop: 10,
    marginBottom: 16,
  },
  sheetHeader: {
    flexDirection: 'row',
    gap: 14,
    alignItems: 'center',
    paddingHorizontal: 20,
    paddingBottom: 18,
    borderBottomWidth: 1,
    borderBottomColor: color.line,
  },
  menuRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: 24,
    paddingVertical: 17,
    gap: 18,
    minHeight: 58,
  },
});
