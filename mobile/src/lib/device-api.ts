import {
  create,
  fromJson,
  toJson,
  type DescService,
  type JsonObject,
  type JsonValue,
} from '@bufbuild/protobuf';
import { Code, ConnectError } from '@connectrpc/connect';
import { LibraryService, UserDataService } from '../gen/wavelength/v1/library_pb';
import { ListeningService } from '../gen/wavelength/v1/listening_pb';
import { DiscoveryService, IngestionService } from '../gen/wavelength/v1/ingestion_pb';
import { SharingService } from '../gen/wavelength/v1/sharing_pb';
import type { API } from './api';
import { downloads, type Download } from './device-library';
import {
  accountWrite,
  assertAccount,
  changed,
  entries,
  erase,
  exclusive,
  installed,
  online,
  partition,
  read,
  write,
  type LocalAccount,
} from './device-store';

type Event = { method: string; input: JsonObject; at: string };
type Callable = (request: unknown, options?: unknown) => Promise<unknown>;
const emptyOffline = new ConnectError(
  'Connect to use this feature. Downloaded music is available in On this device.',
  Code.Unavailable,
);
const localizable = (error: unknown) =>
  [Code.Unavailable, Code.Unknown, Code.Unauthenticated, Code.DeadlineExceeded].includes(
    ConnectError.from(error).code,
  ) || error instanceof TypeError;
const wire = (value: unknown) => value as JsonObject;
const page = (items: JsonObject[], input: JsonObject, field: string) => ({
  [field]: items.slice(
    Number(input.offset || 0),
    Number(input.offset || 0) + Number(input.limit || 100),
  ),
  total: items.length,
});
function albums(items: Download[]) {
  const groups = new Map<string, JsonObject>();
  for (const { track: t } of items) {
    const id = String(t.albumId || ''),
      old = groups.get(id);
    groups.set(id, {
      id,
      name: t.album || '',
      artist: t.albumArtist || t.artist || '',
      artistId: t.artistId || '',
      year: t.year || 0,
      genre: t.genre || '',
      artworkPath: t.artworkPath || '',
      trackCount: Number(old?.trackCount || 0) + 1,
      durationSeconds: Number(old?.durationSeconds || 0) + Number(t.durationSeconds || 0),
    });
  }
  return [...groups.values()];
}
async function localRead(
  account: LocalAccount,
  service: string,
  method: string,
  input: JsonObject,
): Promise<JsonValue | undefined> {
  const prefix = partition(account);
  if (service === 'user' && method === 'getCurrentUser')
    return { user: { id: account.user.id, name: account.user.name, email: account.user.email } };
  if (service === 'user' && method === 'getQueue')
    return { queue: (await read<JsonObject>(prefix + 'queue')) || {} };
  if (service === 'library') {
    const items = await downloads(account),
      all = items.map((item) => item.track);
    const query = String(input.query || '').toLowerCase();
    const tracks = all.filter((t) =>
      [t.title, t.artist, t.album].join(' ').toLowerCase().includes(query),
    );
    const releases = albums(items).filter(
      (a) =>
        (!input.artistId || a.artistId === input.artistId) &&
        [a.name, a.artist].join(' ').toLowerCase().includes(query),
    );
    if (method === 'listTracks') return page(tracks, input, 'tracks');
    if (method === 'listAlbums') return page(releases, input, 'albums');
    const artists = [
      ...new Map(
        all.map((t) => [
          t.artistId,
          {
            id: t.artistId || '',
            name: t.albumArtist || t.artist || '',
            albumCount: albums(items).filter((a) => a.artistId === t.artistId).length,
            artworkPath: t.artworkPath || '',
          },
        ]),
      ).values(),
    ];
    if (method === 'listArtists')
      return page(
        artists.filter((a) => String(a.name).toLowerCase().includes(query)),
        input,
        'artists',
      );
    if (method === 'getTrack') return { track: all.find((t) => t.id === input.id) || null };
    if (method === 'getAlbum')
      return {
        album: releases.find((a) => a.id === input.id) || null,
        tracks: all
          .filter((t) => t.albumId === input.id)
          .sort(
            (a, b) =>
              Number(a.discNumber || 0) - Number(b.discNumber || 0) ||
              Number(a.trackNumber || 0) - Number(b.trackNumber || 0),
          ),
      };
    if (method === 'getArtist')
      return {
        artist: artists.find((a) => a.id === input.id) || null,
        albums: releases.filter((a) => a.artistId === input.id),
      };
    if (method === 'getLyrics')
      return { lyrics: items.find((item) => item.track.id === input.id)?.lyrics || '' };
  }
  return read<JsonValue>(prefix + 'rpc:' + service + ':' + method + ':' + JSON.stringify(input));
}

