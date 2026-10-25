import React, {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  useRef,
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
import { accountWrite, installed, partition, read } from './device-store';
import { localSource } from './device-library';

// A native player is an imperative host object, separate from immutable React state.
function audioPort(audio: AudioPlayer, controls: () => PlaybackController): AudioPort {
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
      audio.setActiveForLockScreen(true, {
        title: track.title,
        artist: track.artist,
        albumTitle: track.album,
      });
      if (Platform.OS === 'web' && 'mediaSession' in navigator) {
        try {
          navigator.mediaSession.setActionHandler('nexttrack', () => controls().next());
          navigator.mediaSession.setActionHandler('previoustrack', () => controls().previous());
        } catch {
          /* Unsupported actions leave play, pause and seeking available. */
        }
      }
    },
    clearMetadata() {
      audio.clearLockScreenControls();
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
  const [controller] = useState(() => {
    const instance: PlaybackController = new PlaybackController(
      audioPort(audio, () => instance),
      () => Crypto.randomUUID(),
      errorMessage,
    );
    return instance;
  });
  const preferencesReady = useRef('');
  const snapshot = useSyncExternalStore(
    controller.subscribe,
    controller.getSnapshot,
    controller.getSnapshot,
  );
  useEffect(() => {
    if (!installed() || !session.account) return;
    const account = session.account;
    let disposed = false;
    void read<{ volume?: number; repeat?: 'off' | 'all' | 'one'; shuffle?: boolean }>(
      partition(account) + 'preferences',
    ).then((settings) => {
      if (disposed) return;
      if (settings?.volume !== undefined) controller.setVolume(settings.volume);
      controller.restoreSettings(settings || {});
      preferencesReady.current = account.key;
    });
    return () => {
      disposed = true;
      preferencesReady.current = '';
    };
  }, [controller, session.account]);
  useEffect(() => {
    if (!installed() || !session.account || preferencesReady.current !== session.account.key)
      return;
    void accountWrite(session.account, 'preferences', {
      volume: audio.volume,
      repeat: snapshot.repeat,
      shuffle: snapshot.shuffle,
    }).catch(() => {});
  }, [session.account, snapshot.repeat, snapshot.shuffle, audio]);
  const server = session.account?.server || session.grant?.server,
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
    const checkpoint = () => controller.checkpoint();
    if (Platform.OS === 'web') {
      window.addEventListener('pagehide', checkpoint);
      document.addEventListener('visibilitychange', checkpoint);
    }
    return () => {
      subscription.remove();
      app.remove();
      if (Platform.OS === 'web') {
        window.removeEventListener('pagehide', checkpoint);
        document.removeEventListener('visibilitychange', checkpoint);
      }
    };
  }, [audio, controller]);
  useEffect(() => {
    if (!api) return;
    const port: PlaybackPort = {
      async source(id) {
        if (preview)
          return { source: require('../../assets/preview.wav') as number, release: async () => {} };
        if (installed() && session.account) {
          const local = await localSource(session.account, id);
          if (local) return local;
        }
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
  }, [api, server, preview, controller, session.account]);
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
      setVolume: (volume) => {
        controller.setVolume(volume);
        if (installed() && session.account)
          void accountWrite(session.account, 'preferences', {
            volume,
            repeat: snapshot.repeat,
            shuffle: snapshot.shuffle,
          }).catch(() => {});
      },
    }),
    [audio, snapshot, controller, session.account],
  );
  return <Context.Provider value={value}>{children}</Context.Provider>;
}
export function usePlayer() {
  const player = useContext(Context);
  if (!player) throw new Error('Missing player provider');
  return player;
}
