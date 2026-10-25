import React, { createContext, useContext, useEffect, useMemo, useState } from 'react';
import { AppState, Platform } from 'react-native';
import * as SecureStore from 'expo-secure-store';
import * as Crypto from 'expo-crypto';
import * as Linking from 'expo-linking';
import * as WebBrowser from 'expo-web-browser';
import { fetch as nativeFetch } from 'expo/fetch';
import { focusManager, QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { OAuthSession, type Grant, type Vault } from '../core/auth';
import { createAPI, type API } from './api';

const vault: Vault = {
  async get(key) {
    return Platform.OS === 'web'
      ? (globalThis.sessionStorage?.getItem(key) ?? null)
      : SecureStore.getItemAsync(key);
  },
  async set(key, value) {
    if (Platform.OS === 'web') globalThis.sessionStorage?.setItem(key, value);
    else
      await SecureStore.setItemAsync(key, value, {
        keychainAccessible: SecureStore.WHEN_UNLOCKED_THIS_DEVICE_ONLY,
      });
  },
  async remove(key) {
    if (Platform.OS === 'web') globalThis.sessionStorage?.removeItem(key);
    else await SecureStore.deleteItemAsync(key);
  },
};
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
  process.env.EXPO_PUBLIC_WAVELENGTH_URL || 'https://wavelength.prayujt.com';

export function SessionProvider({ children }: { children: React.ReactNode }) {
  const [grant, setGrant] = useState<Grant | null>(null),
    [ready, setReady] = useState(false),
    [preview, setPreview] = useState(false),
    [error, setError] = useState('');
  const [queries] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: { staleTime: 20000, retry: 1, refetchOnWindowFocus: false },
          mutations: { retry: false },
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
          if (!value) queries.clear();
          setGrant(value);
        },
      ),
  );
  const server = grant?.server;
  const version = auth.version;
  const api = useMemo(
    () => (server || preview ? createAPI(auth, preview, version) : null),
    [server, preview, auth, version],
  );

  useEffect(() => {
    void auth
      .restore()
      .catch(() => setError('Could not restore your session. Please sign in again.'))
      .finally(() => setReady(true));
  }, [auth]);
  useEffect(() => {
    if (Platform.OS === 'web') return;
    focusManager.setFocused(AppState.currentState === 'active');
    const subscription = AppState.addEventListener('change', (state) =>
      focusManager.setFocused(state === 'active'),
    );
    return () => subscription.remove();
  }, []);

  async function complete(url: string) {
    await auth.complete(url);
    queries.clear();
    setPreview(false);
    setError('');
    if (Platform.OS === 'web') window.history.replaceState({}, '', '/oauth/callback');
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
    api,
    error,
    setError,
    async login(server) {
      setError('');
      const returnUri =
        Platform.OS === 'web'
          ? window.location.origin + '/oauth/callback'
          : Linking.createURL('oauth/callback', { scheme: 'wavelength' });
      const url = await auth.begin(server, returnUri, Platform.OS === 'web');
      if (Platform.OS === 'web') window.location.assign(url);
      else
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
      setPreview(false);
      queries.clear();
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
