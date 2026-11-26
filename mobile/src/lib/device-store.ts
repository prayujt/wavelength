// Browser persistence is deliberately separate from disposable service-worker assets.
// Only delegated Wavelength credentials are stored here; never provider credentials.
export type LocalUser = { id: string; name: string; email: string };
export type LocalAccount = { key: string; server: string; user: LocalUser };
const DB = 'wavelength-device-v1';
const STORAGE_TIMEOUT_MS = 10000;
export const MUSIC_CACHE = 'wavelength-device-music-v1';
let database: Promise<IDBDatabase> | undefined;
export class DeviceStorageTimeout extends Error {
  constructor(readonly mayHaveCommitted = false) {
    super('Device storage took too long.');
  }
}
export const browserStorage = () => typeof indexedDB !== 'undefined';
export function installed() {
  return (
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    (window.matchMedia('(display-mode: standalone)').matches ||
      (navigator as Navigator & { standalone?: boolean }).standalone === true)
  );
}
export const online = () => typeof navigator === 'undefined' || navigator.onLine !== false;
function db() {
  if (database) return database;
  const forget = () => {
    if (database === connection) database = undefined;
  };
  const connection = new Promise<IDBDatabase>((resolve, reject) => {
    const request = indexedDB.open(DB, 1);
    let expired = false;
    const timeout = setTimeout(() => {
      expired = true;
      reject(new DeviceStorageTimeout());
    }, STORAGE_TIMEOUT_MS);
    request.onupgradeneeded = () => request.result.createObjectStore('state');
    request.onsuccess = () => {
      clearTimeout(timeout);
      const opened = request.result;
      if (expired) {
        opened.close();
        return;
      }
      opened.onclose = forget;
      opened.onversionchange = () => {
        opened.close();
        forget();
      };
      resolve(opened);
    };
    request.onerror = () => {
      clearTimeout(timeout);
      reject(request.error);
    };
  });
  database = connection;
  void connection.catch(forget);
  return connection;
}
export async function closeDatabase() {
  const connection = database;
  database = undefined;
  await connection?.then(
    (opened) => opened.close(),
    () => {},
  );
}
export async function read<T>(key: string): Promise<T | undefined> {
  return transaction('readonly', (store, result) => {
    const request = store.get(key);
    request.onsuccess = () => result(request.result as T | undefined);
  });
}
async function transaction<T>(
  mode: IDBTransactionMode,
  run: (store: IDBObjectStore, result: (value: T) => void) => void,
) {
  const database = await db();
  return new Promise<T>((resolve, reject) => {
    const tx = database.transaction('state', mode);
    let value: T;
    const timeout = setTimeout(() => {
      let mayHaveCommitted = false;
      try {
        tx.abort();
      } catch {
        // The browser may have finished without delivering a completion event.
        mayHaveCommitted = mode === 'readwrite';
      }
      reject(new DeviceStorageTimeout(mayHaveCommitted));
    }, STORAGE_TIMEOUT_MS);
    tx.oncomplete = () => {
      clearTimeout(timeout);
      resolve(value);
    };
    tx.onabort = () => {
      clearTimeout(timeout);
      reject(tx.error ?? new Error('Wavelength couldn’t access device storage.'));
    };
    try {
      run(tx.objectStore('state'), (result) => {
        value = result;
      });
    } catch (error) {
      clearTimeout(timeout);
      tx.abort();
      reject(error);
    }
  });
}
export async function write(key: string, value: unknown, removeKey?: string) {
  await transaction<void>('readwrite', (store) => {
    store.put(value, key);
    if (removeKey !== undefined) store.delete(removeKey);
  });
}
export async function erase(key: string) {
  await transaction<void>('readwrite', (store) => store.delete(key));
}
export async function entries<T>(prefix: string): Promise<[string, T][]> {
  return transaction('readonly', (store, finish) => {
    const result: [string, T][] = [];
    const request = store.openCursor(IDBKeyRange.bound(prefix, prefix + '\uffff'));
    request.onsuccess = () => {
      const cursor = request.result;
      if (!cursor) {
        finish(result);
        return;
      }
      result.push([String(cursor.key), cursor.value as T]);
      cursor.continue();
    };
  });
}
export function changed() {
  if (typeof window !== 'undefined') window.dispatchEvent(new Event('wavelength-device'));
}
export function announce() {
  changed();
  if (typeof BroadcastChannel !== 'undefined') {
    const channel = new BroadcastChannel('wavelength-device');
    channel.postMessage('changed');
    channel.close();
  }
}
export const accountKey = (server: string, user: string) => JSON.stringify([server, user]);
export const partition = (account: LocalAccount) => 'account:' + account.key + ':';
export async function remember(server: string, user: LocalUser) {
  const account = {
    key: accountKey(server, user.id),
    server,
    user: { id: user.id, name: user.name, email: user.email },
  };
  await write('active-account', account);
  announce();
  return account;
}
export async function currentAccount() {
  return browserStorage() ? read<LocalAccount>('active-account') : undefined;
}
export async function assertAccount(account: LocalAccount) {
  if ((await currentAccount())?.key !== account.key) throw new Error('The active account changed.');
}
export async function accountWrite(account: LocalAccount, key: string, value: unknown) {
  await exclusive('data:' + account.key, async () => {
    await assertAccount(account);
    await write(partition(account) + key, value);
  });
}
// Web Locks serialize refresh across tabs. The IndexedDB lease is for older browsers.
// Refresh network calls are bounded to 20s; a 60s lease is renewed while held.
const LOCK_WAIT_MS = 65000;
const lockWaitError = () => new Error('Another window is connecting. Try again.');

