import { toJson, fromJson, type JsonObject } from '@bufbuild/protobuf';
import { TrackSchema, type Track } from '../gen/wavelength/v1/library_pb';
import type { API } from './api';
import type { OAuthSession } from '../core/auth';
import { Code, ConnectError } from '@connectrpc/connect';
import { authenticatedURL } from '../core/auth';
import {
  assertAccount,
  changed,
  entries,
  erase,
  exclusive,
  installed,
  MUSIC_CACHE,
  online,
  partition,
  read,
  resourceKey,
  write,
  type LocalAccount,
} from './device-store';

export type Download = {
  track: JsonObject;
  audio: string;
  artwork?: string;
  bytes: number;
  version: string;
  lyrics: string;
};
export type DownloadProgress = {
  id: string;
  title: string;
  received: number;
  total: number;
  error?: string;
};
const active = new Map<string, AbortController>();
export function cancelDownloads() {
  for (const controller of active.values()) controller.abort();
}
export async function downloads(account: LocalAccount): Promise<Download[]> {
  const cache = await caches.open(MUSIC_CACHE),
    result: Download[] = [];
  for (const [key, item] of await entries<Download>(partition(account) + 'track:')) {
    if (await cache.match(item.audio)) result.push(item);
    else await erase(key); // Browser eviction must never leave a false downloaded indicator.
  }
  return result;
}
export async function saved(account: LocalAccount, id: string) {
  const record = await read<Download>(partition(account) + 'track:' + id);
  if (!record) return undefined;
  const response = await (await caches.open(MUSIC_CACHE)).match(record.audio);
  if (!response) {
    await erase(partition(account) + 'track:' + id);
    changed();
    return undefined;
  }
  return { record, response };
}
export async function localSource(account: LocalAccount, id: string) {
  const item = await saved(account, id);
  if (!item) return undefined;
  const uri = URL.createObjectURL(await item.response.blob());
  return { source: { uri }, release: async () => URL.revokeObjectURL(uri) };
}
export async function localArtwork(account: LocalAccount, path: string) {
  // Artwork lookup must not open every saved audio response for every visible
  // cover. Only the small metadata records and the matching image are needed.
  const item = (await entries<Download>(partition(account) + 'track:'))
    .map(([, item]) => item)
    .find((item) => item.track.artworkPath === path);
  const response = item?.artwork && (await (await caches.open(MUSIC_CACHE)).match(item.artwork));
  return response ? URL.createObjectURL(await response.blob()) : undefined;
}
export async function removeDownload(account: LocalAccount, id: string) {
  await exclusive('data:' + account.key, async () => {
    const key = partition(account) + 'track:' + id,
      record = await read<Download>(key);
    await erase(key);
    if (record) {
      const cache = await caches.open(MUSIC_CACHE);
      await cache.delete(record.audio);
      if (record.artwork) await cache.delete(record.artwork);
    }
  });
  changed();
}
export async function downloadTracks(
  account: LocalAccount,
  auth: OAuthSession,
  api: API,
  tracks: Track[],
  progress: (value: DownloadProgress) => void,
) {
  if (!installed()) throw new Error('Install Wavelength to save music on this device.');
  if (!online()) throw new Error('Connect to download music. Your saved songs are ready offline.');
  await navigator.storage?.persist?.().catch(() => false); // Denial is supported; never promise immunity from eviction.
  for (const track of tracks) {
    const jobKey = account.key + ':' + track.id;
    if (active.has(jobKey)) continue;
    const controller = new AbortController();
    active.set(jobKey, controller);
    let cacheKey: string | undefined;
    try {
      await exclusive('download:' + jobKey, async () => {
        await assertAccount(account);
        const info = await api.library.getPlaybackInfo({ id: track.id });
        const existing = await saved(account, track.id);
        if (existing?.record.version.startsWith(info.httpPath + ':')) return;
        const token = await auth.token();
        const response = await fetch(authenticatedURL(account.server, info.httpPath), {
          headers: { Authorization: 'Bearer ' + token },
          credentials: 'omit',
          redirect: 'error',
          cache: 'no-store',
          signal: controller.signal,
        });
        if (response.status !== 200 || !response.headers.get('Content-Type')?.startsWith('audio/'))
          throw new Error('The server did not return a complete audio file.');
        const current = info.track || track,
          expected = Number(current.sizeBytes);
        const total = Number(response.headers.get('Content-Length')) || expected;
        const chunks: Uint8Array<ArrayBuffer>[] = [];
        let received = 0;
        const reader = response.body?.getReader();
        if (!reader) throw new Error('This browser could not save the audio file.');
        progress({ id: track.id, title: track.title, received, total });
        for (;;) {
          const { done, value } = await reader.read();
          if (done) break;
          chunks.push(new Uint8Array(value));
          received += value.byteLength;
          progress({ id: track.id, title: track.title, received, total });
        }
        if (
          !received ||
          (expected > 0 && received !== expected) ||
          (total > 0 && received !== total)
        )
          throw new Error('Download was interrupted. Try this song again.');
        let lyrics = '';
        try {
          lyrics = (await api.library.getLyrics({ id: track.id })).lyrics;
        } catch (error) {
          if (ConnectError.from(error).code !== Code.NotFound) throw error;
        }
        const version = info.httpPath + ':' + (response.headers.get('ETag') || '');
        const audio = resourceKey(account, track.id, version),
          artwork = resourceKey(account, track.id, version, 'artwork');
        const cache = await caches.open(MUSIC_CACHE);
        let art: Response | undefined;
        if (current.artworkPath) {
          const result = await fetch(authenticatedURL(account.server, current.artworkPath), {
            headers: { Authorization: 'Bearer ' + (await auth.token()) },
            credentials: 'omit',
            redirect: 'error',
            signal: controller.signal,
          });
          if (result.ok && result.headers.get('Content-Type')?.startsWith('image/'))
            art = new Response(await result.blob(), {
              headers: { 'Content-Type': result.headers.get('Content-Type')! },
            });
        }
        await exclusive('data:' + account.key, async () => {
          controller.signal.throwIfAborted();
          await assertAccount(account);
          const old = await read<Download>(partition(account) + 'track:' + track.id);
          cacheKey = audio;
          await cache.put(
            audio,
            new Response(new Blob(chunks, { type: response.headers.get('Content-Type')! }), {
              headers: {
                'Content-Type': response.headers.get('Content-Type')!,
                'Content-Length': String(received),
              },
            }),
          );
          if (art) await cache.put(artwork, art);
          controller.signal.throwIfAborted();
          await assertAccount(account);
          await write(partition(account) + 'track:' + track.id, {
            track: toJson(TrackSchema, current) as JsonObject,
            audio,
            artwork: art ? artwork : undefined,
            bytes: received,
            version,
            lyrics,
          } satisfies Download);
          cacheKey = undefined;
          if (old && old.audio !== audio) {
            await cache.delete(old.audio);
            if (old.artwork) await cache.delete(old.artwork);
          }
        });
        changed();
      });
    } catch (error) {
      if (cacheKey) {
        const cache = await caches.open(MUSIC_CACHE);
        await cache.delete(cacheKey);
        await cache.delete(cacheKey.replace(/\/audio$/, '/artwork'));
      }
      progress({
        id: track.id,
        title: track.title,
        received: 0,
        total: Number(track.sizeBytes),
        error:
          error instanceof DOMException && error.name === 'QuotaExceededError'
            ? 'Device storage is full. Remove some downloads and retry.'
            : error instanceof Error
              ? error.message
              : 'Could not download this song.',
      });
      throw error;
    } finally {
      active.delete(jobKey);
    }
  }
}
export const decodedTracks = (items: Download[]) =>
  items.map((item) => fromJson(TrackSchema, item.track));
