import { usePageBottomInset } from '../lib/layout';
import React, { useEffect, useRef, useState } from 'react';
import { ActivityIndicator, FlatList, ScrollView, View, useWindowDimensions } from 'react-native';
import { router } from 'expo-router';
import { LinearGradient } from 'expo-linear-gradient';
import { useAudioPlayerStatus } from 'expo-audio';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import Slider from '@react-native-community/slider';
import {
  ArrowDown,
  ArrowUp,
  Bookmark,
  ChevronDown,
  ListMusic,
  Sparkles,
  MoreHorizontal,
  Pause,
  Play,
  Quote,
  Repeat,
  Repeat1,
  Share2,
  Shuffle,
  SkipBack,
  SkipForward,
  Smartphone,
  Trash2,
  X,
} from 'lucide-react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { usePlayer } from '../lib/player';
import { useAPI } from '../lib/session';
import { color, colors } from '../lib/theme';
import { clock } from '../core/queue';
import { parseLyrics } from '../core/lyrics';
import { Artwork } from '../components/artwork';
import { useMediaMenu } from '../components/media';
import {
  Button,
  Empty,
  FadeIn,
  Failure,
  Header,
  IconButton,
  Loading,
  Pill,
  Press,
  Screen,
  Txt,
  useReducedMotion,
} from '../components/ui';
import { useFeedback } from '../components/feedback';

