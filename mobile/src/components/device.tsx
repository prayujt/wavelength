import React, { createContext, useContext, useEffect, useState } from 'react';
import { View } from 'react-native';
import { router } from 'expo-router';
import { Download, Check, WifiOff, X } from 'lucide-react-native';
import type { Track } from '../gen/wavelength/v1/library_pb';
import { useSession } from '../lib/session';
import { usePlayer } from '../lib/player';
import { changed, installed } from '../lib/device-store';
import {
  downloads,
  downloadTracks,
  removeDownload,
  type Download as Saved,
  type DownloadProgress,
  cancelDownloads,
} from '../lib/device-library';
import { color } from '../lib/theme';
import { Button, IconButton, Press, Txt } from './ui';
import { useFeedback } from './feedback';

type Device = {
  enabled: boolean;
  items: Saved[];
  progress?: DownloadProgress;
  error: string;
  busy: boolean;
  save(tracks: Track[]): Promise<void>;
  remove(id: string): Promise<void>;
};
const Context = createContext<Device>({
  enabled: false,
  items: [],
  error: '',
  busy: false,
  async save() {},
  async remove() {},
});
export const useDevice = () => useContext(Context);
export function DeviceProvider({ children }: { children: React.ReactNode }) {
  const session = useSession(),
    feedback = useFeedback();
  const [library, setLibrary] = useState<{ key: string; items: Saved[] }>({ key: '', items: [] }),
    [progress, setProgress] = useState<DownloadProgress>(),
    [error, setError] = useState(''),
    [busy, setBusy] = useState(false);
  const enabled = installed() && !!session.account && !session.preview;
  useEffect(() => {
    if (!enabled || !session.account) {
      return;
    }
    let disposed = false;
    const refresh = () => {
      void downloads(session.account!)
        .then((items) => {
          if (!disposed) setLibrary({ key: session.account!.key, items });
        })
        .catch(() => {
          if (!disposed) setError('Device storage could not be opened.');
        });
    };
    refresh();
    window.addEventListener('wavelength-device', refresh);
    return () => {
      disposed = true;
      cancelDownloads();
      window.removeEventListener('wavelength-device', refresh);
    };
  }, [enabled, session.account]);
  const value: Device = {
    enabled,
    items: enabled && library.key === session.account?.key ? library.items : [],
    progress,
    error,
    busy,
    async save(tracks) {
      if (!enabled || !session.account || !session.api || busy) return;
      setBusy(true);
      setError('');
      try {
        await downloadTracks(session.account, session.auth, session.api, tracks, (value) => {
          setProgress(value);
          if (value.error) setError(value.error);
        });
        feedback.notify('Saved on this device');
      } catch (error) {
        const message =
          error instanceof Error ? error.message : 'Download could not finish. Please retry.';
        setError((previous) => previous || message);
        feedback.notify(message);
      } finally {
        setBusy(false);
        setProgress(undefined);
        changed();
      }
    },
    async remove(id) {
      if (session.account) await removeDownload(session.account, id);
    },
  };
  return <Context.Provider value={value}>{children}</Context.Provider>;
}
export function DeviceDownload({ tracks }: { tracks: Track[] }) {
  const device = useDevice(),
    session = useSession();
  if (!device.enabled) return null;
  const complete =
    tracks.length > 0 &&
    tracks.every((track) => device.items.some((item) => item.track.id === track.id));
  return (
    <IconButton
      icon={complete ? Check : Download}
      label={complete ? 'Saved on this device' : 'Download collection to device'}
      active={complete}
      disabled={!tracks.length || device.busy || !session.online || complete}
      onPress={() => device.save(tracks)}
    />
  );
}
export function DeviceStatus() {
  const session = useSession(),
    device = useDevice(),
    player = usePlayer();
  const [waiting, setWaiting] = useState(false),
    [cached, setCached] = useState(false);
  const feedback = useFeedback();
  const [installPrompt, setInstallPrompt] = useState<Event & { prompt(): Promise<void> }>();
  useEffect(() => {
    if (typeof window === 'undefined' || typeof window.addEventListener !== 'function') return;
    const receive = (event: Event) => {
      event.preventDefault();
      setInstallPrompt(event as Event & { prompt(): Promise<void> });
    };
    window.addEventListener('beforeinstallprompt', receive);
    return () => window.removeEventListener('beforeinstallprompt', receive);
  }, []);
  useEffect(() => {
    if (
      typeof location === 'undefined' ||
      !location.pathname.startsWith('/app/') ||
      !('serviceWorker' in navigator)
    )
      return;
    let disposed = false;
    void navigator.serviceWorker
      .register('/app/sw.js', { scope: '/app/', updateViaCache: 'none' })
      .then((registration) => {
        const inspect = () => {
          if (!disposed) setWaiting(!!registration.waiting);
        };
        inspect();
        registration.addEventListener('updatefound', () =>
          registration.installing?.addEventListener('statechange', inspect),
        );
        void navigator.serviceWorker.ready.then(() => {
          if (!disposed) setCached(true);
        });
        void registration.update().catch(() => {});
      })
      .catch(() => {});
    return () => {
      disposed = true;
    };
  }, []);
  if (session.preview) return null;
  return (
    <View style={{ backgroundColor: color.surface }}>
      {typeof location !== 'undefined' && location.pathname.startsWith('/app/') && !installed() && (
        <Press
          label="Install Wavelength for offline listening"
          onPress={() =>
            installPrompt
              ? installPrompt.prompt()
              : feedback.notify(
                  'Use your browser’s Install or Add to Home Screen action, then open the Wavelength icon.',
                )
          }
          style={{ padding: 12 }}
        >
          <Txt size={12} style={{ color: color.accent }}>
            Install Wavelength for offline listening →
          </Txt>
        </Press>
      )}
      {!session.online && (
        <View style={{ padding: 12, flexDirection: 'row', gap: 10, alignItems: 'center' }}>
          <WifiOff size={15} color={color.accent} />
          <Txt size={12}>
            {device.enabled
              ? 'Offline · your downloaded music is ready'
              : 'Offline · connect to use Wavelength'}
          </Txt>
        </View>
      )}
      {!!session.account && !session.grant && (
        <Press
          label="Reconnect your account"
          onPress={() => session.online && session.login(session.account!.server)}
          style={{ padding: 12 }}
        >
          <Txt size={12} style={{ color: color.accent }}>
            Sign in again to reconnect. Your downloaded music is still here.
          </Txt>
        </Press>
      )}
      {device.busy && device.progress && (
        <View style={{ padding: 12, gap: 8 }}>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
            <Txt size={12} style={{ flex: 1 }} numberOfLines={1}>
              Saving {device.progress.title} ·{' '}
              {device.progress.total
                ? Math.round((device.progress.received / device.progress.total) * 100) + '%'
                : 'downloading'}
            </Txt>
            <IconButton
              icon={X}
              label="Cancel device download"
              onPress={cancelDownloads}
              size={16}
            />
          </View>
          <View style={{ height: 3, backgroundColor: color.line }}>
            <View
              style={{
                height: 3,
                width: `${device.progress.total ? Math.min(100, (device.progress.received / device.progress.total) * 100) : 0}%`,
                backgroundColor: color.accent,
              }}
            />
          </View>
        </View>
      )}
      {waiting && (
        <View style={{ padding: 12 }}>
          <Txt size={12}>
            {player.playing
              ? 'An update is ready. Finish listening, then close all Wavelength windows to update.'
              : 'An update is ready. Close all Wavelength windows and reopen to update. Your music stays saved.'}
          </Txt>
        </View>
      )}
      {device.enabled && !cached && session.online && (
        <View style={{ padding: 8 }}>
          <Txt muted size={11}>
            Preparing Wavelength for offline launch…
          </Txt>
        </View>
      )}
    </View>
  );
}
export function DeviceLibraryLink() {
  const device = useDevice();
  if (!device.enabled) return null;
  return (
    <View style={{ paddingHorizontal: 24, paddingBottom: 12 }}>
      <Button
        title={`On this device · ${device.items.length} songs`}
        icon={Download}
        onPress={() => router.push('/on-device')}
      />
    </View>
  );
}
