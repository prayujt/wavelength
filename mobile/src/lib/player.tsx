import React, {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  useSyncExternalStore,
} from 'react';
import { AppState, Platform } from 'react-native';
import { Code, ConnectError } from '@connectrpc/connect';
import { setAudioModeAsync, useAudioPlayer, type AudioPlayer } from 'expo-audio';
import * as Crypto from 'expo-crypto';
import {
  PlaybackController,
  type AudioPort,
  type PlaybackPort,
  type PlaybackSnapshot,
} from '../core/playback';
import { authenticatedURL } from '../core/auth';
import { useSession } from './session';
import { errorMessage } from './api';

// A native player is an imperative host object, separate from immutable React state.
function audioPort(audio: AudioPlayer): AudioPort {
  return {
    pause: () => audio.pause(),
    replace: (source) => audio.replace(source),
    play: () => audio.play(),
    seek: (value) => audio.seekTo(value),
    position: () => audio.currentTime,
    duration: () => audio.duration,
    volume(value) {
      audio.volume = value;
    },
    metadata(track) {
      if (Platform.OS !== 'web')
        audio.setActiveForLockScreen(true, {
          title: track.title,
          artist: track.artist,
          albumTitle: track.album,
        });
    },
    clearMetadata() {
      if (Platform.OS !== 'web') audio.clearLockScreenControls();
    },
  };
}
type PlayerState = PlaybackSnapshot &
  Pick<
    PlaybackController,
    | 'play'
    | 'toggle'
    | 'next'
    | 'previous'
    | 'seek'
    | 'add'
    | 'remove'
    | 'move'
    | 'toggleShuffle'
    | 'toggleRepeat'
    | 'clear'
    | 'setVolume'
  > & { audio: AudioPlayer };
const Context = createContext<PlayerState | null>(null);
export function PlayerProvider({ children }: { children: React.ReactNode }) {
  const session = useSession(),
    api = session.api,
    audio = useAudioPlayer(null, { updateInterval: 500 });
  const [controller] = useState(
    () => new PlaybackController(audioPort(audio), () => Crypto.randomUUID(), errorMessage),
  );
  const snapshot = useSyncExternalStore(
    controller.subscribe,
    controller.getSnapshot,
    controller.getSnapshot,
  );
  const server = session.grant?.server,
    preview = session.preview;
  useEffect(() => {
    void setAudioModeAsync({
      playsInSilentMode: true,
      shouldPlayInBackground: true,
      interruptionMode: 'doNotMix',
    }).catch(() => {});
    const subscription = audio.addListener('playbackStatusUpdate', controller.status);
    const app = AppState.addEventListener('change', (value) => {
      if (value !== 'active') controller.checkpoint();
    });
    return () => {
      subscription.remove();
      app.remove();
    };
  }, [audio, controller]);
  useEffect(() => {
    if (!api) return;
    const port: PlaybackPort = {
      async source(id) {
        if (preview)
          return { source: require('../../assets/preview.wav') as number, release: async () => {} };
        const result = await api.sharing.createPlaybackLink({ trackId: id });
        if (!result.link) throw new Error('Wavelength could not prepare this song.');
        return {
          source: { uri: authenticatedURL(server!, result.url) },
          release: () => api.sharing.revokeAccessLink({ id: result.link!.id }, { timeoutMs: 5000 }),
        };
      },
      async track(id) {
        try {
          return (await api.library.getTrack({ id })).track;
        } catch (e) {
          if (ConnectError.from(e).code === Code.NotFound) return undefined;
          throw e;
        }
      },
      async queue() {
        return (await api.user.getQueue({})).queue;
      },
      saveQueue: (trackIds, currentTrackId, position) =>
        api.user.saveQueue(
          {
            trackIds,
            currentTrackId,
            positionMs: BigInt(Math.round(Math.max(0, position) * 1000)),
          },
          { timeoutMs: 5000 },
        ),
      record: (trackId) => api.user.recordPlay({ trackId }),
      listening: api.listening,
    };
    controller.connect(port);
    return () => controller.disconnect();
  }, [api, server, preview, controller]);
  const value = useMemo<PlayerState>(
    () => ({
      ...snapshot,
      audio,
      play: controller.play,
      toggle: controller.toggle,
      next: controller.next,
      previous: controller.previous,
      seek: controller.seek,
      add: controller.add,
      remove: controller.remove,
      move: controller.move,
      toggleShuffle: controller.toggleShuffle,
      toggleRepeat: controller.toggleRepeat,
      clear: controller.clear,
      setVolume: controller.setVolume,
    }),
    [audio, snapshot, controller],
  );
  return <Context.Provider value={value}>{children}</Context.Provider>;
}
export function usePlayer() {
  const player = useContext(Context);
  if (!player) throw new Error('Missing player provider');
  return player;
}