export function PlayerScreen() {
  const player = usePlayer(),
    status = useAudioPlayerStatus(player.audio),
    api = useAPI(),
    menu = useMediaMenu(),
    feedback = useFeedback(),
    cache = useQueryClient(),
    insets = useSafeAreaInsets();
  const { width, height } = useWindowDimensions(),
    [scrub, setScrub] = useState<number | null>(null),
    track = player.track;
  const [frameHeight, setFrameHeight] = useState(height);
  const usableHeight = Math.max(0, frameHeight - insets.top - insets.bottom);
  const compact = usableHeight < 800;
  if (!track)
    return (
      <Screen>
        <Header title="Now playing" back />
        <Empty
          title="Something good is one tap away."
          text="Choose a song from your library to start listening."
          action="Open library"
          onPress={() => router.replace('/library')}
        />
      </Screen>
    );
  const tint = colors(track.albumId || track.album),
    size = Math.min(430, width - 52, Math.max(148, usableHeight * (compact ? 0.33 : 0.42)));
  const total = status.duration || track.durationSeconds,
    position = scrub ?? status.currentTime;
  async function bookmark() {
    await api.user.saveBookmark({
      trackId: track!.id,
      positionMs: BigInt(Math.round(status.currentTime * 1000)),
      comment: '',
    });
    void cache.invalidateQueries({ queryKey: ['bookmarks'] });
    feedback.notify('Your place is saved');
  }
  return (
    <View
      testID="full-player"
      onLayout={(event) => setFrameHeight(event.nativeEvent.layout.height)}
      style={{ flex: 1, backgroundColor: color.bg }}
    >
      <LinearGradient
        colors={[tint[0], color.bg, color.bg]}
        locations={[0, 0.8, 1]}
        style={{ position: 'absolute', inset: 0 }}
      />
      <ScrollView
        testID="player-scroll"
        showsVerticalScrollIndicator={false}
        contentContainerStyle={{
          flexGrow: 1,
          justifyContent: 'space-between',
          paddingTop: insets.top + 8,
          paddingBottom: Math.max(insets.bottom, 18),
          paddingHorizontal: 26,
          width: '100%',
          maxWidth: 600,
          alignSelf: 'center',
        }}
      >
        <View
          style={{
            flexDirection: 'row',
            alignItems: 'center',
            justifyContent: 'space-between',
            marginBottom: compact ? 14 : 25,
          }}
        >
          <IconButton
            icon={ChevronDown}
            label="Close player"
            onPress={() => (router.canGoBack() ? router.back() : router.replace('/home'))}
          />
          <View style={{ alignItems: 'center', gap: 5 }}>
            <Txt size={9} weight="bold" style={{ letterSpacing: 2, color: tint[2] }}>
              NOW PLAYING
            </Txt>
            <Txt size={11} numberOfLines={1} style={{ maxWidth: Math.max(100, width - 160) }}>
              {track.album}
            </Txt>
          </View>
          <IconButton icon={MoreHorizontal} label="Song options" onPress={() => menu.open(track)} />
        </View>
        <FadeIn style={{ alignItems: 'center', marginBottom: compact ? 18 : 31 }}>
          <View
            style={{
              width: size,
              height: size,
              borderRadius: 15,
              boxShadow: '0px 20px 50px #00000077',
            }}
          >
            <Artwork
              title={track.album}
              id={track.albumId || track.album}
              path={track.artworkPath}
              size={size}
              radius={15}
            />
          </View>
        </FadeIn>
        <View
          style={{
            flexDirection: 'row',
            alignItems: 'center',
            gap: 8,
            marginBottom: compact ? 14 : 19,
          }}
        >
          <View style={{ flex: 1, gap: 9 }}>
            <Txt
              size={compact ? 25 : 28}
              weight="heavy"
              numberOfLines={2}
              style={{ letterSpacing: -0.9, lineHeight: compact ? 30 : 34 }}
            >
              {track.title}
            </Txt>
            <Press
              label={`View artist ${track.artist}`}
              onPress={() =>
                track.artistId &&
                router.push({ pathname: '/artist/[id]', params: { id: track.artistId } })
              }
              style={{ paddingVertical: 2 }}
            >
              <Txt size={15} style={{ color: tint[2] }}>
                {track.artist}
              </Txt>
            </Press>
          </View>
          <IconButton icon={Bookmark} label="Bookmark this position" onPress={bookmark} size={24} />
        </View>
        <View>
          <Slider
            accessibilityLabel="Seek playback"
            minimumValue={0}
            maximumValue={total || 1}
            value={position}
            minimumTrackTintColor={color.accent}
            maximumTrackTintColor="#FFFFFF28"
            thumbTintColor={color.text}
            onSlidingStart={(value) => setScrub(value)}
            onValueChange={setScrub}
            onSlidingComplete={async (value) => {
              try {
                await player.seek(value);
              } catch {
                feedback.notify('Could not seek. Try playing this song again.', 'error');
              } finally {
                setScrub(null);
              }
            }}
            disabled={!status.isLoaded}
            style={{ width: '100%', height: 32 }}
          />
          <View style={{ flexDirection: 'row', justifyContent: 'space-between', marginTop: 2 }}>
            <Txt size={11} muted>
              {clock(position)}
            </Txt>
            <Txt size={11} muted>
              {clock(total)}
            </Txt>
          </View>
        </View>
        <View
          style={{
            flexDirection: 'row',
            alignItems: 'center',
            justifyContent: 'space-between',
            marginTop: compact ? 16 : 22,
            marginBottom: compact ? 16 : 24,
          }}
        >
          <IconButton
            icon={Shuffle}
            label={player.shuffle ? 'Turn shuffle off' : 'Turn shuffle on'}
            active={player.shuffle}
            onPress={player.toggleShuffle}
            size={21}
          />
          <IconButton icon={SkipBack} label="Previous song" onPress={player.previous} size={29} />
          <Press
            label={player.playing ? 'Pause song' : 'Play song'}
            onPress={player.toggle}
            style={{
              width: compact ? 64 : 76,
              height: compact ? 64 : 76,
              borderRadius: 38,
              backgroundColor: color.accent,
              alignItems: 'center',
              justifyContent: 'center',
            }}
          >
            {player.loading || status.isBuffering ? (
              <ActivityIndicator size="large" color={color.accentInk} />
            ) : player.playing ? (
              <Pause size={32} color={color.accentInk} fill={color.accentInk} strokeWidth={1.5} />
            ) : (
              <Play
                size={32}
                color={color.accentInk}
                fill={color.accentInk}
                strokeWidth={1.5}
                style={{ marginLeft: 3 }}
              />
            )}
          </Press>
          <IconButton icon={SkipForward} label="Next song" onPress={player.next} size={29} />
          <IconButton
            icon={player.repeat === 'one' ? Repeat1 : Repeat}
            label={`Repeat ${player.repeat}; change repeat mode`}
            active={player.repeat !== 'off'}
            onPress={player.toggleRepeat}
            size={21}
          />
        </View>
        {player.error && (
          <View
            style={{ backgroundColor: '#482E2A', borderRadius: 14, padding: 15, marginBottom: 14 }}
          >
            <Txt
              accessibilityRole="alert"
              size={12}
              style={{ color: color.danger, lineHeight: 20 }}
            >
              {player.error}
            </Txt>
          </View>
        )}
        <View
          style={{
            flexDirection: 'row',
            alignItems: 'center',
            justifyContent: 'space-between',
            marginBottom: 18,
          }}
        >
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 7 }}>
            <Smartphone size={15} color={color.accent} />
            <Txt size={10} style={{ color: color.accent }}>
              This device
            </Txt>
          </View>
          <View style={{ flexDirection: 'row', gap: 5 }}>
            <IconButton
              icon={Share2}
              label="Share this song"
              size={20}
              onPress={() => menu.share([track.id], track.title)}
            />
            <IconButton
              icon={Quote}
              label="Open lyrics"
              size={20}
              onPress={() => router.push('/lyrics')}
            />
            <IconButton
              icon={ListMusic}
              label="Open play queue"
              size={22}
              onPress={() => router.push('/queue')}
            />
          </View>
        </View>
      </ScrollView>
    </View>
  );
}

