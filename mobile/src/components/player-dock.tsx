import React from 'react';
import { ActivityIndicator, View } from 'react-native';
import { useAudioPlayerStatus } from 'expo-audio';
import { router, useSegments } from 'expo-router';
import { Pause, Play, SkipForward } from 'lucide-react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { usePlayer } from '../lib/player';
import { color } from '../lib/theme';
import { Artwork } from './artwork';
import { IconButton, Press, Txt } from './ui';

export function PlayerDock() {
  const player = usePlayer(),
    status = useAudioPlayerStatus(player.audio),
    insets = useSafeAreaInsets(),
    segments = useSegments();
  if (!player.track || segments.some((s) => ['player', 'queue', 'lyrics'].includes(s))) return null;
  const tabs = segments.some((s) => s === '(tabs)');
  const progress = status.duration > 0 ? Math.min(1, status.currentTime / status.duration) : 0;
  return (
    <View
      pointerEvents="box-none"
      style={{
        position: 'absolute',
        left: 12,
        right: 12,
        bottom: (tabs ? 75 : 12) + insets.bottom,
        alignItems: 'center',
      }}
    >
      <View
        style={{
          width: '100%',
          maxWidth: 550,
          backgroundColor: '#263C32',
          borderRadius: 18,
          borderWidth: 1,
          borderColor: '#3A5143',
          overflow: 'hidden',
          boxShadow: '0px 5px 24px #00000055',
        }}
      >
        <View style={{ flexDirection: 'row', alignItems: 'center', padding: 8, gap: 9 }}>
          <View style={{ flex: 1 }}>
            <Press
              label="Open now playing"
              onPress={() => router.push('/player')}
              style={{ flexDirection: 'row', alignItems: 'center', gap: 11 }}
            >
              <Artwork
                title={player.track.album}
                id={player.track.albumId || player.track.album}
                path={player.track.artworkPath}
                size={44}
                radius={10}
              />
              <View style={{ flex: 1, gap: 4 }}>
                <Txt size={13} weight="bold" numberOfLines={1}>
                  {player.track.title}
                </Txt>
                <Txt size={11} style={{ color: '#B6C8B9' }} numberOfLines={1}>
                  {player.error ? 'Tap play to retry' : player.track.artist}
                </Txt>
              </View>
            </Press>
          </View>
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
        <View style={{ height: 2, marginHorizontal: 9, backgroundColor: '#577060' }}>
          <View style={{ width: `${progress * 100}%`, backgroundColor: color.accent, height: 2 }} />
        </View>
      </View>
    </View>
  );
}