async function enqueue(account: LocalAccount, method: string, input: JsonObject) {
  const id = String(input.requestId || input.sessionId),
    order =
      method === 'startListeningSession' ? '0' : method === 'reportListeningProgress' ? '1' : '2';
  const key = partition(account) + 'outbox:' + id + ':' + order;
  await exclusive('events:' + account.key, async () => {
    await assertAccount(account);
    if (order === '0' && (await read(key))) return;
    await write(key, {
      method,
      input: {
        ...input,
        occurredAt: input.occurredAt || new Date().toISOString(),
        ...(order === '0' ? { countPlay: true } : {}),
      },
      at: new Date().toISOString(),
    } satisfies Event);
  });
  changed();
}
export async function syncHistory(account: LocalAccount, raw: API) {
  if (!installed() || !online()) return;
  await exclusive('sync:' + account.key, async () => {
    await assertAccount(account);
    // Validate stable identity before replay, even after signing back in on this device.
    const user = await raw.user.getCurrentUser({});
    if (user.user?.id !== account.user.id)
      throw new Error('Sign in to the account that owns this device library.');
    for (const [key, event] of await entries<Event>(partition(account) + 'outbox:')) {
      if (!online()) return;
      await assertAccount(account);
      const method = (ListeningService as DescService).method[event.method];
      try {
        await (raw.listening[event.method as keyof typeof raw.listening] as Callable)(
          fromJson(method.input, event.input),
        );
        await exclusive('events:' + account.key, async () => {
          if (JSON.stringify(await read(key)) === JSON.stringify(event)) await erase(key);
        });
      } catch (error) {
        const code = ConnectError.from(error).code;
        if (![Code.NotFound, Code.InvalidArgument, Code.FailedPrecondition].includes(code))
          throw error;
        // Deleted tracks / invalid historical inputs cannot be replayed. Retain a
        // visible failed record for review, without blocking other sessions forever.
        await write(key.replace('outbox:', 'failed:'), {
          ...event,
          error: ConnectError.from(error).rawMessage,
        });
        await erase(key);
      }
    }
    changed();
  });
}

export function deviceAPI(raw: API, account: LocalAccount): API {
  const wrap = <T extends object>(service: DescService, name: string, client: T): T =>
    new Proxy(client, {
      get(target, property, receiver) {
        const method = service.method[String(property)];
        if (!method || method.methodKind !== 'unary')
          return Reflect.get(target, property, receiver);
        return async (request: Record<string, unknown> = {}, options?: unknown) => {
          if (!installed()) return (Reflect.get(target, property) as Callable)(request, options);
          await assertAccount(account);
          const input = toJson(method.input, create(method.input, request)) as JsonObject;
          if (name === 'listening' && property !== 'listListeningSessions') {
            await enqueue(account, String(property), input);
            void syncHistory(account, raw).catch(() => {});
            return create(method.output);
          }
          if (name === 'user' && property === 'saveQueue') {
            // The installed app's device queue wins locally; never overwrite another
            // device's server queue automatically when this one reconnects.
            await accountWrite(account, 'queue', input);
            return fromJson(method.output, { queue: input });
          }
          if (name === 'user' && property === 'getQueue') {
            const queue = await read<JsonObject>(partition(account) + 'queue');
            if (queue) return fromJson(method.output, { queue });
          }
          if (name === 'user' && property === 'recordPlay') {
            // count_play on the idempotent detailed session owns aggregate counts.
            if (online())
              void raw.user.recordPlay({ ...request, nowPlayingOnly: true }).catch(() => {});
            return create(method.output);
          }
          const isRead = /^(get|list|search)/.test(String(property));
          if (online()) {
            try {
              const result = await (Reflect.get(target, property) as Callable)(request, options);
              if (isRead && ['library', 'user', 'listening'].includes(name)) {
                const value = toJson(method.output, result as ReturnType<typeof create>);
                await assertAccount(account);
                await accountWrite(
                  account,
                  'rpc:' + name + ':' + String(property) + ':' + JSON.stringify(input),
                  value,
                ).catch(() => {});
                if (name === 'user' && property === 'getQueue' && wire(value).queue)
                  await accountWrite(account, 'queue', wire(value).queue).catch(() => {});
              }
              return result;
            } catch (error) {
              if (!isRead || !localizable(error)) throw error;
              const result = await localRead(account, name, String(property), input);
              if (result !== undefined) return fromJson(method.output, result);
              throw error;
            }
          }
          if (isRead) {
            const result = await localRead(account, name, String(property), input);
            if (result !== undefined) return fromJson(method.output, result);
          }
          throw emptyOffline;
        };
      },
    });
  return {
    ...raw,
    library: wrap(LibraryService, 'library', raw.library),
    user: wrap(UserDataService, 'user', raw.user),
    listening: wrap(ListeningService, 'listening', raw.listening),
    discovery: wrap(DiscoveryService, 'discovery', raw.discovery),
    ingestion: wrap(IngestionService, 'ingestion', raw.ingestion),
    sharing: wrap(SharingService, 'sharing', raw.sharing),
  };
}
