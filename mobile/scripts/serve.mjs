import { createServer } from 'node:http';
import { readFile, stat } from 'node:fs/promises';
import { resolve, extname, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
const project = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const root = resolve(project, 'dist'),
  relay = resolve(project, '..', 'web', 'public');
const mime = {
  '.html': 'text/html',
  '.js': 'application/javascript',
  '.css': 'text/css',
  '.json': 'application/json',
  '.png': 'image/png',
  '.svg': 'image/svg+xml',
  '.wav': 'audio/wav',
  '.ttf': 'font/ttf',
  '.woff2': 'font/woff2',
  '.ico': 'image/x-icon',
  '.webmanifest': 'application/manifest+json',
};
let workerRevision = 0;
const server = createServer(async (request, response) => {
  try {
    const path = decodeURIComponent(new URL(request.url, 'http://127.0.0.1').pathname);
    if (path === '/__test/update' && request.method === 'POST') {
      workerRevision++;
      response.writeHead(204).end();
      return;
    }
    const isRelay = ['/mobile-auth.html', '/mobile-auth.js'].includes(path);
    const isApp = path.startsWith('/app/');
    const base = isRelay ? relay : isApp ? resolve(project, 'pwa-dist') : root;
    let file = resolve(base, '.' + (isApp ? path.slice(4) : path));
    if (!file.startsWith(base + '/') && file !== base) {
      response.writeHead(403).end();
      return;
    }
    try {
      if (!(await stat(file)).isFile()) throw new Error();
    } catch {
      if (!isRelay && !extname(path)) file = resolve(base, 'index.html');
      else {
        response.writeHead(404).end();
        return;
      }
    }
    let data = await readFile(file);
    if (path === '/app/sw.js' && workerRevision)
      data = Buffer.from(
        data
          .toString()
          .replace(
            /wavelength-app-shell-([a-f0-9]+)/,
            `wavelength-app-shell-$1-test-${workerRevision}`,
          ),
      );
    const headers = {
      'Content-Type': mime[extname(file)] || 'application/octet-stream',
      'Cache-Control': 'no-store',
      'Referrer-Policy': 'no-referrer',
      'Accept-Ranges': 'bytes',
      ...(isApp
        ? {
            'Content-Security-Policy':
              "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' blob: data: https://*.mzstatic.com; font-src 'self' data:; media-src 'self' blob:; connect-src 'self'; worker-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'",
          }
        : {}),
    };
    if (request.method === 'HEAD') {
      response.writeHead(200, { ...headers, 'Content-Length': data.length }).end();
      return;
    }
    const range = request.headers.range?.match(/^bytes=(\d+)-(\d*)$/);
    if (range) {
      const start = Number(range[1]),
        end = Math.min(data.length - 1, range[2] ? Number(range[2]) : data.length - 1);
      if (start > end) {
        response.writeHead(416).end();
        return;
      }
      response
        .writeHead(206, {
          ...headers,
          'Content-Range': `bytes ${start}-${end}/${data.length}`,
          'Content-Length': end - start + 1,
        })
        .end(data.subarray(start, end + 1));
    } else response.writeHead(200, headers).end(data);
  } catch {
    response.writeHead(500).end('Preview could not serve this file.');
  }
});
server.listen(Number(process.env.MOBILE_PORT || 4174), '127.0.0.1', () =>
  console.log('Mobile review server: http://127.0.0.1:' + (process.env.MOBILE_PORT || 4174)),
);
for (const signal of ['SIGINT', 'SIGTERM'])
  process.on(signal, () => server.close(() => process.exit(0)));
