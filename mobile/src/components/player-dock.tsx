import React from 'react';
import { ActivityIndicator, View } from 'react-native';
import { useAudioPlayerStatus } from '../lib/audio';
import { router, useSegments } from 'expo-router';
import { Pause, Play, SkipForward } from 'lucide-react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { usePlayer } from '../lib/player';
import { color, TAB_BAR_HEIGHT, PLAYER_DOCK_GAP } from '../lib/theme';
import { hidesPlayerDock } from '../lib/layout';
import { Artwork } from './artwork';
import { Equalizer } from './equalizer';
import { IconButton, Press, Txt } from './ui';

export function PlayerDock() {
  const player = usePlayer(),
    status = useAudioPlayerStatus(player.audio),
    insets = useSafeAreaInsets(),
    segments = useSegments();
  if (!player.track || hidesPlayerDock(segments)) return null;
  const tabs = segments.some((s) => s === '(tabs)');
  const progress = status.duration > 0 ? Math.min(1, status.currentTime / status.duration) : 0;
  return (
    <View
      pointerEvents="box-none"
      style={{
        position: 'absolute',
        left: 12,
        right: 12,
        bottom: (tabs ? TAB_BAR_HEIGHT + PLAYER_DOCK_GAP : 12) + insets.bottom,
        alignItems: 'center',
      }}
    >
      <View
        style={{
          width: '100%',
          maxWidth: 550,
          backgroundColor: color.raised,
          borderRadius: 10,
          borderWidth: 1,
          borderColor: color.line,
          overflow: 'hidden',
          boxShadow: '0px 5px 24px #00000055',
        }}
      >
        <View style={{ flexDirection: 'row', alignItems: 'center', padding: 6, gap: 2 }}>
          <View style={{ flex: 1 }}>
            <Press
              label="Open now playing"
              onPress={() => router.push('/player')}
              style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}
            >
              <Artwork
                title={player.track.album}
                id={player.track.albumId || player.track.album}
                path={player.track.artworkPath}
                size={42}
                radius={6}
              />
              <View style={{ flex: 1, gap: 2 }}>
                <Txt size={13} weight="bold" numberOfLines={1}>
                  {player.track.title}
                </Txt>
                <View style={{ flexDirection: 'row', alignItems: 'center', gap: 5 }}>
                  {!player.playFeedEnabled && <Equalizer size={11} />}
                  <Txt size={12} muted numberOfLines={1} style={{ flex: 1 }}>
                    {player.error ? 'Tap play to retry' : player.track.artist}
                  </Txt>
                </View>
              </View>
            </Press>
          </View>
          {player.playFeedEnabled && (
            <IconButton
              icon={Equalizer}
              label="Turn off play feed"
              active
              size={17}
              onPress={player.stopFeed}
            />
          )}
          {player.loading || status.isBuffering ? (
            <View style={{ width: 44, alignItems: 'center' }}>
              <ActivityIndicator color={color.accent} size="small" />
            </View>
          ) : (
            <IconButton
              icon={player.playing ? Pause : Play}
              label={player.playing ? 'Pause playback' : 'Resume playback'}
              onPress={player.toggle}
            />
          )}
          <IconButton icon={SkipForward} label="Next song" onPress={player.next} size={20} />
        </View>
        <View style={{ height: 2, backgroundColor: color.line }}>
          <View style={{ width: `${progress * 100}%`, backgroundColor: color.accent, height: 2 }} />
        </View>
      </View>
    </View>
  );
}
