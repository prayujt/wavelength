import { test, expect, chromium, type BrowserContext, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
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
async function phoneInsets(context: BrowserContext) {
  await context.addInitScript(() => {
    const computed = window.getComputedStyle.bind(window);
    window.getComputedStyle = (element, pseudo) => {
      const style = computed(element, pseudo);
      if ((element as HTMLElement).style?.paddingBottom.includes('safe-area-inset-bottom'))
        return new Proxy(style, {
          get: (target, key) =>
            key === 'paddingBottom'
              ? '34px'
              : key === 'paddingTop'
                ? '59px'
                : Reflect.get(target, key, target),
        });
      return style;
    };
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
    largePlaylist?: boolean;
    slowRemainder?: boolean;
  } = {},
) {
  const preview = createPreviewFetch(),
    reports: { method: string; body: Record<string, unknown> }[] = [];
  if (options.largePlaylist)
    await preview(
      'https://preview.wavelength.invalid/wavelength.v1.UserDataService/UpdatePlaylist',
      {
        method: 'POST',
        body: JSON.stringify({
          id: 'playlist-1',
          replaceTracks: {
            ids: Array.from({ length: 40 }, (_, n) => `track-${Math.floor(n / 5)}-${n % 5}`),
          },
        }),
      },
    );
  let refreshes = 0,
    callbackURL = '';
  const artworkRequests: string[] = [];
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
      // Playwright's WebKit route API cannot fulfill a redirect status. The
      // fixture authorization page navigates to the same callback instead.
      if (context.browser()?.browserType().name() === 'webkit')
        return route.fulfill({
          contentType: 'text/html',
          body: `<script>location.replace(${JSON.stringify(target.href)})</script>`,
        });
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
    if (
      options.slowRemainder &&
      method === 'GetTrack' &&
      !String(body.id).startsWith('track-0-') &&
      !String(body.id).startsWith('track-1-')
    )
      await new Promise((resolve) => setTimeout(resolve, 1800));
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
    if (method === 'CreatePlaybackLink') json.url = origin + '/preview-audio';
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
  await context.route(origin + '/media/**/artwork', (route) => {
    artworkRequests.push(route.request().url());
    return route.fulfill({ contentType: 'image/png', body: artwork });
  });
  return {
    reports,
    artworkRequests,
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
  if (page.context().browser()?.browserType().name() !== 'webkit') {
    await expect
      .poll(() => page.evaluate(async () => (await navigator.serviceWorker.ready).active?.state))
      .toBe('activated');
    await page.waitForFunction(() => !!navigator.serviceWorker.controller);
  }
  await page.getByRole('button', { name: 'Find your wavelength', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Find your rhythm.' })).toBeVisible();
  // Home can render optimistically after the code exchange. Expiry/race tests
  // must start after identity binding and the local profile are durable, so a
  // reload does not cancel the initial login's writes or refresh requests.
  await expect
    .poll(() =>
      page.evaluate(async () => {
        const database = await new Promise<IDBDatabase>((resolve) => {
          const open = indexedDB.open('wavelength-device-v1');
          open.onsuccess = () => resolve(open.result);
        });
        const read = <T>(key: string) =>
          new Promise<T | undefined>((resolve) => {
            const request = database.transaction('state').objectStore('state').get(key);
            request.onsuccess = () => resolve(request.result);
          });
        const [grant, account] = await Promise.all([
          read<string>('credential:wavelength.oauth.grant.v1'),
          read<{ user: { id: string } }>('active-account'),
        ]);
        database.close();
        return !!account && JSON.parse(grant || '{}').accountId === account.user.id;
      }),
    )
    .toBe(true);
}
async function downloadSong(page: Page) {
  await page.getByRole('tab', { name: 'Library', exact: true }).click();
  await page.getByRole('button', { name: 'Browse all music', exact: true }).click();
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
  await page.getByRole('button', { name: 'Browse all music', exact: true }).click();
  await page.getByRole('button', { name: 'More options for Weightless', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Download to device', exact: true })).toHaveCount(
    0,
  );
});

test.describe('Mobile polish', () => {
  test('tab labels stay readable inside the visible iPhone viewport', async ({ page, context }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await standalone(context);
    await phoneInsets(context);
    // Reproduce Safari reporting a visible window shorter than its CSS large viewport.
    await context.addInitScript(() =>
      Object.defineProperty(window, 'innerHeight', { configurable: true, get: () => 780 }),
    );
    await service(context);
    await connect(page);
    const bar = await page.getByTestId('bottom-navigation').boundingBox();
    expect(Math.round(bar!.y + bar!.height)).toBe(780);
    for (const name of ['Home', 'Playlists', 'Library', 'Discover']) {
      const label = page.getByRole('tab', { name, exact: true }).getByText(name, { exact: true });
      const rect = await label.boundingBox();
      expect(rect!.height).toBeGreaterThanOrEqual(18);
      expect(rect!.y + rect!.height).toBeLessThanOrEqual(bar!.y + bar!.height - 34);
      expect(await label.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
    }
  });

  test('update notices and feedback stay below the Dynamic Island without overlapping', async ({
    page,
    context,
  }) => {
    await standalone(context);
    await phoneInsets(context);
    await context.addInitScript(() => {
      const registration = {
        waiting: {},
        active: { state: 'activated' },
        addEventListener() {},
        async update() {},
      };
      Object.defineProperty(navigator, 'serviceWorker', {
        configurable: true,
        value: {
          controller: {},
          ready: Promise.resolve(registration),
          async register() {
            return registration;
          },
          async getRegistration() {
            return registration;
          },
        },
      });
    });
    await service(context);
    await connect(page);
    const notice = page.getByTestId('app-update-notice');
    await expect(notice.getByText('An update is ready.', { exact: true })).toBeVisible();
    expect((await notice.boundingBox())!.y).toBeGreaterThanOrEqual(59);
    await page.getByRole('tab', { name: 'Playlists', exact: true }).click();
    await page.getByRole('button', { name: 'Create playlist', exact: true }).click();
    await page.getByRole('textbox', { name: 'Playlist name' }).fill('Safe area check');
    await page.getByRole('button', { name: 'Create playlist', exact: true }).click();
    const toast = page.getByText('Your playlist is ready', { exact: true });
    await expect(toast).toBeVisible();
    await expect
      .poll(async () => {
        const update = await notice.boundingBox(),
          feedback = await toast.boundingBox();
        return !!update && !!feedback && feedback.y >= update.y + update.height;
      })
      .toBe(true);
  });

  test('page ends reserve only the space used by the visible player', async ({ page, context }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await standalone(context);
    await phoneInsets(context);
    await service(context);
    await connect(page);
    await page.getByRole('tab', { name: 'Playlists', exact: true }).click();
    const shortList = page.getByTestId('playlists-scroll');
    await shortList.evaluate((el) => {
      el.scrollTop = 100000;
    });
    expect(await shortList.evaluate((el) => el.scrollTop)).toBe(0);
    await page.getByRole('tab', { name: 'Library', exact: true }).click();
    await page.getByRole('button', { name: 'Browse all music', exact: true }).click();
    const list = page.getByTestId('catalog-scroll');
    await expect(page.getByText('40 songs', { exact: true })).toBeVisible();
    const last = list.getByRole('button', { name: /^More options for / }).last();
    await expect
      .poll(async () => {
        await list.evaluate((el) => {
          el.scrollTop = el.scrollHeight;
        });
        const frame = (await list.boundingBox())!,
          row = (await last.boundingBox())!;
        return Math.abs(frame.y + frame.height - (row.y + row.height));
      })
      .toBeLessThanOrEqual(40);
    await list.evaluate((el) => {
      el.scrollTop = 0;
    });
    await page.getByRole('button', { name: 'Play Weightless', exact: true }).click();
    const dock = page.getByRole('button', { name: 'Open now playing', exact: true });
    await expect(dock).toBeVisible();
    await expect
      .poll(async () => {
        await list.evaluate((el) => {
          el.scrollTop = el.scrollHeight;
        });
        const player = (await dock.boundingBox())!,
          row = (await last.boundingBox())!;
        return Math.abs(player.y - (row.y + row.height));
      })
      .toBeLessThanOrEqual(52);
  });

  test('Home opens the play feed instead of an inserted collection hero', async ({
    page,
    context,
  }) => {
    await standalone(context);
    await service(context);
    await connect(page);
    await expect(page.getByText('FROM YOUR COLLECTION', { exact: true })).toHaveCount(0);
    await page.getByRole('button', { name: 'Open play feed', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Your play feed', exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Choose a playlist', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Playlists', exact: true })).toBeVisible();
  });
  test('compact tabs fill the viewport and keep home-indicator padding inside the bar', async ({
    page,
    context,
  }, info) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await standalone(context);
    // Model a notched iPhone's safe-area values; desktop WebKit has no OS notch.
    await context.addInitScript(() => {
      const computed = window.getComputedStyle.bind(window);
      window.getComputedStyle = (element, pseudo) => {
        const style = computed(element, pseudo);
        if ((element as HTMLElement).style?.paddingBottom.includes('safe-area-inset-bottom'))
          return new Proxy(style, {
            get: (target, key) =>
              key === 'paddingBottom'
                ? '34px'
                : key === 'paddingTop'
                  ? '59px'
                  : Reflect.get(target, key, target),
          });
        return style;
      };
    });
    await service(context);
    await connect(page);
    await page.getByRole('tab', { name: 'Library', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Browse all music', exact: true })).toBeVisible();
    await expect(page.getByRole('textbox', { name: 'Search your songs' })).toHaveCount(0);
    await expect(page.getByRole('tab')).toHaveCount(4);
    const background = page.getByRole('tablist').locator('..');
    const bar = await background.boundingBox();
    expect(bar).not.toBeNull();
    expect(Math.abs(bar!.y + bar!.height - 844)).toBeLessThanOrEqual(1);
    expect(bar!.height).toBeLessThanOrEqual(94);
    expect(await background.evaluate((el) => getComputedStyle(el).paddingBottom)).toBe('34px');
    await page.waitForTimeout(350);
    await page.screenshot({ path: info.outputPath('library.png') });
    await page.setViewportSize({ width: 844, height: 390 });
    await expect
      .poll(async () => {
        const rect = await background.boundingBox();
        return Math.round(rect!.y + rect!.height);
      })
      .toBe(390);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
  });

  test('stream starts directly and artwork stays stable across playback ticks', async ({
    page,
    context,
  }) => {
    await standalone(context);
    const backend = await service(context);
    await connect(page);
    await page.getByRole('button', { name: 'Open album The quiet between', exact: true }).click();
    const start = Date.now();
    await page.getByRole('button', { name: 'Play', exact: true }).click();
    await expect
      .poll(
        () =>
          page.evaluate(
            () => (window as Window & { pwaAudio?: HTMLAudioElement }).pwaAudio?.currentTime || 0,
          ),
        { timeout: 2500 },
      )
      .toBeGreaterThan(0.1);
    expect(Date.now() - start).toBeLessThan(2500);
    expect(
      await page.evaluate(() => (window as Window & { pwaAudio?: HTMLAudioElement }).pwaAudio?.src),
    ).toBe(origin + '/preview-audio');
    await page.getByRole('button', { name: 'Open now playing' }).click();
    const image = page
      .getByLabel('The quiet between artwork', { exact: true })
      .filter({ visible: true })
      .last()
      .locator('img');
    await expect(image).toHaveAttribute('src', /^blob:/);
    const source = await image.getAttribute('src'),
      requests = backend.artworkRequests.length;
    await expect
      .poll(() =>
        page.evaluate(
          () => (window as Window & { pwaAudio?: HTMLAudioElement }).pwaAudio?.currentTime || 0,
        ),
      )
      .toBeGreaterThan(3);
    expect(await image.getAttribute('src')).toBe(source);
    expect(backend.artworkRequests.length).toBe(requests);
  });

  test('lock-screen metadata includes readable artwork and follows the playing song', async ({
    page,
    context,
  }) => {
    await standalone(context);
    await service(context);
    await connect(page);
    await page.getByRole('button', { name: 'Open album The quiet between', exact: true }).click();
    await page.getByRole('button', { name: 'Play', exact: true }).click();
    await expect
      .poll(() => page.evaluate(() => navigator.mediaSession.metadata?.artwork.length))
      .toBe(1);
    const first = await page.evaluate(async () => {
      const metadata = navigator.mediaSession.metadata!;
      const image = new Image();
      const loaded = new Promise<void>((resolve, reject) => {
        image.onload = () => resolve();
        image.onerror = () => reject(new Error('Lock-screen artwork is not readable'));
      });
      image.src = metadata.artwork[0].src;
      await loaded;
      return {
        title: metadata.title,
        artist: metadata.artist,
        album: metadata.album,
        width: image.naturalWidth,
        src: metadata.artwork[0].src,
      };
    });
    expect(first).toMatchObject({
      title: 'Weightless',
      artist: 'Rowan Hale',
      album: 'The quiet between',
    });
    expect(first.width).toBeGreaterThan(0);
    await page.getByRole('button', { name: 'Next song', exact: true }).click();
    await expect
      .poll(() => page.evaluate(() => navigator.mediaSession.metadata?.title))
      .toBe('A little further');
    await expect
      .poll(() => page.evaluate(() => navigator.mediaSession.metadata?.artwork.length))
      .toBe(1);
    expect(await page.evaluate(() => navigator.mediaSession.metadata?.artwork[0].src)).not.toBe(
      first.src,
    );
  });

  test('large playlists start before remaining metadata arrives and keep the full queue', async ({
    page,
    context,
  }) => {
    await standalone(context);
    await service(context, { largePlaylist: true, slowRemainder: true });
    await connect(page);
    await page.getByRole('tab', { name: 'Playlists', exact: true }).click();
    await page.getByRole('button', { name: 'Open playlist The slow hours', exact: true }).click();
    await expect(page.getByText('Loading the rest of your playlist…')).toBeVisible();
    await page.getByRole('button', { name: 'Play', exact: true }).click();
    await expect
      .poll(
        () =>
          page.evaluate(
            () => (window as Window & { pwaAudio?: HTMLAudioElement }).pwaAudio?.currentTime || 0,
          ),
        { timeout: 2500 },
      )
      .toBeGreaterThan(0.1);
    await page.getByRole('button', { name: 'Open now playing' }).click();
    await page.getByRole('button', { name: 'Open play queue' }).click();
    await expect(page.getByText('40 songs, in your order.')).toBeVisible();
  });

  test('editing a long playlist preserves scroll while moving and removing songs', async ({
    page,
    context,
  }) => {
    await standalone(context);
    await service(context, { largePlaylist: true });
    await connect(page);
    await page.getByRole('tab', { name: 'Playlists', exact: true }).click();
    await page.getByRole('button', { name: 'Open playlist The slow hours', exact: true }).click();
    await page.getByRole('button', { name: 'Edit playlist', exact: true }).click();
    const move = page.getByRole('button', { name: 'Move song 20 down', exact: true });
    await move.scrollIntoViewIfNeeded();
    const before = await move.boundingBox();
    await move.click();
    await expect(
      page.getByRole('button', { name: 'Move song 20 down', exact: true }),
    ).toBeVisible();
    const after = await page
      .getByRole('button', { name: 'Move song 20 down', exact: true })
      .boundingBox();
    expect(Math.abs(after!.y - before!.y)).toBeLessThan(4);
    await page.getByRole('button', { name: 'Remove song 20', exact: true }).click();
    await expect(
      page.getByRole('button', { name: 'Move song 20 down', exact: true }),
    ).toBeVisible();
    const removed = await page
      .getByRole('button', { name: 'Move song 20 down', exact: true })
      .boundingBox();
    expect(Math.abs(removed!.y - before!.y)).toBeLessThan(4);
    await page.getByRole('button', { name: 'Save changes', exact: true }).click();
    await expect(page.getByText('39 songs ·', { exact: false }).last()).toBeVisible();
  });

  test('horizontal swipes change tabs while vertical scrolling and edge gestures stay put', async ({
    page,
    context,
  }) => {
    await standalone(context);
    await service(context);
    await connect(page);
    const surface = page.getByTestId('tab-swipe-surface');
    async function swipe(x: number, y: number, endX: number, endY: number) {
      await surface.dispatchEvent('touchstart', {
        touches: [{ identifier: 0, pageX: x, pageY: y, clientX: x, clientY: y }],
        changedTouches: [{ identifier: 0, pageX: x, pageY: y, clientX: x, clientY: y }],
      });
      await surface.dispatchEvent('touchend', {
        touches: [],
        changedTouches: [{ identifier: 0, pageX: endX, pageY: endY, clientX: endX, clientY: endY }],
      });
    }
    await swipe(300, 180, 100, 180);
    await expect(page.getByRole('heading', { name: 'Playlists', exact: true })).toBeVisible();
    await swipe(200, 400, 205, 180);
    await expect(page.getByRole('heading', { name: 'Playlists', exact: true })).toBeVisible();
    await swipe(10, 180, 200, 180);
    await expect(page.getByRole('heading', { name: 'Playlists', exact: true })).toBeVisible();
    await swipe(300, 180, 100, 180);
    await expect(page.getByRole('heading', { name: 'Your library', exact: true })).toBeVisible();
  });
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
  await page.getByRole('button', { name: 'Browse all music', exact: true }).click();
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
  await page.getByRole('button', { name: 'Browse all music', exact: true }).click();
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
  await page.getByRole('button', { name: 'Open album The quiet between', exact: true }).click();
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

test('a legacy redirected shell survives an update and full browser shutdown offline', async ({
  request,
}) => {
  const profile = await mkdtemp(join(tmpdir(), 'wavelength-pwa-upgrade-'));
  let browser: BrowserContext | undefined;
  try {
    await request.post(origin + '/__test/legacy');
    browser = await chromium.launchPersistentContext(profile, { headless: true });
    let page = await browser.newPage();
    await page.goto(origin + '/app/');
    await page.waitForFunction(() => !!navigator.serviceWorker.controller);
    const legacyKey = await page.evaluate(async () => {
      const key = (await caches.keys()).find((key) => key.startsWith('wavelength-app-shell-'))!;
      if (!(await (await caches.open(key)).match('/app/index.html'))?.redirected)
        throw new Error('The fixture must reproduce the redirected legacy response');
      await (
        await caches.open('wavelength-device-music-v1')
      ).put('/app/_local/upgrade-probe', new Response('saved music'));
      const database = await new Promise<IDBDatabase>((resolve) => {
        const open = indexedDB.open('wavelength-device-v1');
        open.onsuccess = () => resolve(open.result);
      });
      await new Promise<void>((resolve) => {
        const transaction = database.transaction('state', 'readwrite');
        transaction.objectStore('state').put('saved session', 'upgrade-probe');
        transaction.oncomplete = () => resolve();
      });
      database.close();
      return key;
    });
    await request.post(origin + '/__test/update');
    await page.evaluate(async () => {
      await (await navigator.serviceWorker.getRegistration('/app/'))!.update();
    });
    await expect
      .poll(() =>
        page.evaluate(
          async () => (await navigator.serviceWorker.getRegistration())?.waiting?.state,
        ),
      )
      .toBe('installed');
    expect(
      await page.evaluate(async (key) => {
        return (await (await caches.open(key)).match('/app/index.html'))?.redirected;
      }, legacyKey),
    ).toBe(false);
    await browser.close();
    browser = await chromium.launchPersistentContext(profile, { headless: true });
    await browser.setOffline(true);
    page = await browser.newPage();
    await page.goto(origin + '/app/');
    await expect(
      page.getByRole('button', { name: 'Find your wavelength', exact: true }),
    ).toBeVisible();
    expect(
      await page.evaluate(async () => {
        const music = await (
          await caches.open('wavelength-device-music-v1')
        ).match('/app/_local/upgrade-probe');
        const database = await new Promise<IDBDatabase>((resolve) => {
          const open = indexedDB.open('wavelength-device-v1');
          open.onsuccess = () => resolve(open.result);
        });
        const session = await new Promise<string>((resolve) => {
          const read = database.transaction('state').objectStore('state').get('upgrade-probe');
          read.onsuccess = () => resolve(read.result);
        });
        database.close();
        return { music: await music?.text(), session };
      }),
    ).toEqual({ music: 'saved music', session: 'saved session' });
  } finally {
    await browser?.close();
    await request.post(origin + '/__test/update');
    await rm(profile, { recursive: true, force: true });
  }
});
