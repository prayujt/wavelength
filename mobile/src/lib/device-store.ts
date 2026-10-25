// Browser persistence is deliberately separate from disposable service-worker assets.
// Only delegated Wavelength credentials are stored here; never provider credentials.
export type LocalUser = { id: string; name: string; email: string };
export type LocalAccount = { key: string; server: string; user: LocalUser };
const DB = 'wavelength-device-v1';
export const MUSIC_CACHE = 'wavelength-device-music-v1';
let database: Promise<IDBDatabase> | undefined;
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
  if (!database)
    database = new Promise((resolve, reject) => {
      const request = indexedDB.open(DB, 1);
      request.onupgradeneeded = () => request.result.createObjectStore('state');
      request.onsuccess = () => {
        request.result.onversionchange = () => request.result.close();
        resolve(request.result);
      };
      request.onerror = () => {
        database = undefined;
        reject(request.error);
      };
    });
  return database;
}
export async function read<T>(key: string): Promise<T | undefined> {
  const database = await db();
  return new Promise((resolve, reject) => {
    const request = database.transaction('state').objectStore('state').get(key);
    request.onsuccess = () => resolve(request.result as T | undefined);
    request.onerror = () => reject(request.error);
  });
}
export async function write(key: string, value: unknown) {
  const database = await db();
  await new Promise<void>((resolve, reject) => {
    const transaction = database.transaction('state', 'readwrite');
    transaction.objectStore('state').put(value, key);
    transaction.oncomplete = () => resolve();
    transaction.onabort = transaction.onerror = () => reject(transaction.error);
  });
}
export async function erase(key: string) {
  const database = await db();
  await new Promise<void>((resolve, reject) => {
    const transaction = database.transaction('state', 'readwrite');
    transaction.objectStore('state').delete(key);
    transaction.oncomplete = () => resolve();
    transaction.onabort = transaction.onerror = () => reject(transaction.error);
  });
}
export async function entries<T>(prefix: string): Promise<[string, T][]> {
  const database = await db();
  return new Promise((resolve, reject) => {
    const result: [string, T][] = [];
    const request = database
      .transaction('state')
      .objectStore('state')
      .openCursor(IDBKeyRange.bound(prefix, prefix + '\uffff'));
    request.onsuccess = () => {
      const cursor = request.result;
      if (!cursor) {
        resolve(result);
        return;
      }
      result.push([String(cursor.key), cursor.value as T]);
      cursor.continue();
    };
    request.onerror = () => reject(request.error);
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
export async function exclusive<T>(name: string, run: () => Promise<T>): Promise<T> {
  if (navigator.locks) return navigator.locks.request('wavelength:' + name, run);
  const owner = crypto.randomUUID(),
    key = 'lock:' + name;
  const acquire = async () => {
    const database = await db();
    return new Promise<boolean>((resolve, reject) => {
      const transaction = database.transaction('state', 'readwrite'),
        store = transaction.objectStore('state');
      let acquired = false;
      const request = store.get(key);
      request.onsuccess = () => {
        const lock = request.result as { owner: string; expires: number } | undefined;
        if (!lock || lock.expires < Date.now() || lock.owner === owner) {
          store.put({ owner, expires: Date.now() + 60000 }, key);
          acquired = true;
        }
      };
      transaction.oncomplete = () => resolve(acquired);
      transaction.onabort = transaction.onerror = () => reject(transaction.error);
    });
  };
  const deadline = Date.now() + 65000;
  while (!(await acquire())) {
    if (Date.now() > deadline) throw new Error('Another window is connecting. Try again.');
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  const renew = setInterval(() => {
    void acquire();
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
