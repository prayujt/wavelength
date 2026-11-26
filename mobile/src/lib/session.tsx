import React, { createContext, useContext, useEffect, useMemo, useState } from 'react';
import { AppState, Platform } from 'react-native';
import * as SecureStore from 'expo-secure-store';
import * as Crypto from 'expo-crypto';
import * as Linking from 'expo-linking';
import * as WebBrowser from 'expo-web-browser';
import { fetch as nativeFetch } from 'expo/fetch';
import {
  focusManager,
  onlineManager,
  QueryClient,
  QueryClientProvider,
} from '@tanstack/react-query';
import { OAuthSession, type Grant, type Vault } from '../core/auth';
import { createAPI, type API } from './api';
import * as device from './device-store';
import { deviceAPI, syncHistory } from './device-api';
import { cancelDownloads } from './device-library';

function pendingFromTab(key: string, state: string) {
  try {
    const pending = sessionStorage.getItem(key);
    if (pending && JSON.parse(pending).state === state) return pending;
  } catch {
    // The durable per-state record also supports a callback in another window.
  }
  return null;
}

const vault: Vault = {
  async get(key, state) {
    if (Platform.OS !== 'web') return SecureStore.getItemAsync(key);
    if (!key.endsWith('pending.v1'))
      return (await device.read<string>('credential:' + key)) ?? null;
    if (!state) return null;
    return pendingFromTab(key, state) ?? (await device.read<string>('pending:' + state)) ?? null;
  },
  async set(key, value) {
    if (Platform.OS === 'web') {
      if (key.endsWith('pending.v1')) {
        const pending = JSON.parse(value) as { state: string; expiresAt: number };
        let savedInTab = false;
        try {
          sessionStorage.setItem(key, value);
          savedInTab = true;
        } catch {
          // Authorization can still return through the durable record.
        }
        try {
          await device.write('pending:' + pending.state, value);
        } catch (error) {
          if (!savedInTab) throw error;
          return;
        }
        for (const [oldKey, raw] of await device.entries<string>('pending:').catch(() => []))
          if (JSON.parse(raw).expiresAt < Date.now()) await device.erase(oldKey).catch(() => {});
      } else {
        await device.write('credential:' + key, value);
        device.announce();
      }
    } else
      await SecureStore.setItemAsync(key, value, {
        keychainAccessible: SecureStore.WHEN_UNLOCKED_THIS_DEVICE_ONLY,
      });
  },
  async remove(key, state) {
    if (Platform.OS === 'web') {
      if (key.endsWith('pending.v1')) {
        globalThis.sessionStorage?.removeItem(key);
        if (state) await device.erase('pending:' + state);
        else
          for (const [pendingKey] of await device.entries('pending:'))
            await device.erase(pendingKey);
      } else {
        await device.erase('credential:' + key);
        device.announce();
      }
    } else await SecureStore.deleteItemAsync(key);
  },
};
if (Platform.OS === 'web') {
  vault.lock = (run) => device.exclusive('credentials', run);
  vault.commitSignIn = async (grantKey, value, pendingKey, state) => {
    const credential = 'credential:' + grantKey;
    try {
      await device.write(credential, value, 'pending:' + state);
    } catch (error) {
      if (!(error instanceof device.DeviceStorageTimeout && error.mayHaveCommitted))
        return 'temporary';
      const saved = await device.read<string>(credential).catch(() => undefined);
      if (saved !== value) return 'temporary';
    } finally {
      try {
        if (pendingFromTab(pendingKey, state)) sessionStorage.removeItem(pendingKey);
      } catch {
        // The completed OAuth code can only be used once.
      }
    }
    device.announce();
  };
}
function base64url(bytes: Uint8Array) {
  const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_';
  let out = '';
  for (let i = 0; i < bytes.length; i += 3) {
    const n = (bytes[i] << 16) | ((bytes[i + 1] || 0) << 8) | (bytes[i + 2] || 0);
    out += chars[(n >>> 18) & 63] + chars[(n >>> 12) & 63];
    if (i + 1 < bytes.length) out += chars[(n >>> 6) & 63];
    if (i + 2 < bytes.length) out += chars[n & 63];
  }
  return out;
}
type Session = {
  auth: OAuthSession;
  grant: Grant | null;
  ready: boolean;
  preview: boolean;
  account: device.LocalAccount | null;
  online: boolean;
  api: API | null;
  login(server: string): Promise<void>;
  complete(url: string): Promise<void>;
  enterPreview(): void;
  logout(): Promise<void>;
  error: string;
  setError(value: string): void;
};
const Context = createContext<Session | null>(null);
export const DEFAULT_SERVER =
  Platform.OS === 'web' && typeof location !== 'undefined' && location.pathname.startsWith('/app/')
    ? location.origin
    : process.env.EXPO_PUBLIC_WAVELENGTH_URL || 'https://wavelength.prayujt.com';

