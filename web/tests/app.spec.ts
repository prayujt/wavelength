import { expect, test, type Page } from '@playwright/test';

const track = {
  id: 'track-1',
  title: 'First Light',
  artist: 'The Morning',
  album: 'Small Hours',
  durationSeconds: 183,
  sizeBytes: '4096',
};
const ref = { provider: 'itunes', id: '123' };
const external = {
  ref,
  title: 'New Light',
  artist: 'The Morning',
  album: 'Next Chapter',
  durationSeconds: 180,
};

async function signedIn(page: Page) {
  await page.context().addCookies([
    {
      name: 'wavelength_session',
      value: 'test-session',
      url: 'http://127.0.0.1:5173',
      httpOnly: true,
      sameSite: 'Lax',
    },
  ]);
  await page.route('**/auth/me', (route) =>
    route.fulfill({
      json: {
        user: { id: 'listener-1', name: 'Jamie', email: 'jamie@example.test' },
        csrf_token: 'test-csrf',
      },
    }),
  );
  await page.route(/\/wavelength\.v1\./, async (route) => {
    const request = route.request();
    expect(request.headers()['x-csrf-token']).toBe('test-csrf');
    expect(request.headers()['authorization']).toBeUndefined();
    expect(request.headers()['connect-protocol-version']).toBe('1');
    const method = new URL(request.url()).pathname.split('/').pop()!;
    const responses: Record<string, object> = {
      GetCurrentUser: { user: { id: 'listener-1', name: 'Jamie' } },
      GetQueue: {},
      SaveQueue: {},
      ListTracks: { tracks: [track], total: 1 },
      GetTrack: { track },
      ListAlbums: {
        albums: [
          {
            id: 'album-1',
            name: 'Small Hours',
            artist: 'The Morning',
            trackCount: 1,
          },
        ],
        total: 1,
      },
      ListArtists: {
        artists: [{ id: 'artist-1', name: 'The Morning', albumCount: 1 }],
        total: 1,
      },
      GetAlbum: {
        album: { id: 'album-1', name: 'Small Hours' },
        tracks: [track],
      },
      GetArtist: {
        artist: { id: 'artist-1', name: 'The Morning' },
        albums: [
          {
            id: 'album-1',
            name: 'Small Hours',
            artist: 'The Morning',
            trackCount: 1,
          },
        ],
      },
      ListPlaylists: { playlists: [], total: 0 },
      ListImports: { imports: [], total: 0 },
      Search: {
        tracks: [external],
        artists: [
          { ref: { provider: 'itunes', id: '456' }, name: 'The Morning' },
        ],
        releases: [
          {
            ref: { provider: 'itunes', id: '789' },
            title: 'Next Chapter',
            artist: 'The Morning',
          },
        ],
      },
      GetRelease: {
        release: {
          title: 'Next Chapter',
          artist: 'The Morning',
          tracks: [external],
        },
      },
      FindSources: {
        candidates: [
          {
            ref: { provider: 'youtube', id: 'BaW_jenozKc' },
            title: 'New Light — Official Audio',
            uploader: 'The Morning',
            durationSeconds: 181,
            score: 0.98,
            suitable: true,
          },
        ],
      },
      CreateImport: { id: 'import-1', state: 'queued' },
    };
    await route.fulfill({ json: responses[method] || {} });
  });
}

test('signed-out users start OIDC login without entering an API token', async ({
  page,
}, testInfo) => {
  await page.route('**/auth/me', (route) =>
    route.fulfill({ status: 401, json: { error: 'unauthorized' } }),
  );
  await page.goto('/');
  await expect(
    page.getByRole('link', { name: 'Sign in', exact: true }),
  ).toHaveAttribute('href', '/auth/login?return_to=%2F');
  await expect(page.getByRole('textbox')).toHaveCount(0);
  await expect(page.getByRole('heading', { name: /Stay for/ })).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath('sign-in.png'),
    fullPage: true,
  });
});