export async function exclusive<T>(name: string, run: () => Promise<T>): Promise<T> {
  if (navigator.locks) {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), LOCK_WAIT_MS);
    try {
      return await navigator.locks.request(
        'wavelength:' + name,
        { signal: controller.signal },
        () => {
          clearTimeout(timeout);
          if (controller.signal.aborted) throw lockWaitError();
          return run();
        },
      );
    } catch (error) {
      if (controller.signal.aborted) throw lockWaitError();
      throw error;
    } finally {
      clearTimeout(timeout);
    }
  }
  const owner = crypto.randomUUID(),
    key = 'lock:' + name;
  const acquire = async () => {
    let acquired = false;
    await transaction<void>('readwrite', (store) => {
      const request = store.get(key);
      request.onsuccess = () => {
        const lock = request.result as { owner: string; expires: number } | undefined;
        if (!lock || lock.expires < Date.now() || lock.owner === owner) {
          store.put({ owner, expires: Date.now() + 60000 }, key);
          acquired = true;
        }
      };
    });
    return acquired;
  };
  const deadline = Date.now() + LOCK_WAIT_MS;
  while (!(await acquire())) {
    if (Date.now() > deadline) throw lockWaitError();
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  const renew = setInterval(() => {
    void acquire().catch(() => {});
  }, 15000);
  try {
    return await run();
  } finally {
    clearInterval(renew);
    if ((await read<{ owner: string }>(key))?.owner === owner) await erase(key);
  }
}
export function resourceKey(account: LocalAccount, id: string, version: string, kind = 'audio') {
  return new URL(
    '/app/_local/' +
      encodeURIComponent(account.key) +
      '/' +
      encodeURIComponent(id) +
      '/' +
      encodeURIComponent(version) +
      '/' +
      kind,
    location.origin,
  ).href;
}
export async function clearAccount(account: LocalAccount) {
  const cache = await caches.open(MUSIC_CACHE);
  const prefix = new URL('/app/_local/' + encodeURIComponent(account.key) + '/', location.origin)
    .href;
  for (const request of await cache.keys())
    if (request.url.startsWith(prefix)) await cache.delete(request);
  for (const [key] of await entries(partition(account))) await erase(key);
  if ((await currentAccount())?.key === account.key) await erase('active-account');
  announce();
}