export function SessionProvider({ children }: { children: React.ReactNode }) {
  const [grant, setGrant] = useState<Grant | null>(null),
    [account, setAccount] = useState<device.LocalAccount | null>(null),
    [online, setOnline] = useState(device.online),
    [ready, setReady] = useState(false),
    [preview, setPreview] = useState(false),
    [error, setError] = useState('');
  const [queries] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            staleTime: 20000,
            retry: 1,
            refetchOnWindowFocus: false,
            networkMode: 'always',
          },
          mutations: { retry: false, networkMode: 'always' },
        },
      }),
  );
  const [auth] = useState(
    () =>
      new OAuthSession(
        vault,
        {
          random: () => base64url(Crypto.getRandomBytes(32)),
          challenge: async (value) =>
            base64url(
              new Uint8Array(
                await Crypto.digest(
                  Crypto.CryptoDigestAlgorithm.SHA256,
                  new TextEncoder().encode(value),
                ),
              ),
            ),
        },
        nativeFetch as typeof fetch,
        (value) => {
          if (!value && Platform.OS !== 'web') queries.clear();
          setGrant(value);
        },
      ),
  );
  const activeAccount =
    auth.temporary ||
    (grant && (grant.accountId !== account?.user.id || grant.server !== account?.server))
      ? null
      : account;
  const server = activeAccount?.server || grant?.server;
  const identity = activeAccount?.key;
  const version = Platform.OS === 'web' && activeAccount ? -1 : auth.version;
  const api = useMemo(
    () => {
      if (!server && !preview) return null;
      const raw = createAPI(auth, preview, version, server, activeAccount?.user.id);
      return Platform.OS === 'web' && activeAccount && !preview
        ? deviceAPI(raw, activeAccount)
        : raw;
    },
    // Token rotation and connectivity do not tear down a playing local source.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [server, preview, auth, identity, version],
  );

  useEffect(() => {
    void (async () => {
      const restored = await auth.restore();
      if (Platform.OS === 'web' && !auth.temporary) {
        const local = await device.currentAccount();
        if (auth.temporary) return;
        if (
          local &&
          ((!restored && device.installed()) ||
            (restored?.accountId === local.user.id && restored.server === local.server))
        )
          setAccount(local);
        if (
          restored &&
          (!local || restored.accountId !== local.user.id || restored.server !== local.server)
        ) {
          const user = await createAPI(auth).user.getCurrentUser({});
          if (user.user) {
            await auth.identifyAccount(user.user.id);
            setAccount(await device.remember(restored.server, user.user));
          }
        }
      }
    })()
      .catch(() => setError('Could not restore your session. Please sign in again.'))
      .finally(() => setReady(true));
  }, [auth]);
  useEffect(() => {
    if (Platform.OS !== 'web') return;
    const connectivity = () => {
      setOnline(device.online());
      onlineManager.setOnline(device.online());
    };
    const channel =
      typeof BroadcastChannel === 'undefined'
        ? undefined
        : new BroadcastChannel('wavelength-device');
    if (channel)
      channel.onmessage = () => {
        if (auth.temporary) return;
        void auth
          .restore()
          .then(() => device.currentAccount())
          .then((local) => {
            if (auth.temporary) return;
            setAccount((previous) => {
              if (previous?.key !== local?.key) queries.clear();
              return previous?.key === local?.key ? previous : local || null;
            });
          })
          .catch(() => {});
      };
    connectivity();
    window.addEventListener('online', connectivity);
    window.addEventListener('offline', connectivity);
    return () => {
      channel?.close();
      window.removeEventListener('online', connectivity);
      window.removeEventListener('offline', connectivity);
    };
  }, [queries, auth]);
  useEffect(() => {
    if (Platform.OS !== 'web' || !activeAccount || !online || preview) return;
    const sync = () => {
      void syncHistory(
        activeAccount,
        createAPI(auth, false, auth.version, activeAccount.server),
      ).catch(() => {});
    };
    sync();
    const timer = setInterval(sync, 30000);
    return () => clearInterval(timer);
  }, [activeAccount, online, preview, auth]);
  useEffect(() => {
    if (Platform.OS === 'web') return;
    focusManager.setFocused(AppState.currentState === 'active');
    const subscription = AppState.addEventListener('change', (state) =>
      focusManager.setFocused(state === 'active'),
    );
    return () => subscription.remove();
  }, []);

  async function complete(url: string) {
    const connected = await auth.complete(url);
    if (Platform.OS === 'web') {
      const user = await createAPI(auth).user.getCurrentUser({});
      if (!user.user?.id) throw new Error('Wavelength could not identify this account.');
      await auth.identifyAccount(user.user.id);
      setAccount(auth.temporary ? null : await device.remember(connected.server, user.user));
    }
    queries.clear();
    setPreview(false);
    setError('');
    if (Platform.OS === 'web')
      window.history.replaceState({}, '', browserBase() + '/oauth/callback');
    else {
      try {
        WebBrowser.dismissBrowser();
      } catch {
        /* Android browser dismisses on return. */
      }
    }
  }
  useEffect(() => {
    const receive = ({ url }: { url: string }) => {
      if (!url.includes('/oauth/callback') || !/[?&](code|error)=/.test(url)) return;
      void complete(url).catch((error) =>
        setError(error instanceof Error ? error.message : 'Sign-in could not finish.'),
      );
    };
    void Linking.getInitialURL().then((url) => {
      if (url) receive({ url });
    });
    const subscription = Linking.addEventListener('url', receive);
    return () => subscription.remove();
    // The OAuthSession instance, storage and query cache are stable for this app lifetime.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [auth]);

  const session: Session = {
    auth,
    grant,
    ready,
    preview,
    account: activeAccount,
    online,
    api,
    error,
    setError,
    async login(server) {
      setError('');
      const returnUri =
        Platform.OS === 'web'
          ? window.location.origin + browserBase() + '/oauth/callback'
          : Linking.createURL('oauth/callback', { scheme: 'wavelength' });
      const url = await auth.begin(server, returnUri, Platform.OS === 'web');
      if (Platform.OS === 'web') {
        // A suspended WebKit document can hold up IndexedDB after the OAuth return.
        await device.closeDatabase();
        window.location.assign(url);
      } else
        await WebBrowser.openBrowserAsync(url, {
          toolbarColor: '#0A1111',
          controlsColor: '#C7F3CB',
          showTitle: false,
          enableBarCollapsing: true,
        });
    },
    enterPreview() {
      queries.clear();
      setError('');
      setPreview(true);
    },
    async logout() {
      cancelDownloads();
      const local = activeAccount;
      setAccount(null);
      setPreview(false);
      queries.clear();
      if (Platform.OS === 'web' && local) {
        await device.erase('active-account');
        device.announce();
        await device.exclusive('data:' + local.key, () =>
          device.exclusive('events:' + local.key, () => device.clearAccount(local)),
        );
      }
      await auth.logout();
    },
    complete,
  };
  return (
    <Context.Provider value={session}>
      <QueryClientProvider client={queries}>{children}</QueryClientProvider>
    </Context.Provider>
  );
}
export function browserBase() {
  return typeof location !== 'undefined' && location.pathname.startsWith('/app') ? '/app' : '';
}
export function useSession() {
  const value = useContext(Context);
  if (!value) throw new Error('Missing session provider');
  return value;
}
export function useAPI() {
  const api = useSession().api;
  if (!api) throw new Error('Sign in to continue');
  return api;
}