test('library categories use generated Connect clients and keep the cookie private', async ({
  page,
}, testInfo) => {
  await signedIn(page);
  await page.goto('/');
  await expect(
    page.getByRole('button', { name: 'First Light', exact: true }),
  ).toBeVisible();
  await expect(page.locator('.account')).toContainText('Jamie');
  await page.screenshot({
    path: testInfo.outputPath('library.png'),
    fullPage: true,
  });
  expect(await page.evaluate(() => document.cookie)).not.toContain(
    'test-session',
  );
  await page.getByRole('button', { name: 'Albums', exact: true }).click();
  await page.getByRole('button', { name: /Small Hours.*The Morning/ }).click();
  await expect(
    page.getByRole('button', { name: 'First Light', exact: true }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Artists', exact: true }).click();
  await page.getByRole('button', { name: /The Morning.*1 album/ }).click();
  await expect(
    page.getByRole('heading', { name: 'The Morning', exact: true }),
  ).toBeVisible();
});

test('discovery lets the listener select a song and a recording before importing', async ({
  page,
}) => {
  await signedIn(page);
  await page.goto('/#discover');
  await page
    .getByRole('textbox', { name: 'Search for new music' })
    .fill('The Morning');
  await page.getByRole('button', { name: 'Search', exact: true }).click();
  await expect(
    page.getByRole('checkbox', { name: 'Select New Light' }),
  ).toBeVisible();
  await page
    .getByRole('button', { name: 'Choose recording for New Light' })
    .click();
  await page.getByRole('button', { name: /98.*New Light/ }).click();
  const request = page.waitForRequest(
    (request) =>
      request.url().endsWith('/IngestionService/CreateImport') ||
      request.url().endsWith('.IngestionService/CreateImport'),
  );
  await page
    .getByRole('button', { name: 'Add to library', exact: true })
    .click();
  const body = (await request).postDataJSON();
  expect(body.requestId).toBeTruthy();
  expect(body.items).toEqual([
    { metadataRef: ref, sourceRef: { provider: 'youtube', id: 'BaW_jenozKc' } },
  ]);
  await expect(page.getByRole('heading', { name: 'Downloads.' })).toBeVisible();
});

test('an uncertain import can choose a recording and retry without losing completed items', async ({
  page,
}) => {
  await signedIn(page);
  const job = {
    id: 'import-1',
    state: 'needs_selection',
    items: [
      {
        metadataRef: ref,
        metadata: external,
        state: 'needs_selection',
        error: 'Choose a recording',
      },
    ],
  };
  await page.route('**/wavelength.v1.IngestionService/*', (route) => {
    const method = new URL(route.request().url()).pathname.split('/').pop();
    return route.fulfill({
      json: method === 'ListImports' ? { imports: [job], total: 1 } : job,
    });
  });
  await page.goto('/#imports');
  await page
    .getByRole('button', { name: /New Light.*needs selection/ })
    .click();
  await page
    .getByRole('button', { name: 'Choose recording for New Light' })
    .click();
  await page.getByRole('button', { name: /98.*New Light/ }).click();
  const request = page.waitForRequest(
    '**/wavelength.v1.IngestionService/RetryImport',
  );
  await page.getByRole('button', { name: 'Retry remaining' }).click();
  expect((await request).postDataJSON()).toMatchObject({
    id: 'import-1',
    overrides: [{ sourceRef: { provider: 'youtube', id: 'BaW_jenozKc' } }],
  });
});

test('expired RPC credentials return to sign-in', async ({ page }) => {
  await signedIn(page);
  await page.route('**/wavelength.v1.LibraryService/ListTracks', (route) =>
    route.fulfill({
      status: 401,
      json: { code: 'unauthenticated', message: 'Session expired' },
    }),
  );
  await page.goto('/');
  await expect(
    page.getByRole('link', { name: 'Sign in', exact: true }),
  ).toBeVisible();
});

test('mobile library fits the viewport and keeps navigation accessible', async ({
  page,
}, testInfo) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await signedIn(page);
  await page.goto('/');
  await expect(
    page.getByRole('button', { name: 'First Light', exact: true }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
  await page.getByRole('button', { name: 'Show queue' }).click();
  await expect(
    page.getByRole('complementary', { name: 'Playback queue' }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Close queue' }).click();
  await page.screenshot({
    path: testInfo.outputPath('mobile-library.png'),
    fullPage: true,
  });
  await expect(
    page
      .getByRole('navigation')
      .getByRole('link', { name: 'Discover', exact: true }),
  ).toBeVisible();
});