export function FeedScreen() {
  const player = usePlayer();
  const start = useRef(player.startFeed);
  useEffect(() => {
    const timer = setTimeout(() => {
      void start.current();
    }, 0);
    return () => clearTimeout(timer);
  }, []);
  return <QueueScreen feedEntry />;
}

export function QueueScreen({ feedEntry = false }: { feedEntry?: boolean }) {
  const bottom = usePageBottomInset();
  const player = usePlayer(),
    feedback = useFeedback();
  return (
    <Screen scroll={false}>
      <FlatList
        testID="queue-scroll"
        data={player.queue}
        keyExtractor={(track, index) => `${track.id}:${index}`}
        contentContainerStyle={{ paddingBottom: bottom }}
        ListHeaderComponent={
          <>
            <Header
              back
              title={feedEntry ? 'Your play feed' : 'Play queue'}
              eyebrow={feedEntry ? 'MADE FOR YOU' : 'YOUR QUEUE'}
              subtitle={
                player.startingFeed
                  ? 'Choosing your first songs…'
                  : feedEntry
                    ? 'Picked from your listening history. More as you go.'
                    : `${player.queue.length} songs, in your order.`
              }
              right={
                player.queue.length > 0 ? (
                  <IconButton
                    icon={Trash2}
                    label="Clear play queue"
                    onPress={async () => {
                      if (
                        await feedback.confirm(
                          'Clear your queue?',
                          'Playback will stop. Your songs and playlists stay in the library.',
                          'Clear queue',
                        )
                      )
                        player.clear();
                    }}
                  />
                ) : undefined
              }
            />
            <View
              style={{
                marginHorizontal: 24,
                marginBottom: 20,
                paddingVertical: 12,
                flexDirection: 'row',
                gap: 8,
              }}
            >
              <Pill
                title="Autoplay"
                active={player.playFeedEnabled}
                onPress={player.togglePlayFeed}
                icon={Sparkles}
              />
              <Pill
                title="Shuffle"
                active={player.shuffle}
                onPress={player.toggleShuffle}
                icon={Shuffle}
              />
              <Pill
                title={`Repeat: ${player.repeat}`}
                active={player.repeat !== 'off'}
                onPress={player.toggleRepeat}
                icon={Repeat}
              />
            </View>
            {feedEntry && player.track && (
              <View
                style={{ paddingHorizontal: 24, flexDirection: 'row', gap: 10, marginBottom: 14 }}
              >
                <Button
                  title={player.playing ? 'Pause feed' : 'Play feed'}
                  icon={player.playing ? Pause : Play}
                  onPress={() => player.toggle()}
                  small
                />
                <IconButton icon={SkipForward} label="Next feed song" onPress={player.next} />
                <IconButton
                  icon={Sparkles}
                  label="Start a new play feed"
                  onPress={() => {
                    void player.startFeed(true);
                  }}
                  disabled={player.startingFeed}
                />
              </View>
            )}
            {(player.playFeedLoading || player.playFeedError || (feedEntry && player.error)) && (
              <Txt muted size={12} style={{ marginHorizontal: 24, marginBottom: 12 }}>
                {player.playFeedLoading
                  ? player.startingFeed
                    ? 'Choosing your first songs…'
                    : 'Updating recommendations…'
                  : 'Recommendations will retry when connected.'}
              </Txt>
            )}
          </>
        }
        renderItem={({ item, index }) => (
          <View
            style={{
              marginHorizontal: 24,
              paddingVertical: 13,
              paddingHorizontal: 12,
              borderRadius: 14,
              backgroundColor: index === player.index ? color.raised : 'transparent',
              flexDirection: 'row',
              alignItems: 'center',
              gap: 10,
            }}
          >
            <Press
              label={`Play queued ${item.title}`}
              onPress={() => player.jump(index)}
              style={{ flexDirection: 'row', alignItems: 'center', flex: 1, gap: 11 }}
            >
              <Artwork
                title={item.album}
                id={item.albumId || item.album}
                path={item.artworkPath}
                size={46}
                radius={8}
              />
              <View style={{ flex: 1, gap: 5 }}>
                <Txt
                  size={13}
                  weight="medium"
                  style={index === player.index ? { color: color.accent } : undefined}
                  numberOfLines={1}
                >
                  {item.title}
                </Txt>
                <Txt size={11} muted numberOfLines={1}>
                  {index === player.index
                    ? 'Now playing'
                    : player.recommendation(item)
                      ? 'Recommended · ' + item.artist
                      : item.artist}
                </Txt>
              </View>
            </Press>
            <View style={{ flexDirection: 'row' }}>
              <IconButton
                icon={ArrowUp}
                label={`Move queued song ${index + 1} up`}
                onPress={() => player.move(index, index - 1)}
                disabled={index === 0}
                style={{ width: 32 }}
                size={16}
              />
              <IconButton
                icon={ArrowDown}
                label={`Move queued song ${index + 1} down`}
                onPress={() => player.move(index, index + 1)}
                disabled={index === player.queue.length - 1}
                style={{ width: 32 }}
                size={16}
              />
              <IconButton
                icon={X}
                label={`Remove queued song ${index + 1}`}
                onPress={() => player.remove(index)}
                disabled={index === player.index}
                style={{ width: 32 }}
                size={16}
              />
            </View>
          </View>
        )}
        ListEmptyComponent={
          player.startingFeed ? (
            <Loading rows={3} />
          ) : feedEntry ? (
            <Empty
              icon={Sparkles}
              title="Your next song is out there"
              text={player.error || 'Start listening from your history. No queue needed.'}
              action="Try play feed again"
              onPress={() => {
                void player.startFeed(true);
              }}
            />
          ) : (
            <Empty
              icon={ListMusic}
              title="Start with something you love"
              text="Choose a playlist or a song. Autoplay will find what comes next."
              action="Choose a playlist"
              onPress={() => router.replace('/playlists')}
            />
          )
        }
      />
    </Screen>
  );
}

