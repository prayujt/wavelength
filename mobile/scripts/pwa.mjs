import { createHash } from 'node:crypto';
import { readFile, writeFile, readdir } from 'node:fs/promises';
import { join } from 'node:path';
import sharp from 'sharp';

const root = process.argv[2] || 'pwa-dist';
for (const size of [192, 512])
  await sharp('assets/icon.png').resize(size, size).png().toFile(`${root}/icon-${size}.png`);
async function walk(root) {
  const result = [];
  for (const entry of await readdir(root, { withFileTypes: true })) {
    const path = join(root, entry.name);
    if (entry.isDirectory()) result.push(...(await walk(path)));
    else if (!path.endsWith('.map') && !path.endsWith('/sw.js')) result.push(path);
  }
  return result;
}
const files = (await walk(root)).sort();
const hash = createHash('sha256');
// A worker-only change must get a fresh disposable cache as well.
hash.update(await readFile(new URL(import.meta.url)));
for (const file of files) hash.update(await readFile(file));
const cache = 'wavelength-app-shell-' + hash.digest('hex').slice(0, 16);
// Go's file server redirects index.html to ./; caching that redirected Response
// makes Chromium reject an offline navigation. Precache the canonical URL.
const assets = files.map((file) =>
  file === join(root, 'index.html') ? '/app/' : '/app/' + file.slice(root.length + 1),
);
const worker = `// Generated from the actual Expo export. Music/account databases are never touched.
const CACHE = ${JSON.stringify(cache)};
const ASSETS = ${JSON.stringify(assets)};
self.addEventListener('install', event => {
  event.waitUntil((async () => {
    try { await (await caches.open(CACHE)).addAll(ASSETS); }
    catch (error) { await caches.delete(CACHE); throw error; }
  })());
});
// No skipWaiting: a new release waits until old windows close. It cannot interrupt music.
self.addEventListener('activate', event => event.waitUntil((async () => {
  const shells = (await caches.keys()).filter(key => key.startsWith('wavelength-app-shell-') && key !== CACHE);
  // Keep the previous shell as well; never enumerate/delete music or account data.
  for (const key of shells.slice(0, -1)) await caches.delete(key);
  await self.clients.claim();
})()));
self.addEventListener('fetch', event => {
  const request = event.request, url = new URL(request.url);
  if (request.method !== 'GET' || url.origin !== self.location.origin || !url.pathname.startsWith('/app/')) return;
  if (request.mode === 'navigate') {
    // Never persist OAuth codes, queries or callback responses. Only the clean shell.
    event.respondWith(caches.open(CACHE).then(cache => cache.match('/app/')).then(response => response || fetch(request)));
    return;
  }
  if (!ASSETS.includes(url.pathname) || url.search) return;
  event.respondWith(caches.open(CACHE).then(cache => cache.match(url.pathname)).then(response => response || fetch(request)));
});
`;
await writeFile(join(root, 'sw.js'), worker);
console.log(`PWA shell: ${assets.length} assets, ${cache}`);
