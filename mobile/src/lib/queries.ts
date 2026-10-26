import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Code, ConnectError } from '@connectrpc/connect';
import { useEffect, useState } from 'react';
import { AppState } from 'react-native';
import { useAPI, useSession } from './session';
import type { Track } from '../gen/wavelength/v1/library_pb';

export function useUser() {
  const api = useAPI();
  return useQuery({ queryKey: ['user'], queryFn: () => api.user.getCurrentUser({}) });
}
export function usePlaylists() {
  const api = useAPI();
  return useQuery({
    queryKey: ['playlists'],
    queryFn: () => api.user.listPlaylists({ limit: 500 }),
  });
}
export function useTracks(ids: readonly string[]) {
  const api = useAPI(),
    cache = useQueryClient();
  return useQuery({
    queryKey: ['tracks-by-id', ids],
    placeholderData: (previous: Track[] | undefined) => {
      const retained = ids.flatMap((id) => previous?.find((track) => track.id === id) || []);
      return retained.length ? retained : undefined;
    },
    queryFn: async () => {
      const result: Track[] = [];
      // Preserve playlist order and duplicates while bounding requests for large collections.
      for (let i = 0; i < ids.length; i += 8) {
        const batch = await Promise.all(
          ids.slice(i, i + 8).map(async (id) => {
            try {
              return await cache.fetchQuery({
                queryKey: ['track', id],
                queryFn: async () => (await api.library.getTrack({ id })).track,
              });
            } catch (error) {
              if (ConnectError.from(error).code === Code.NotFound) return undefined;
              throw error;
            }
          }),
        );
        result.push(...batch.filter((x): x is Track => Boolean(x)));
        // A large playlist becomes usable after its first batch, instead of
        // holding the whole screen behind hundreds of per-track RPCs.
        if (result.length) cache.setQueryData(['tracks-by-id', ids], [...result]);
      }
      return result;
    },
  });
}
export function useImports() {
  const api = useAPI();
  const { online, preview } = useSession();
  return useQuery({
    queryKey: ['imports'],
    enabled: online || preview,
    queryFn: () => api.ingestion.listImports({ limit: 100 }),
    refetchInterval: (query) =>
      query.state.data?.imports.some((j) => ['queued', 'running'].includes(j.state)) ? 3000 : 15000,
  });
}
export function useLibraryPulse(onLibraryChanged?: () => void) {
  const api = useAPI(),
    cache = useQueryClient();
  const { preview, online } = useSession();
  const [live, setLive] = useState(false);
  useEffect(() => {
    if (preview || !online) return;
    let stopped = false,
      connection: AbortController | undefined,
      refresh: ReturnType<typeof setTimeout> | undefined;
    const keys = new Set<string>();
    const invalidate = (...names: string[]) => {
      for (const name of names) keys.add(name);
      if (!refresh)
        refresh = setTimeout(() => {
          for (const key of keys) void cache.invalidateQueries({ queryKey: [key] });
          keys.clear();
          refresh = undefined;
        }, 200);
    };
    const delay = (signal: AbortSignal, ms: number) =>
      new Promise<void>((resolve) => {
        const done = () => {
          clearTimeout(timer);
          signal.removeEventListener('abort', done);
          resolve();
        };
        const timer = setTimeout(done, ms);
        signal.addEventListener('abort', done, { once: true });
      });
    const connect = () => {
      if (stopped || (connection && !connection.signal.aborted)) return;
      const controller = new AbortController();
      connection = controller;
      void (async () => {
        let backoff = 2000;
        while (!controller.signal.aborted && !stopped) {
          try {
            for await (const event of api.realtime.subscribe({}, { signal: controller.signal })) {
              if (controller.signal.aborted || stopped) break;
              if (event.payload.case === 'ready') {
                onLibraryChanged?.();
                setLive(true);
                backoff = 2000;
                invalidate(
                  'library',
                  'album',
                  'artist',
                  'playlists',
                  'imports',
                  'imported',
                  'listening',
                  'playlist',
                  'track',
                  'tracks-by-id',
                  'release-matches',
                  'bookmarks',
                  'shares',
                );
              }
              if (event.payload.case === 'importUpdated') invalidate('imports');
              if (event.payload.case === 'libraryUpdated') {
                onLibraryChanged?.();
                invalidate(
                  'library',
                  'album',
                  'artist',
                  'track',
                  'tracks-by-id',
                  'imported',
                  'release-matches',
                );
              }
            }
          } catch {
            /* Restricted networks can fall back to collection polling. */
          }
          if (controller.signal.aborted || stopped) break;
          setLive(false);
          await delay(controller.signal, backoff);
          backoff = Math.min(30000, backoff * 2);
        }
      })();
    };
    connect();
    const app = AppState.addEventListener('change', (value) => {
      if (value === 'active') connect();
      else {
        connection?.abort();
        setLive(false);
      }
    });
    return () => {
      stopped = true;
      connection?.abort();
      app.remove();
      if (refresh) clearTimeout(refresh);
    };
  }, [api, cache, preview, online, onLibraryChanged]);
  return useQuery({
    queryKey: ['library-pulse'],
    queryFn: async () => {
      const result = await api.ingestion.listImports({ limit: 100 });
      const version = result.imports
        .filter((j) => ['completed', 'partial'].includes(j.state))
        .map((j) => `${j.id}:${j.updatedAt?.seconds}`)
        .join('|');
      const previous = cache.getQueryData<string>(['library-version']);
      if (previous !== undefined && previous !== version) {
        onLibraryChanged?.();
        for (const key of [
          'library',
          'album',
          'artist',
          'imports',
          'imported',
          'release-matches',
          'playlists',
        ])
          void cache.invalidateQueries({ queryKey: [key] });
      }
      cache.setQueryData(['library-version'], version);
      return version;
    },
    refetchInterval: 12000,
    enabled: preview || (online && !live),
  });
}