export function LyricsScreen() {
  const bottom = usePageBottomInset();
  const player = usePlayer(),
    status = useAudioPlayerStatus(player.audio),
    api = useAPI(),
    scroll = useRef<ScrollView>(null),
    positions = useRef<number[]>([]),
    reduced = useReducedMotion();
  const [follow, setFollow] = useState(true),
    track = player.track;
  const data = useQuery({
    queryKey: ['lyrics', track?.id],
    queryFn: () => api.library.getLyrics({ id: track!.id }),
    enabled: !!track,
  });
  const lines = parseLyrics(data.data?.lyrics || ''),
    timed = lines.some((line) => line.time !== undefined);
  const active = lines.reduce(
    (current, line, i) =>
      line.time !== undefined && line.time <= status.currentTime ? i : current,
    -1,
  );
  useEffect(() => {
    if (follow && active >= 0 && positions.current[active] !== undefined)
      scroll.current?.scrollTo({
        y: Math.max(0, positions.current[active] - 150),
        animated: !reduced,
      });
  }, [active, follow, reduced]);
  if (!track)
    return (
      <Screen>
        <Header back title="Lyrics" />
        <Empty title="Words need a song." text="Choose a track first to see its saved lyrics." />
      </Screen>
    );
  return (
    <View style={{ flex: 1, backgroundColor: colors(track.albumId || track.album)[0] }}>
      <Screen scroll={false} style={{ backgroundColor: 'transparent' }}>
        <Header
          back
          title={track.title}
          eyebrow="THE WORDS"
          subtitle={track.artist}
          right={
            timed ? (
              <IconButton
                icon={Quote}
                label={follow ? 'Stop following lyrics' : 'Follow current lyric'}
                active={follow}
                onPress={() => setFollow(!follow)}
              />
            ) : undefined
          }
        />
        {data.isPending ? (
          <Loading />
        ) : data.error ? (
          <Failure error={data.error} retry={data.refetch} />
        ) : !lines.length ? (
          <Empty
            icon={Quote}
            title="Let the music do the talking."
            text="There aren’t any saved lyrics for this track yet."
          />
        ) : (
          <ScrollView
            testID="lyrics-scroll"
            ref={scroll}
            showsVerticalScrollIndicator={false}
            onScrollBeginDrag={() => setFollow(false)}
            contentContainerStyle={{ paddingHorizontal: 28, paddingTop: 24, paddingBottom: bottom }}
          >
            {lines.map((line, i) => (
              <View
                key={`${line.time}:${i}`}
                onLayout={(event) => {
                  positions.current[i] = event.nativeEvent.layout.y;
                }}
              >
                <Press
                  label={line.time === undefined ? line.text : `Seek to lyric: ${line.text}`}
                  onPress={line.time === undefined ? undefined : () => player.seek(line.time!)}
                  style={{ paddingVertical: 14 }}
                >
                  <Txt
                    size={29}
                    weight="display"
                    style={{
                      lineHeight: 42,
                      letterSpacing: -0.6,
                      color: !timed || i === active ? color.text : '#FFFFFF60',
                    }}
                  >
                    {line.text}
                  </Txt>
                </Press>
              </View>
            ))}
            <Txt muted size={10} style={{ marginTop: 30 }}>
              Lyrics saved with this song{timed ? ' · Tap a line to seek' : ''}
            </Txt>
          </ScrollView>
        )}
        {timed && !follow && (
          <View style={{ position: 'absolute', bottom: 30, alignSelf: 'center' }}>
            <Button title="Follow along" small icon={Quote} onPress={() => setFollow(true)} />
          </View>
        )}
      </Screen>
    </View>
  );
}
