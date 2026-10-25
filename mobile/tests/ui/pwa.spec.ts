import { test, expect, type BrowserContext, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { createPreviewFetch } from '../../src/preview/store';

test.describe.configure({ mode: 'serial' });
test.beforeEach(({ page }) => {
  page.on('pageerror', (error) => {
    throw error;
  });
});
const origin = 'http://127.0.0.1:4174';
const audio = readFileSync('assets/preview.wav');
const artwork = readFileSync('assets/icon.png');
async function standalone(context: BrowserContext) {
  await context.addInitScript(() => {
    const AudioClass = window.Audio;
    window.Audio = new Proxy(AudioClass, {
      construct(target, args) {
        const audio = new target(...args);
        (window as Window & { pwaAudio?: HTMLAudioElement }).pwaAudio = audio;
        return audio;
      },
    });
    const match = window.matchMedia.bind(window);
    window.matchMedia = (query) =>
      query === '(display-mode: standalone)' ? { ...match(query), matches: true } : match(query);
  });
}
async function service(
  context: BrowserContext,
  options: {
    partial?: boolean;
    revoked?: boolean;
    offline?: boolean;
    user?: string;
    handoff?: boolean;
  } = {},
) {
  const preview = createPreviewFetch(),
    reports: { method: string; body: Record<string, unknown> }[] = [];
  let refreshes = 0,
    callbackURL = '';
  await context.route(origin + '/oauth/**', async (route) => {
    const url = new URL(route.request().url());
    if (options.offline) return route.abort('internetdisconnected');
    if (url.pathname === '/oauth/register')
      return route.fulfill({ json: { client_id: 'pwa-test-client' } });
    if (url.pathname === '/oauth/authorize') {
      const target = new URL(url.searchParams.get('redirect_uri')!);
      target.search = new URLSearchParams({
        code: 'test-code',
        state: url.searchParams.get('state')!,
        iss: origin,
      }).toString();
      callbackURL = target.href;
      if (options.handoff)
        return route.fulfill({ contentType: 'text/html', body: 'Continue in Wavelength.' });
      return route.fulfill({ status: 302, headers: { Location: target.href } });
    }
    if (url.pathname === '/oauth/token') {
      if (route.request().postData()?.includes('refresh_token')) {
        refreshes++;
        if (options.revoked)
          return route.fulfill({ status: 400, json: { error: 'invalid_grant' } });
        await new Promise((resolve) => setTimeout(resolve, 100));
      }
      return route.fulfill({
        json: {
          access_token: 'test-access-' + refreshes,
          refresh_token: 'test-refresh-' + refreshes,
          expires_in: 3600,
          token_type: 'Bearer',
          scope: 'wavelength:read wavelength:write wavelength:stream wavelength:ingest',
        },
      });
    }
    return route.fulfill({ status: 200, json: {} });
  });
  await context.route(origin + '/wavelength.v1.*/**', async (route) => {
    const request = route.request(),
      method = request.url().split('/').pop()!;
    if (method === 'Subscribe')
      return route.fulfill({
        status: 503,
        json: { code: 'unavailable', message: 'Fixture has no stream' },
      });
    const body = request.postDataJSON();
    if (method.includes('Listening')) reports.push({ method, body });
    const response = await preview(
      'https://preview.wavelength.invalid' + new URL(request.url()).pathname,
      {
        method: 'POST',
        headers: request.headers(),
        body: request.postData(),
      },
    );
    const json = await response.json();
    function tracks(value: unknown) {
      if (!value || typeof value !== 'object') return;
      const row = value as Record<string, unknown>;
      if (typeof row.title === 'string' && 'durationSeconds' in row) {
        row.sizeBytes = String(audio.length);
        row.contentType = 'audio/wav';
        row.artworkPath = `/media/tracks/${row.id}/artwork`;
      }
      for (const child of Object.values(row)) tracks(child);
    }
    tracks(json);
    if (method === 'GetCurrentUser' && options.user) json.user.id = options.user;
    return route.fulfill({ status: response.status, json });
  });
  await context.route(origin + '/preview-audio', (route) =>
    route.fulfill({
      status: options.partial ? 206 : 200,
      contentType: 'audio/wav',
      body: audio,
      headers: { 'Content-Length': String(audio.length), ETag: 'fixture-v1' },
    }),
  );
  await context.route(origin + '/media/**/artwork', (route) =>
    route.fulfill({ contentType: 'image/png', body: artwork }),
  );
  return {
    reports,
    refreshes: () => refreshes,
    callback: () => callbackURL,
    offline(value: boolean) {
      options.offline = value;
    },
    user(value: string) {
      options.user = value;
    },
  };
}
async function connect(page: Page) {
  await page.goto('/app/');
  await page.getByRole('button', { name: 'Find your wavelength', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Find your rhythm.' })).toBeVisible();
  await expect
    .poll(() => page.evaluate(async () => !!(await navigator.serviceWorker.ready).active))
    .toBe(true);
  await page.waitForFunction(() => !!navigator.serviceWorker.controller);
}
async function downloadSong(page: Page) {
  await page.getByRole('tab', { name: 'Library', exact: true }).click();
  await page.getByRole('button', { name: 'More options for Weightless', exact: true }).click();
  await page.getByRole('button', { name: 'Download to device', exact: true }).click();
  await expect(page.getByRole('button', { name: 'On this device · 1 songs' })).toBeVisible();
}
async function expire(page: Page) {
  await page.evaluate(async () => {
    const database = await new Promise<IDBDatabase>((resolve) => {
      const request = indexedDB.open('wavelength-device-v1');
      request.onsuccess = () => resolve(request.result);
    });
    await new Promise<void>((resolve) => {
      const transaction = database.transaction('state', 'readwrite'),
        store = transaction.objectStore('state');
      const request = store.get('credential:wavelength.oauth.grant.v1');
      request.onsuccess = () => {
        const grant = JSON.parse(request.result);
        grant.expiresAt = 0;
        store.put(JSON.stringify(grant), 'credential:wavelength.oauth.grant.v1');
      };
      // The first release persisted protobuf runtime metadata in this record.
      // Keep the old shape here to verify that an update reads it without re-login.
      const profile = store.get('active-account');
      profile.onsuccess = () => {
        if (profile.result) {
          profile.result.user.$typeName = 'wavelength.v1.User';
          store.put(profile.result, 'active-account');
        }
      };
      transaction.oncomplete = () => resolve();
    });
    database.close();
  });
}
test('ordinary mobile browser tabs hide device downloads', async ({ page, context }) => {
  await service(context);
  await connect(page);
  await page.getByRole('tab', { name: 'Library', exact: true }).click();
  await expect(page.getByRole('button', { name: /On this device/ })).toHaveCount(0);
  await page.getByRole('button', { name: 'More options for Weightless', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Download to device', exact: true })).toHaveCount(
    0,
  );
});

test('OAuth return in a new app window uses its durable per-state PKCE record', async ({
  page,
  context,
}) => {
  await standalone(context);
  const backend = await service(context, { handoff: true });
  await page.goto('/app/');
  await page.getByRole('button', { name: 'Find your wavelength', exact: true }).click();
  await expect.poll(backend.callback).not.toBe('');
  const returned = await context.newPage();
  await returned.goto(backend.callback());
  await expect(returned.getByRole('heading', { name: 'Find your rhythm.' })).toBeVisible();
  await expect(returned).not.toHaveURL(/code=/);
});
test('complete download cold-starts offline with expired credentials, plays and seeks with artwork and lyrics', async ({
  page,
  context,
}) => {
  await standalone(context);
  const backend = await service(context);
  await connect(page);
  await downloadSong(page);
  backend.offline(true);
  await expire(page);
  await context.setOffline(true);
  await page.close();
  page = await context.newPage();
  await page.goto('/app/settings');
  await expect(page.getByText('Alex Morgan', { exact: true })).toBeVisible();
  await page.goto('/app/on-device');
  await expect(page.getByRole('heading', { name: 'On this device' })).toBeVisible();
  await expect(page.getByText('1 complete songs', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Play downloaded music', exact: true }).click();
  await page.getByRole('button', { name: 'Open now playing' }).click();
  await expect(page.getByRole('button', { name: 'Pause song', exact: true })).toBeVisible();
  await expect
    .poll(() =>
      page.evaluate(
        () => (window as Window & { pwaAudio?: HTMLAudioElement }).pwaAudio?.currentTime || 0,
      ),
    )
    .toBeGreaterThan(0);
  await page.getByRole('button', { name: 'Open lyrics' }).click();
  await page.getByRole('button', { name: 'Seek to lyric: Somewhere we begin again' }).click();
  await expect
    .poll(() =>
      page.evaluate(
        () => (window as Window & { pwaAudio?: HTMLAudioElement }).pwaAudio?.currentTime || 0,
      ),
    )
    .toBeGreaterThan(12);
  expect(backend.refreshes()).toBe(0);
  backend.offline(false);
  await context.setOffline(false);
  await expect
    .poll(() => backend.reports.filter((event) => event.method === 'StartListeningSession').length)
    .toBeGreaterThan(0);
  expect(
    backend.reports.find((event) => event.method === 'StartListeningSession')?.body,
  ).toMatchObject({ countPlay: true, occurredAt: expect.any(String) });
});
test('partial media never becomes a completed download', async ({ page, context }) => {
  await standalone(context);
  await service(context, { partial: true });
  await connect(page);
  await page.getByRole('tab', { name: 'Library', exact: true }).click();
  await page.getByRole('button', { name: 'More options for Weightless', exact: true }).click();
  await page.getByRole('button', { name: 'Download to device', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('complete audio file');
  await expect(page.getByRole('button', { name: 'On this device · 0 songs' })).toBeVisible();
});
test('browser quota errors leave no completed song', async ({ page, context }) => {
  await standalone(context);
  await service(context);
  await connect(page);
  await page.evaluate(() => {
    const put = Cache.prototype.put;
    Cache.prototype.put = function (request, response) {
      if (String(request).includes('/app/_local/'))
        return Promise.reject(new DOMException('Quota exceeded', 'QuotaExceededError'));
      return put.call(this, request, response);
    };
  });
  await page.getByRole('tab', { name: 'Library', exact: true }).click();
  await page.getByRole('button', { name: 'More options for Weightless', exact: true }).click();
  await page.getByRole('button', { name: 'Download to device', exact: true }).click();
  await expect(page.getByRole('button', { name: 'On this device · 0 songs' })).toBeVisible();
  await expect(page.getByRole('alert')).toBeVisible();
});
test('durable refresh is serialized across windows', async ({ page, context }) => {
  await standalone(context);
  const backend = await service(context);
  await connect(page);
  await expire(page);
  const other = await context.newPage();
  await Promise.all([page.reload(), other.goto('/app/library')]);
  await expect(other.getByRole('heading', { name: 'Your library', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Find your rhythm.' })).toBeVisible();
  await expect.poll(backend.refreshes).toBe(1);
});

test('revoked online authorization keeps downloaded music accessible and asks to reconnect', async ({
  page,
  context,
}) => {
  await standalone(context);
  await service(context, { revoked: true });
  await connect(page);
  await downloadSong(page);
  await expire(page);
  await page.close();
  page = await context.newPage();
  await page.goto('/app/on-device');
  await expect(
    page.getByRole('button', { name: 'Reconnect your account', exact: true }),
  ).toBeVisible();
  await expect(page.getByText('1 complete songs', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Play downloaded music', exact: true }).click();
  await page.getByRole('button', { name: 'Open now playing' }).click();
  await expect(page.getByRole('button', { name: 'Pause song', exact: true })).toBeVisible();
});

test('album downloads save every song and desktop installed mode exposes the same controls', async ({
  page,
  context,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await standalone(context);
  await service(context);
  await connect(page);
  await page.getByRole('button', { name: 'Explore The quiet between', exact: true }).click();
  await page.getByRole('button', { name: 'Download collection to device', exact: true }).click();
  await expect(
    page.getByRole('button', { name: 'Saved on this device', exact: true }),
  ).toBeVisible();
  await page.goto('/app/on-device');
  await expect(page.getByText('5 complete songs', { exact: false })).toBeVisible();
});

test('a new worker waits during playback and preserves credentials, queue and downloads on restart', async ({
  page,
  context,
}) => {
  await standalone(context);
  await service(context);
  await connect(page);
  await downloadSong(page);
  await page.getByRole('button', { name: 'Play Weightless', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Open now playing' })).toBeVisible();
  await expect
    .poll(() =>
      page.evaluate(
        () => (window as Window & { pwaAudio?: HTMLAudioElement }).pwaAudio?.currentTime || 0,
      ),
    )
    .toBeGreaterThan(1);
  await page.request.post('/__test/update');
  await page.evaluate(async () => {
    await (await navigator.serviceWorker.getRegistration('/app/'))?.update();
  });
  await expect(page.getByText('An update is ready.', { exact: false })).toBeVisible();
  expect(
    await page.evaluate(
      () => !(window as Window & { pwaAudio?: HTMLAudioElement }).pwaAudio?.paused,
    ),
  ).toBe(true);
  await page.close();
  page = await context.newPage();
  await page.goto('/app/on-device');
  await expect(page.getByText('1 complete songs', { exact: false })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Open now playing' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Find your wavelength', exact: true })).toHaveCount(
    0,
  );
});

test('switching accounts isolates downloads; explicit offline sign-out clears local credentials and data', async ({
  page,
  context,
}) => {
  await standalone(context);
  const backend = await service(context);
  await connect(page);
  await downloadSong(page);
  backend.user('different-listener');
  await page.goto('/app/settings');
  await page.getByRole('button', { name: 'Reconnect account', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Find your rhythm.' })).toBeVisible();
  await page.getByRole('tab', { name: 'Library', exact: true }).click();
  await expect(page.getByRole('button', { name: 'On this device · 0 songs' })).toBeVisible();
  backend.user('preview-user');
  await page.goto('/app/settings');
  await page.getByRole('button', { name: 'Reconnect account', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Find your rhythm.' })).toBeVisible();
  // The original profile's ID comes from the fixture, rather than an email or token.
  await page.goto('/app/on-device');
  await expect(page.getByText('1 complete songs', { exact: false })).toBeVisible();
  backend.offline(true);
  await context.setOffline(true);
  await page.goto('/app/settings');
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(
    page.getByRole('button', { name: 'Find your wavelength', exact: true }),
  ).toBeVisible();
  await page.reload();
  await expect(
    page.getByRole('button', { name: 'Find your wavelength', exact: true }),
  ).toBeVisible();
});
