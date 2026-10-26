import { expect, test, type Page } from '@playwright/test';

declare global {
  interface Window {
    testMusicEvent: (event: object) => void;
    testEndMusicStream: () => void;
  }
}

const artwork = '/media/tracks/track-1/artwork';
const track = {
  id: 'track-1',
  artistId: 'artist-1',
  albumId: 'album-1',
  title: 'First Light',
  artist: 'The Morning',
  album: 'Small Hours',
  durationSeconds: 183,
  sizeBytes: '4096',
  artworkPath: artwork,
};
const album = {
  id: 'album-1',
  name: 'Small Hours',
  artist: 'The Morning',
  artistId: 'artist-1',
  trackCount: 1,
  artworkPath: artwork,
};
const artist = {
  id: 'artist-1',
  name: 'The Morning',
  albumCount: 1,
  artworkPath: artwork,
};
const ref = { provider: 'itunes', id: '123' };
const external = {
  ref,
  artistRef: { provider: 'itunes', id: '789' },
  releaseRef: { provider: 'itunes', id: '456' },
  title: 'New Light',
  artist: 'The Morning',
  album: 'Next Chapter',
  durationSeconds: 180,
};
const importedJob = {
  id: 'import-1',
  state: 'completed',
  items: [
    {
      metadataRef: ref,
      metadata: external,
      state: 'completed',
      trackId: track.id,
      albumId: album.id,
      artistId: artist.id,
    },
  ],
};

async function signedIn(page: Page, existing = false) {
  await page.addInitScript(() => {
    const originalFetch = window.fetch.bind(window);
    window.fetch = async (input, init) => {
      const url =
        typeof input === 'string'
          ? input
          : input instanceof URL
            ? input.href
            : input.url;
      if (!url.endsWith('/wavelength.v1.RealtimeService/Subscribe'))
        return originalFetch(input, init);
      const body = new ReadableStream<Uint8Array>({
        start(controller) {
          let closed = false;
          const send = (value: object, flags = 0) => {
            const bytes = new TextEncoder().encode(JSON.stringify(value));
            const frame = new Uint8Array(5 + bytes.length);
            frame[0] = flags;
            new DataView(frame.buffer).setUint32(1, bytes.length);
            frame.set(bytes, 5);
            controller.enqueue(frame);
          };
          window.testMusicEvent = (event) => send(event);
          window.testEndMusicStream = () => {
            send({}, 2);
            closed = true;
            controller.close();
          };
          init?.signal?.addEventListener(
            'abort',
            () => {
              if (!closed) {
                closed = true;
                controller.close();
              }
            },
            {
              once: true,
            },
          );
          send({ ready: {} });
        },
      });
      return new Response(body, {
        headers: { 'Content-Type': 'application/connect+json' },
      });
    };
  });
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
  await page.route('**/media/tracks/*/artwork', (route) =>
    route.fulfill({
      contentType: 'image/png',
      body: Buffer.from(
        'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+j4K0AAAAASUVORK5CYII=',
        'base64',
      ),
    }),
  );
  const wave = Buffer.alloc(48044);
  wave.write('RIFF');
  wave.writeUInt32LE(48036, 4);
  wave.write('WAVEfmt ', 8);
  wave.writeUInt32LE(16, 16);
  wave.writeUInt16LE(1, 20);
  wave.writeUInt16LE(1, 22);
  wave.writeUInt32LE(8000, 24);
  wave.writeUInt32LE(16000, 28);
  wave.writeUInt16LE(2, 32);
  wave.writeUInt16LE(16, 34);
  wave.write('data', 36);
  wave.writeUInt32LE(48000, 40);
  await page.route('**/media/tracks/track-1', (route) =>
    route.fulfill({
      contentType: 'audio/wav',
      headers: {
        'Accept-Ranges': 'bytes',
        'Content-Length': String(wave.length),
      },
      body: wave,
    }),
  );
  let created = false;
  await page.route(/\/wavelength\.v1\./, async (route) => {
    const request = route.request();
    expect(request.headers()['x-csrf-token']).toBe('test-csrf');
    expect(request.headers()['authorization']).toBeUndefined();
    expect(request.headers()['connect-protocol-version']).toBe('1');
    const method = new URL(request.url()).pathname.split('/').pop()!;
    if (method === 'CreateImport') created = true;
    const responses: Record<string, object> = {
      GetCurrentUser: { user: { id: 'listener-1', name: 'Jamie' } },
      GetQueue: {},
      GetPlayFeed: { items: [], exhausted: true },
      SaveQueue: {},
      RecordPlay: {},
      ListTracks: { tracks: [track], total: 1 },
      GetTrack: { track },
      GetPlaybackInfo: {
        track,
        httpPath: '/media/tracks/track-1',
        supportsRanges: true,
      },
      GetLyrics: { lyrics: '[00:00.00]First line\n[00:02.00]Second line' },
      ListAlbums: { albums: [album], total: 1 },
      ListArtists: { artists: [artist], total: 1 },
      GetAlbum: { album, tracks: [track] },
      GetArtist: { artist, albums: [album] },
      ListPlaylists: {
        playlists: [{ id: 'playlist-1', name: 'Downloads mix', trackIds: [] }],
        total: 1,
      },
      ListImports: {
        imports: created ? [importedJob] : [],
        total: created ? 1 : 0,
      },
      GetImport: importedJob,
      Search: {
        tracks: [external],
        artists: [
          {
            ref: { provider: 'itunes', id: '456' },
            name: 'The Morning',
            artworkUrl: artwork,
          },
        ],
        releases: [
          {
            ref: { provider: 'itunes', id: '789' },
            title: 'Next Chapter',
            artist: 'The Morning',
            artistRef: { provider: 'itunes', id: '456' },
          },
        ],
      },
      GetReleases: {
        releases: [
          {
            ref: { provider: 'itunes', id: '789' },
            title: 'Next Chapter',
            artist: 'The Morning',
            artistRef: { provider: 'itunes', id: '456' },
            tracks: [external],
          },
        ],
      },
      GetRelease: {
        release: {
          ref: { provider: 'itunes', id: '789' },
          title: 'Next Chapter',
          artist: 'The Morning',
          artistRef: { provider: 'itunes', id: '456' },
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
      LookupImports: {
        tracks: existing ? [{ metadataRef: ref, trackId: track.id }] : [],
      },
      CreateImport: importedJob,
    };
    if (request.url().includes('.DiscoveryService/GetArtist'))
      responses.GetArtist = {
        artist: {
          name: 'The Morning',
          ref: { provider: 'itunes', id: '456' },
          releases: [
            {
              ref: { provider: 'itunes', id: '789' },
              title: 'Next Chapter',
              artist: 'The Morning',
            },
          ],
        },
      };
    await route.fulfill({ json: responses[method] || {} });
  });
}

async function findNewMusic(page: Page) {
  await page.goto('/#discover');
  await page
    .getByRole('textbox', { name: 'Search for new music' })
    .fill('The Morning');
  await page.getByRole('button', { name: 'Search', exact: true }).click();
  await expect(
    page.getByRole('checkbox', { name: 'Select New Light' }),
  ).toBeVisible();
}

test('signed-out users start OIDC login without entering an API token', async ({
  page,
}, info) => {
  await page.route('**/auth/me', (route) =>
    route.fulfill({ status: 401, json: { error: 'unauthorized' } }),
  );
  await page.goto('/');
  await expect(
    page.getByRole('link', { name: 'Sign in', exact: true }),
  ).toHaveAttribute('href', '/auth/login?return_to=%2F');
  await expect(page.getByRole('textbox')).toHaveCount(0);
  await expect(
    page.getByRole('heading', { name: 'Your music library' }),
  ).toBeVisible();
  await page.screenshot({
    path: info.outputPath('sign-in.png'),
    fullPage: true,
  });
});

test('artwork loads and artist/album pages have persistent navigation', async ({
  page,
}, info) => {
  await signedIn(page);
  await page.goto('/');
  await expect(page.locator('.track-title img')).toBeVisible();
  await expect
    .poll(() =>
      page
        .locator('.track-title img')
        .evaluate((img: HTMLImageElement) => img.naturalWidth),
    )
    .toBeGreaterThan(0);
  expect(await page.evaluate(() => document.cookie)).not.toContain(
    'test-session',
  );
  await page.screenshot({
    path: info.outputPath('library.png'),
    fullPage: true,
  });
  await page
    .locator('.tabs')
    .getByRole('link', { name: 'Albums', exact: true })
    .click();
  await page.getByRole('link', { name: /Small Hours.*The Morning/ }).click();
  await expect(page).toHaveURL(/#album\/album-1$/);
  await expect(
    page.getByRole('heading', { name: 'Small Hours', exact: true }),
  ).toBeVisible();
  await page.reload();
  await expect(
    page.getByRole('button', { name: 'First Light', exact: true }),
  ).toBeVisible();
  await page
    .locator('.entity-header')
    .getByRole('link', { name: 'The Morning', exact: true })
    .click();
  await expect(page).toHaveURL(/#artist\/artist-1$/);
  await expect(
    page.getByRole('heading', { name: 'The Morning', exact: true }),
  ).toBeVisible();
  await page.screenshot({
    path: info.outputPath('artist.png'),
    fullPage: true,
  });
});

test('download review preserves source and playlist choices and opens job progress', async ({
  page,
}, info) => {
  await page.emulateMedia({ colorScheme: 'light', reducedMotion: 'reduce' });
  await signedIn(page);
  await findNewMusic(page);
  await page.getByRole('checkbox', { name: 'Select New Light' }).check();
  await page.getByRole('button', { name: 'Review download' }).click();
  const initialReview = page.getByRole('dialog', { name: 'Review download' });
  await expect(
    initialReview.getByText('Automatic matching', { exact: true }),
  ).toBeVisible();
  await expect(
    initialReview.getByRole('button', {
      name: 'Choose recording for New Light',
    }),
  ).toHaveText('Choose recording');
  await page
    .getByRole('button', { name: 'Choose recording for New Light' })
    .click();
  const sources = page.getByRole('dialog', {
    name: 'Choose a recording for New Light',
  });
  await expect(
    sources.getByRole('link', { name: /Open New Light.*on YouTube/ }),
  ).toHaveAttribute('href', 'https://www.youtube.com/watch?v=BaW_jenozKc');
  await expect(
    sources.getByRole('link', { name: /on YouTube/ }),
  ).toHaveAttribute('target', '_blank');
  await page.getByRole('button', { name: /98.*New Light/ }).click();
  const dialog = page.getByRole('dialog', { name: 'Review download' });
  await expect(dialog).toContainText('New Light — Official Audio');
  await expect(
    dialog.getByRole('button', { name: 'Change recording for New Light' }),
  ).toHaveText('Change recording');
  await dialog.getByRole('button', { name: 'Download 1 song' }).hover();
  await page.screenshot({ path: info.outputPath('review-light.png') });
  await dialog.getByRole('button', { name: 'Back', exact: true }).click();
  await page.getByRole('button', { name: 'Toggle color theme' }).click();
  await page.getByRole('button', { name: 'Review download' }).click();
  await dialog.getByRole('button', { name: 'Download 1 song' }).hover();
  await page.screenshot({ path: info.outputPath('review-dark.png') });
  await dialog.getByRole('combobox', { name: 'Download playlist' }).click();
  await page
    .getByRole('option', { name: 'Downloads mix', exact: true })
    .click();
  const submitted = page.waitForRequest(
    '**/wavelength.v1.IngestionService/CreateImport',
  );
  await dialog.getByRole('button', { name: 'Download 1 song' }).click();
  expect((await submitted).postDataJSON()).toMatchObject({
    requestId: expect.any(String),
    playlistId: 'playlist-1',
    items: [
      {
        metadataRef: ref,
        sourceRef: { provider: 'youtube', id: 'BaW_jenozKc' },
      },
    ],
  });
  await expect(page).toHaveURL(/#imports\/import-1$/);
  await expect(
    page.getByRole('heading', { name: 'Download complete' }),
  ).toBeVisible();
  await expect(
    page.getByRole('button', { name: 'Play downloaded' }),
  ).toBeVisible();
});

test('discovery identifies existing downloads and allows direct playback', async ({
  page,
}) => {
  await signedIn(page, true);
  await findNewMusic(page);
  await expect(
    page.getByRole('checkbox', { name: 'Select New Light' }),
  ).toBeDisabled();
  await expect(page.locator('.discovery-track')).toContainText('In library');
  await page
    .getByRole('button', { name: 'Play New Light', exact: true })
    .click();
  await expect(page.locator('.now-playing')).toContainText('First Light');
});

test('Cmd/Ctrl+K searches library and external music with different actions', async ({
  page,
}, info) => {
  await signedIn(page);
  await page.goto('/');
  await page.keyboard.press('Control+k');
  const dialog = page.getByRole('dialog', {
    name: 'Search music',
    exact: true,
  });
  await dialog.getByRole('combobox').fill('light');
  await expect(
    dialog.getByRole('option', { name: /First Light.*In library.*Play/ }),
  ).toBeVisible();
  await expect(
    dialog.getByRole('option', {
      name: /New Light.*Not downloaded.*Download/,
    }),
  ).toBeVisible();
  await page.screenshot({ path: info.outputPath('search.png') });
  const queued = page.waitForRequest(
    '**/wavelength.v1.IngestionService/CreateImport',
  );
  await dialog
    .getByRole('option', { name: /New Light.*Not downloaded.*Download/ })
    .click();
  expect((await queued).postDataJSON()).toMatchObject({
    items: [{ metadataRef: ref }],
  });
  await expect(
    page.getByRole('dialog', { name: 'Review download' }),
  ).toHaveCount(0);
  await page.keyboard.press('Meta+k');
  await expect(dialog).toBeVisible();
  await dialog.getByRole('combobox').press('ArrowDown');
  await dialog.getByRole('combobox').press('Enter');
  await expect(page).toHaveURL(/#discover$/);
});

test('external artist and release pages preserve selection while browsing', async ({
  page,
}) => {
  await signedIn(page);
  await findNewMusic(page);
  await page.getByRole('checkbox', { name: 'Select New Light' }).check();
  await page.locator('.artist-chip').filter({ hasText: 'The Morning' }).click();
  await expect(page).toHaveURL(/#discover\/artist/);
  await expect(
    page.getByRole('heading', { name: 'The Morning' }),
  ).toBeVisible();
  await page.getByRole('link', { name: 'Next Chapter', exact: true }).click();
  await expect(page).toHaveURL(/#discover\/release/);
  await expect(
    page.getByRole('checkbox', { name: 'Select New Light' }),
  ).toBeChecked();
  await expect(
    page.getByRole('heading', { name: 'Next Chapter' }),
  ).toBeVisible();
});

test('an uncertain download can choose a recording and retry', async ({
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
  await page.route('**/wavelength.v1.IngestionService/*', (route) =>
    route.fulfill({
      json: route.request().url().endsWith('ListImports')
        ? { imports: [job], total: 1 }
        : job,
    }),
  );
  await page.goto('/#imports');
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

test('mobile library keeps artwork and player controls visible', async ({
  page,
}, info) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await signedIn(page);
  await page.goto('/');
  await expect(page.locator('.track-title img')).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Show lyrics' })).toBeVisible();
  await page.getByRole('button', { name: 'Show queue' }).click();
  await expect(
    page.getByRole('complementary', { name: 'Playback queue' }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Close queue' }).click();
  await page.screenshot({
    path: info.outputPath('mobile-library.png'),
    fullPage: true,
  });
  await page.getByRole('button', { name: 'Show sidebar' }).click();
  await expect(
    page
      .getByRole('navigation')
      .getByRole('link', { name: 'Discover', exact: true }),
  ).toBeVisible();
});

test('missing artwork falls back to a cover instead of a broken image', async ({
  page,
}) => {
  await signedIn(page);
  await page.route('**/media/tracks/*/artwork', (route) =>
    route.fulfill({ status: 404 }),
  );
  await page.goto('/');
  await expect(
    page.getByRole('button', { name: 'First Light', exact: true }),
  ).toBeVisible();
  await expect(page.locator('.track-title .cover svg')).toBeVisible();
  await expect(page.locator('.track-title img')).toHaveCount(0);
});

test('Discover restores search, category and selection after artist and album navigation', async ({
  page,
}) => {
  await signedIn(page);
  let searches = 0;
  page.on('request', (request) => {
    if (request.url().endsWith('/wavelength.v1.DiscoveryService/Search'))
      searches++;
  });
  await findNewMusic(page);
  await page.getByRole('checkbox', { name: 'Select New Light' }).check();
  await page.getByRole('button', { name: 'Artists', exact: true }).click();
  const artistResult = page.locator('.artist-chip');
  await expect(artistResult.locator('img')).toBeVisible();
  await expect(artistResult.locator('img')).toHaveJSProperty('naturalWidth', 1);
  await artistResult.click();
  await page.getByRole('link', { name: 'Next Chapter', exact: true }).click();
  await page.locator('.back-link').click();
  await expect(
    page.getByRole('textbox', { name: 'Search for new music' }),
  ).toHaveValue('The Morning');
  await expect(
    page.getByRole('button', { name: 'Artists', exact: true }),
  ).toHaveAttribute('aria-pressed', 'true');
  expect(searches).toBe(1);
  await page.getByRole('button', { name: 'Songs', exact: true }).click();
  await expect(
    page.getByRole('checkbox', { name: 'Select New Light' }),
  ).toBeChecked();
  await page.reload();
  await expect(
    page.getByRole('textbox', { name: 'Search for new music' }),
  ).toHaveValue('The Morning');
  await expect(
    page.getByRole('button', { name: 'Songs', exact: true }),
  ).toHaveAttribute('aria-pressed', 'true');
});

test('streamed publication updates search badges and reconnect refreshes missed changes', async ({
  page,
}) => {
  await signedIn(page);
  await findNewMusic(page);
  await page.evaluate(
    ({ ref, id }) =>
      window.testMusicEvent({
        libraryUpdated: { track: { metadataRef: ref, trackId: id } },
      }),
    { ref, id: track.id },
  );
  await expect(
    page.getByRole('checkbox', { name: 'Select New Light' }),
  ).toBeDisabled();
  await expect(page.locator('.discovery-track')).toContainText('In library');
  await page.reload();
  await expect(
    page.getByRole('checkbox', { name: 'Select New Light' }),
  ).toBeEnabled();
  await page.route('**/wavelength.v1.IngestionService/LookupImports', (route) =>
    route.fulfill({
      json: { tracks: [{ metadataRef: ref, trackId: track.id }] },
    }),
  );
  await page.evaluate(() => window.testEndMusicStream());
  await expect(
    page.getByRole('checkbox', { name: 'Select New Light' }),
  ).toBeDisabled();
});

test('streamed job progress updates without polling and completed songs link to the library', async ({
  page,
}) => {
  await signedIn(page);
  const pending = {
    ...importedJob,
    state: 'running',
    items: [{ ...importedJob.items[0], state: 'downloading', trackId: '' }],
  };
  let reads = 0;
  await page.route('**/wavelength.v1.IngestionService/ListImports', (route) => {
    reads++;
    return route.fulfill({ json: { imports: [pending], total: 1 } });
  });
  await page.goto('/#imports');
  await expect(
    page.getByRole('heading', { name: 'Download progress' }),
  ).toBeVisible();
  const initialReads = reads;
  await page.evaluate(
    (job) => window.testMusicEvent({ importUpdated: job }),
    importedJob,
  );
  await expect(
    page.getByRole('heading', { name: 'Download complete' }),
  ).toBeVisible();
  await page.waitForTimeout(2200);
  expect(reads).toBe(initialReads);
  await expect(
    page.getByRole('link', { name: 'Open New Light in library' }),
  ).toHaveAttribute('href', '#album/album-1');
  await page.locator('.track-link').click();
  await expect(
    page.getByRole('heading', { name: 'Small Hours', exact: true }),
  ).toBeVisible();
});

test('timed lyrics follow playback and allow manual browsing before resuming', async ({
  page,
}, info) => {
  await page.emulateMedia({ reducedMotion: 'reduce', colorScheme: 'dark' });
  await signedIn(page);
  await page.route('**/wavelength.v1.LibraryService/GetLyrics', (route) =>
    route.fulfill({
      json: {
        lyrics: Array.from(
          { length: 60 },
          (_, index) =>
            `[00:${(index * 0.04).toFixed(2)}]Lyric line ${index + 1}`,
        ).join('\n'),
      },
    }),
  );
  await page.goto('/');
  await page.getByRole('button', { name: 'Play First Light' }).click();
  await page.getByRole('button', { name: 'Show lyrics', exact: true }).click();
  await expect
    .poll(() =>
      page
        .locator('audio')
        .evaluate((audio: HTMLAudioElement) => audio.readyState),
    )
    .toBeGreaterThan(0);
  const lyrics = page.getByRole('complementary', { name: 'Lyrics' });
  await expect(
    lyrics.getByRole('button', { name: 'Following lyrics' }),
  ).toHaveAttribute('aria-pressed', 'true');
  await page.locator('audio').evaluate((audio: HTMLAudioElement) => {
    audio.pause();
    audio.currentTime = 2;
    audio.dispatchEvent(new Event('timeupdate'));
  });
  await expect(lyrics.locator('[aria-current="true"]')).toHaveText(
    'Lyric line 51',
  );
  const scroll = lyrics.locator('.lyrics-scroll');
  await expect
    .poll(() => scroll.evaluate((element) => element.scrollTop))
    .toBeGreaterThan(100);
  await scroll.dispatchEvent('wheel', { deltaY: -200 });
  await expect(
    lyrics.getByRole('button', { name: 'Follow lyrics', exact: true }),
  ).toHaveAttribute('aria-pressed', 'false');
  await scroll.evaluate((element) => (element.scrollTop = 0));
  await page.locator('audio').evaluate((audio: HTMLAudioElement) => {
    audio.currentTime = 2.2;
    audio.dispatchEvent(new Event('timeupdate'));
  });
  await expect(lyrics.locator('[aria-current="true"]')).toHaveText(
    'Lyric line 56',
  );
  await expect
    .poll(() => scroll.evaluate((element) => element.scrollTop))
    .toBe(0);
  await lyrics
    .getByRole('button', { name: 'Follow lyrics', exact: true })
    .click();
  await expect
    .poll(() => scroll.evaluate((element) => element.scrollTop))
    .toBeGreaterThan(100);
  await page.screenshot({ path: info.outputPath('lyrics-dark.png') });
});

test('search summaries distinguish a partially saved result set from fully in library', async ({
  page,
}) => {
  await signedIn(page, true);
  const another = {
    ...external,
    ref: { provider: 'itunes', id: '124' },
    title: 'Another Light',
  };
  await page.route('**/wavelength.v1.DiscoveryService/Search', (route) =>
    route.fulfill({ json: { tracks: [external, another] } }),
  );
  await findNewMusic(page);
  await expect(page.locator('.library-summary')).toHaveText(
    '1 of 2 songs in library',
  );
  await page.evaluate(
    (ref) =>
      window.testMusicEvent({
        libraryUpdated: { track: { metadataRef: ref, trackId: 'track-2' } },
      }),
    another.ref,
  );
  await expect(page.locator('.library-summary')).toContainText(
    'Fully in library',
  );
  await page.keyboard.press('Control+k');
  const palette = page.getByRole('dialog', { name: 'Search music' });
  await palette.getByRole('combobox').fill('The Morning');
  await expect(palette.locator('.library-summary')).toContainText(
    'Fully in library',
  );
});

test('Cmd/Ctrl+K can open the full Discover search with its query', async ({
  page,
}) => {
  await signedIn(page);
  await page.goto('/');
  await page.keyboard.press('Control+k');
  const palette = page.getByRole('dialog', { name: 'Search music' });
  await palette.getByRole('combobox').fill('The Morning');
  await palette.getByRole('option', { name: /Search in Discover/ }).click();
  await expect(palette).not.toBeVisible();
  await expect(
    page.getByRole('textbox', { name: 'Search for new music' }),
  ).toHaveValue('The Morning');
  await expect(
    page.getByRole('checkbox', { name: 'Select New Light' }),
  ).toBeVisible();
  await expect(page).toHaveURL(/#discover\?q=The\+Morning&category=all$/);
});

test('download history and job details scroll independently without moving the page', async ({
  page,
}, info) => {
  await signedIn(page);
  const manyItems = Array.from({ length: 40 }, (_, index) => ({
    ...importedJob.items[0],
    state: 'needs_selection',
    error:
      'choose a recording with find_audio_sources, then retry this item with its source reference',
    metadata: { ...external, title: `Song ${index + 1}` },
  }));
  const jobs = Array.from({ length: 20 }, (_, index) => ({
    ...importedJob,
    id: `import-${index + 1}`,
    state: 'needs_selection',
    items: manyItems,
  }));
  await page.route('**/wavelength.v1.IngestionService/ListImports', (route) =>
    route.fulfill({ json: { imports: jobs, total: 20 } }),
  );
  await page.goto('/#imports');
  await expect(
    page.getByRole('heading', { name: 'Choose a recording' }),
  ).toBeVisible();
  await expect(page.locator('.job-detail')).not.toContainText(
    'find_audio_sources',
  );
  for (const size of [
    { width: 1440, height: 900 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(size);
    if (
      size.width < 760 &&
      (await page.getByRole('button', { name: 'Hide sidebar' }).isVisible())
    )
      await page.getByRole('button', { name: 'Hide sidebar' }).click();
    const list = page.locator('.job-list');
    const detail = page.locator('.job-detail');
    await expect
      .poll(() =>
        detail.evaluate(
          (element) => element.scrollHeight > element.clientHeight,
        ),
      )
      .toBe(true);
    await expect
      .poll(() =>
        list.evaluate((element) => element.scrollHeight > element.clientHeight),
      )
      .toBe(true);
    await list.evaluate((element) => (element.scrollTop = 160));
    await detail.evaluate((element) => (element.scrollTop = 300));
    expect(await page.evaluate(() => window.scrollY)).toBe(0);
    expect(
      await page.evaluate(() => document.documentElement.scrollHeight),
    ).toBeLessThanOrEqual(size.height);
    expect(await list.evaluate((element) => element.scrollTop)).toBeGreaterThan(
      0,
    );
    expect(
      await detail.evaluate((element) => element.scrollTop),
    ).toBeGreaterThan(0);
    await page.screenshot({
      path: info.outputPath(`downloads-${size.width}.png`),
    });
  }
});

test('album search checks every song and becomes playable when the last one arrives', async ({
  page,
}, info) => {
  await signedIn(page);
  const second = {
    ...external,
    ref: { provider: 'itunes', id: '124' },
    title: 'Last Light',
  };
  await page.route('**/wavelength.v1.DiscoveryService/GetReleases', (route) =>
    route.fulfill({
      json: {
        releases: [
          {
            ref: { provider: 'itunes', id: '789' },
            title: 'Next Chapter',
            artist: 'The Morning',
            artistRef: { provider: 'itunes', id: '456' },
            tracks: [external, second],
          },
        ],
      },
    }),
  );
  await page.route('**/wavelength.v1.IngestionService/LookupImports', (route) =>
    route.fulfill({
      json: { tracks: [{ metadataRef: ref, trackId: track.id }] },
    }),
  );
  await findNewMusic(page);
  const card = page.locator('.external-release');
  await expect(card).toContainText('1 of 2 songs in library');
  await card.hover();
  const request = page.waitForRequest(
    '**/wavelength.v1.IngestionService/CreateImport',
  );
  await card
    .getByRole('button', { name: 'Download album Next Chapter' })
    .click();
  expect((await request).postDataJSON().items).toEqual([
    { metadataRef: second.ref },
  ]);
  await page.evaluate(
    (metadataRef) =>
      window.testMusicEvent({
        libraryUpdated: { track: { metadataRef, trackId: 'track-2' } },
      }),
    second.ref,
  );
  await expect(card).toContainText('Fully in library');
  await page.route('**/wavelength.v1.LibraryService/GetTrack', (route) =>
    route.fulfill({ json: { track } }),
  );
  await card.getByRole('button', { name: 'Play album Next Chapter' }).click();
  await expect(page.locator('.now-playing')).toContainText('First Light');
  await page.screenshot({ path: info.outputPath('owned-album-search.png') });
  await page.reload();
  await expect(page.locator('.external-release')).toContainText(
    '1 of 2 songs in library',
  );
});

test('global search shows album ownership before opening the album', async ({
  page,
}) => {
  await signedIn(page, true);
  await page.goto('/');
  await page.keyboard.press('Control+k');
  const dialog = page.getByRole('dialog', { name: 'Search music' });
  await dialog.getByRole('combobox').fill('The Morning');
  await expect(
    dialog.getByRole('option', { name: /Next Chapter.*Fully in library/ }),
  ).toBeVisible();
  await dialog.getByRole('button', { name: 'Play Next Chapter' }).click();
  await expect(page.locator('.now-playing')).toContainText('First Light');
});

test('direct download queues automatically and keeps the search in place', async ({
  page,
}) => {
  await signedIn(page);
  await findNewMusic(page);
  const request = page.waitForRequest(
    '**/wavelength.v1.IngestionService/CreateImport',
  );
  await page
    .locator('.discovery-track')
    .getByRole('button', { name: 'Download New Light', exact: true })
    .last()
    .click();
  expect((await request).postDataJSON()).toMatchObject({
    requestId: expect.any(String),
    items: [{ metadataRef: ref }],
  });
  await expect(page).toHaveURL(/#discover\?/);
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(page.locator('.discovery-track')).toContainText('Queued');
});

test('saved songs can queue an explicit replacement through their options menu', async ({
  page,
}) => {
  await signedIn(page, true);
  await page.goto('/');
  await page
    .getByRole('button', { name: 'More options for First Light' })
    .click();
  const lookup = page.waitForRequest(
    '**/wavelength.v1.IngestionService/LookupImports',
  );
  await page
    .getByRole('button', { name: 'Replace audio', exact: true })
    .click();
  expect((await lookup).postDataJSON()).toEqual({ trackIds: [track.id] });
  const picker = page.getByRole('dialog', {
    name: 'Choose a recording for First Light',
  });
  await expect(
    picker.getByRole('link', { name: /on YouTube/ }),
  ).toHaveAttribute('href', 'https://www.youtube.com/watch?v=BaW_jenozKc');
  const request = page.waitForRequest(
    '**/wavelength.v1.IngestionService/CreateImport',
  );
  await picker.getByRole('button', { name: /98.*New Light/ }).click();
  expect((await request).postDataJSON()).toMatchObject({
    items: [
      {
        metadataRef: ref,
        sourceRef: { provider: 'youtube', id: 'BaW_jenozKc' },
        replaceTrackId: track.id,
      },
    ],
  });
  await expect(page).toHaveURL(/#imports\/import-1$/);
});

test('quick download retries reuse the request ID after a network failure', async ({
  page,
}) => {
  await signedIn(page);
  let attempts = 0;
  const keys: string[] = [];
  await page.route(
    '**/wavelength.v1.IngestionService/CreateImport',
    (route) => {
      keys.push(route.request().postDataJSON().requestId);
      attempts++;
      return attempts === 1
        ? route.fulfill({
            status: 503,
            json: { code: 'unavailable', message: 'Please try again' },
          })
        : route.fulfill({ json: importedJob });
    },
  );
  await findNewMusic(page);
  const download = page
    .locator('.discovery-track')
    .getByRole('button', { name: 'Download New Light', exact: true })
    .last();
  await download.click();
  await expect(
    page.getByText('Please try again', { exact: true }),
  ).toBeVisible();
  await download.click();
  await expect(page.locator('.discovery-track')).toContainText('Queued');
  expect(keys).toHaveLength(2);
  expect(keys[0]).toBe(keys[1]);
});

for (const partial of [false, true]) {
  test(`a download awaiting selection can be cancelled${partial ? ' while keeping saved songs' : ''}`, async ({
    page,
  }) => {
    await signedIn(page);
    const pending = {
      ...importedJob.items[0],
      state: 'needs_selection',
      trackId: '',
      metadata: { ...external, title: 'Waiting Song' },
    };
    let job = {
      ...importedJob,
      state: 'needs_selection',
      items: partial ? [importedJob.items[0], pending] : [pending],
    };
    await page.route('**/wavelength.v1.IngestionService/*', (route) => {
      const method = route.request().url().split('/').pop();
      if (method === 'CancelImport')
        job = {
          ...job,
          state: 'cancelled',
          items: job.items.map((item) =>
            item.state === 'completed' ? item : { ...item, state: 'cancelled' },
          ),
        };
      return route.fulfill({
        json: method === 'ListImports' ? { imports: [job], total: 1 } : job,
      });
    });
    await page.goto('/#imports');
    await expect(
      page.getByRole('heading', { name: 'Choose a recording' }),
    ).toBeVisible();
    const request = page.waitForRequest(
      '**/wavelength.v1.IngestionService/CancelImport',
    );
    await page
      .getByRole('button', {
        name: partial ? 'Cancel remaining' : 'Cancel download',
        exact: true,
      })
      .click();
    expect((await request).postDataJSON()).toEqual({ id: importedJob.id });
    await expect(page.locator('.job-card .status')).toHaveText(/^cancelled$/i);
    await expect(
      page.getByRole('button', { name: /Cancel (remaining|download)/ }),
    ).toHaveCount(0);
    if (partial) {
      await expect(
        page.getByRole('button', { name: 'Play downloaded' }),
      ).toBeVisible();
      await expect(page.locator('.track-link')).toContainText(external.title);
    }
    await page.reload();
    await expect(page.locator('.job-card .status')).toHaveText(/^cancelled$/i);
  });
}

test('saved albums reuse search metadata, have a normal Play action and link their artist', async ({
  page,
}) => {
  await signedIn(page, true);
  const reads: string[] = [];
  page.on('request', (request) => {
    if (
      ['/GetRelease', '/LookupImports', '/GetArtist'].some((method) =>
        request.url().endsWith(method),
      )
    )
      reads.push(request.url().split('/').pop()!);
  });
  await findNewMusic(page);
  await expect(page.locator('.external-release')).toContainText(
    'Fully in library',
  );
  const lookups = reads.filter((method) => method === 'LookupImports').length;
  await page
    .locator('.external-release')
    .getByRole('link', { name: 'Next Chapter', exact: true })
    .click();
  const header = page.locator('.entity-header');
  await expect(
    header.getByRole('button', { name: 'Play', exact: true }),
  ).toBeVisible();
  await expect(header).not.toContainText('Select remaining songs');
  await expect(header).not.toContainText('Play saved songs');
  expect(reads.filter((method) => method === 'GetRelease')).toHaveLength(0);
  expect(reads.filter((method) => method === 'LookupImports')).toHaveLength(
    lookups,
  );
  await header.getByRole('button', { name: 'Play', exact: true }).click();
  await expect(page.locator('.now-playing')).toContainText(track.title);
  await header.getByRole('link', { name: 'The Morning' }).click();
  await expect(page).toHaveURL(/#discover\/artist.*id=456/);
  await expect(
    page.getByRole('heading', { name: 'The Morning' }),
  ).toBeVisible();
  await page
    .getByRole('link', { name: 'Discover', exact: true })
    .last()
    .click();
  await page.locator('.artist-chip').filter({ hasText: 'The Morning' }).click();
  await expect(
    page.getByRole('heading', { name: 'The Morning' }),
  ).toBeVisible();
  expect(reads.filter((method) => method === 'GetArtist')).toHaveLength(1);
});

test('large artist discographies check one visible batch instead of every album', async ({
  page,
}) => {
  await signedIn(page);
  const albums = Array.from({ length: 60 }, (_, index) => ({
    ref: { provider: 'itunes', id: String(1000 + index) },
    title: `Album ${index + 1}`,
    artist: 'The Morning',
  }));
  const batches: string[][] = [];
  await page.route('**/wavelength.v1.DiscoveryService/GetArtist', (route) =>
    route.fulfill({
      json: {
        artist: {
          name: 'The Morning',
          ref: { provider: 'itunes', id: '456' },
          releases: albums,
        },
      },
    }),
  );
  await page.route('**/wavelength.v1.DiscoveryService/GetReleases', (route) => {
    const ids = route
      .request()
      .postDataJSON()
      .refs.map((ref: { id: string }) => ref.id);
    batches.push(ids);
    return route.fulfill({
      json: {
        releases: albums
          .filter((album) => ids.includes(album.ref.id))
          .map((album) => ({
            ...album,
            tracks: [
              {
                ...external,
                ref: { provider: 'itunes', id: `${album.ref.id}1` },
              },
            ],
          })),
      },
    });
  });
  await page.goto('/#discover/artist?provider=itunes&id=456');
  await expect(page.locator('.external-release')).toHaveCount(12);
  const more = page.getByRole('button', { name: 'Show more albums' });
  await expect(more).toBeEnabled();
  expect(batches).toEqual([albums.slice(0, 12).map((album) => album.ref.id)]);
  await more.click();
  await expect(page.locator('.external-release')).toHaveCount(24);
  await expect(more).toBeEnabled();
  expect(batches[1]).toEqual(albums.slice(12, 24).map((album) => album.ref.id));
});

test('track hover actions cover the artwork and keep contrast in both themes', async ({
  page,
}, info) => {
  await signedIn(page, true);
  await page.emulateMedia({ reducedMotion: 'reduce', colorScheme: 'light' });
  await findNewMusic(page);
  const artwork = page.locator('.track-artwork');
  const button = artwork.getByRole('button', { name: 'Play New Light' });
  for (const theme of ['light', 'dark']) {
    if (theme === 'dark')
      await page.getByRole('button', { name: 'Toggle color theme' }).click();
    await button.hover();
    const area = await artwork.boundingBox();
    const overlay = await button.boundingBox();
    expect(overlay).toEqual(area);
    const colors = await button.evaluate((element) => ({
      background: getComputedStyle(element).backgroundColor,
      color: getComputedStyle(element).color,
    }));
    expect(colors.color).toBe('rgb(255, 255, 255)');
    await expect
      .poll(() =>
        button.evaluate((element) => getComputedStyle(element).backgroundColor),
      )
      .toBe('rgba(22, 23, 28, 0.91)');
    await page.screenshot({
      path: info.outputPath(`track-hover-${theme}.png`),
    });
  }
});

test('playlist covers use one image, two halves, or four cells with a blank fourth for three songs', async ({
  page,
}, info) => {
  await signedIn(page);
  const colors = ['#c0a887', '#697d69', '#747c9c', '#ae8790'];
  await page.route('**/media/tracks/*/artwork', (route) => {
    const index =
      Number(
        new URL(route.request().url()).pathname.split('/')[3].split('-')[1],
      ) - 1;
    return route.fulfill({
      contentType: 'image/svg+xml',
      body: `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100"><rect width="100" height="100" fill="${colors[index]}"/></svg>`,
    });
  });
  const lists = Array.from({ length: 5 }, (_, count) => ({
    id: `playlist-${count}`,
    name: `Playlist ${count}`,
    trackIds: Array.from({ length: count }, (_, index) => `track-${index + 1}`),
  }));
  await page.route('**/wavelength.v1.UserDataService/ListPlaylists', (route) =>
    route.fulfill({ json: { playlists: lists, total: lists.length } }),
  );
  await page.goto('/#playlists');
  for (let count = 0; count <= 4; count++) {
    const card = page.getByRole('button', {
      name: new RegExp(`Playlist ${count} ${count} songs?`),
    });
    const mosaic = card.locator('.playlist-cover');
    await expect(mosaic.locator('img')).toHaveCount(count);
    if (count === 2) await expect(mosaic).toHaveClass(/split/);
    if (count >= 3) await expect(mosaic).toHaveClass(/mosaic/);
    if (count === 3) await expect(mosaic.locator('.empty-tile')).toHaveCount(1);
    const images = mosaic.locator('img');
    for (let index = 0; index < count; index++)
      await expect(images.nth(index)).toHaveAttribute(
        'src',
        `/media/tracks/track-${index + 1}/artwork`,
      );
  }
  await page.screenshot({ path: info.outputPath('playlist-covers.png') });
});

test('switching download jobs preserves history scroll without reloading the page', async ({
  page,
}) => {
  await signedIn(page);
  const jobs = Array.from({ length: 20 }, (_, index) => ({
    ...importedJob,
    id: `job-${index}`,
    items: [
      {
        ...importedJob.items[0],
        metadata: { ...external, title: `Saved song ${index}` },
      },
    ],
  }));
  let lists = 0;
  let details = 0;
  await page.route('**/wavelength.v1.IngestionService/*', (route) => {
    const method = route.request().url().split('/').pop();
    if (method === 'ListImports') lists++;
    if (method === 'GetImport') details++;
    return route.fulfill({
      json: method === 'ListImports' ? { imports: jobs, total: 20 } : jobs[0],
    });
  });
  await page.goto('/#imports');
  await expect(page.locator('.job-detail')).toContainText('Saved song 0');
  const before = lists;
  const list = page.locator('.job-list');
  await list.evaluate((element) => (element.scrollTop = 650));
  const position = await list.evaluate((element) => element.scrollTop);
  await list.getByRole('link', { name: /Saved song 9 / }).click();
  await expect(page.locator('.job-detail')).toContainText('Saved song 9');
  expect(await list.evaluate((element) => element.scrollTop)).toBe(position);
  await expect(page.getByText('Loading downloads…')).toHaveCount(0);
  expect(lists).toBe(before);
  expect(details).toBe(0);
});

test('opening lyrics immediately centers the current line while playback is paused', async ({
  page,
}) => {
  await signedIn(page);
  await page.route('**/wavelength.v1.LibraryService/GetLyrics', (route) =>
    route.fulfill({
      json: {
        lyrics: Array.from(
          { length: 60 },
          (_, i) => `[00:${(i * 0.04).toFixed(2)}]Lyric line ${i + 1}`,
        ).join('\n'),
      },
    }),
  );
  await page.goto('/');
  await page
    .getByRole('button', { name: 'Play First Light', exact: true })
    .click();
  await expect
    .poll(() =>
      page
        .locator('audio')
        .evaluate((audio: HTMLAudioElement) => audio.readyState),
    )
    .toBeGreaterThan(0);
  await page.locator('audio').evaluate((audio: HTMLAudioElement) => {
    audio.pause();
    audio.currentTime = 2;
    audio.dispatchEvent(new Event('timeupdate'));
  });
  for (let attempt = 0; attempt < 2; attempt++) {
    await page
      .getByRole('button', { name: 'Show lyrics', exact: true })
      .click();
    const lyrics = page.getByRole('complementary', {
      name: 'Lyrics',
      exact: true,
    });
    await expect(lyrics.locator('[aria-current="true"]')).toHaveText(
      'Lyric line 51',
    );
    await expect
      .poll(
        () =>
          lyrics
            .locator('.lyrics-scroll')
            .evaluate((element) => element.scrollTop),
        { timeout: 1500 },
      )
      .toBeGreaterThan(100);
    await lyrics
      .getByRole('button', { name: 'Close lyrics', exact: true })
      .click();
  }
});

test('background library publications preserve rows, menus and scroll while coalescing updates', async ({
  page,
}) => {
  await signedIn(page);
  let rows = Array.from({ length: 40 }, (_, i) => ({
    ...track,
    id: `saved-${i}`,
    title: `Saved song ${i}`,
    artist: 'The Morning',
  }));
  let calls = 0;
  let blocked: (() => void) | undefined;
  await page.route(
    '**/wavelength.v1.LibraryService/ListTracks',
    async (route) => {
      calls++;
      if (blocked) await new Promise<void>((resolve) => (blocked = resolve));
      await route.fulfill({ json: { tracks: rows, total: rows.length } });
    },
  );
  await page.goto('/');
  await expect(page.locator('.track-row[data-track-id]')).toHaveCount(40);
  // Wait for the initial stream snapshot refresh before testing a publication burst.
  await expect.poll(() => calls).toBeGreaterThanOrEqual(2);
  await page.locator('[data-track-id="saved-15"]').scrollIntoViewIfNeeded();
  const menu = page.getByRole('button', {
    name: 'More options for Saved song 15',
  });
  await menu.click();
  await page.evaluate(() => {
    const row = document.querySelector('[data-track-id="saved-15"]')!;
    (window as any).retainedRow = row;
    (window as any).removedRows = [];
    (window as any).newAnimations = [];
    const animate = Element.prototype.animate;
    Element.prototype.animate = function (
      ...args: Parameters<Element['animate']>
    ) {
      (window as any).newAnimations.push((this as HTMLElement).dataset.trackId);
      return animate.apply(this, args);
    };
    new MutationObserver((changes) => {
      for (const change of changes)
        for (const node of change.removedNodes)
          if (node === row || node.contains(row))
            (window as any).removedRows.push(true);
    }).observe(document.querySelector('.content')!, {
      childList: true,
      subtree: true,
    });
  });
  const before = calls;
  blocked = () => {};
  rows = [
    ...rows.slice(0, 16),
    { ...track, id: 'fresh-song', title: 'Fresh song' },
    ...rows.slice(16),
  ];
  await page.evaluate(() => {
    for (let i = 0; i < 12; i++)
      window.testMusicEvent({
        libraryUpdated: { track: { trackId: `import-${i}` } },
      });
  });
  await expect.poll(() => calls).toBe(before + 1);
  await expect(page.getByText('Loading library…', { exact: true })).toHaveCount(
    0,
  );
  await expect(menu).toHaveAttribute('aria-expanded', 'true');
  await page.evaluate(() =>
    window.testMusicEvent({
      libraryUpdated: { track: { trackId: 'another-import' } },
    }),
  );
  const release = blocked;
  blocked = undefined;
  release!();
  await expect(
    page.getByRole('button', { name: 'Fresh song', exact: true }),
  ).toBeVisible();
  await expect
    .poll(() => page.evaluate(() => (window as any).newAnimations))
    .toContain('fresh-song');
  expect(
    await page.evaluate(
      () =>
        document.querySelector('[data-track-id="saved-15"]') ===
        (window as any).retainedRow,
    ),
  ).toBe(true);
  expect(await page.evaluate(() => (window as any).removedRows)).toEqual([]);
  await expect(menu).toHaveAttribute('aria-expanded', 'true');
  await expect.poll(() => calls).toBe(before + 2);
  expect(await page.evaluate(() => window.scrollY)).toBeGreaterThan(200);
});

test('share expiry uses a styled keyboard menu and defaults to 30 days in both themes', async ({
  page,
}, info) => {
  await signedIn(page);
  let body: any;
  await page.route(
    '**/wavelength.v1.SharingService/CreateShare',
    async (route) => {
      body = route.request().postDataJSON();
      await route.fulfill({
        json: {
          url: 'https://music.example/s/public-token',
          link: {
            id: 'share-1',
            kind: 'share',
            name: track.title,
            expiresAt: new Date(Date.now() + 30 * 86400000).toISOString(),
          },
        },
      });
    },
  );
  await page.goto('/');
  await page
    .getByRole('button', { name: 'More options for First Light' })
    .click();
  await expect(
    page.getByRole('button', { name: 'Playback link', exact: true }),
  ).toHaveCount(0);
  await page.getByRole('button', { name: 'Share song', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Share music' });
  const expiry = dialog.getByRole('combobox', { name: 'Share expiry' });
  await expect(expiry).toHaveText('In 30 days');
  expect(await dialog.locator('select').count()).toBe(0);
  for (const theme of ['light', 'dark']) {
    await page.evaluate(
      (theme) => (document.documentElement.dataset.theme = theme),
      theme,
    );
    await expiry.click();
    await expect(
      page.getByRole('option', { name: 'In 30 days', exact: true }),
    ).toHaveAttribute('aria-selected', 'true');
    await page.screenshot({
      path: info.outputPath(`share-expiry-${theme}.png`),
    });
    await expiry.press('Escape');
    await expect(dialog).toBeVisible();
    await expect(expiry).toBeFocused();
  }
  await expiry.press('Home');
  await expiry.press('Enter');
  await expect(expiry).toHaveText('In 1 day');
  await expiry.click();
  await page.getByRole('option', { name: 'In 30 days', exact: true }).click();
  await dialog
    .getByRole('button', { name: 'Create link', exact: true })
    .click();
  await expect(
    dialog.getByRole('textbox', { name: 'Created link' }),
  ).not.toBeEmpty();
  expect(new Date(body.expiresAt).getTime() - Date.now()).toBeGreaterThan(
    29 * 86400000,
  );
});

test('playlist menus fit beside duration and song credits navigate to their entities', async ({
  page,
}) => {
  await signedIn(page);
  await page.route('**/wavelength.v1.UserDataService/GetPlaylist', (route) =>
    route.fulfill({
      json: {
        playlist: {
          id: 'playlist-1',
          name: 'Downloads mix',
          trackIds: [track.id],
        },
      },
    }),
  );
  await page.goto('/#playlists');
  await page
    .getByRole('button', { name: 'Downloads mix', exact: false })
    .first()
    .click();
  const row = page.locator('.track-row[data-track-id]').first();
  const menu = row.getByRole('button', {
    name: 'More options for First Light',
  });
  const duration = row.locator('.track-duration');
  for (const width of [1280, 390]) {
    await page.setViewportSize({ width, height: 800 });
    if (width === 390)
      await page.getByRole('button', { name: 'Hide sidebar' }).click();
    const menuBox = await menu.boundingBox();
    const durationBox = await duration.boundingBox();
    expect(menuBox!.x).toBeGreaterThanOrEqual(
      durationBox!.x + durationBox!.width,
    );
    expect(menuBox!.x + menuBox!.width).toBeLessThanOrEqual(width);
    await menu.click();
    await expect(
      row.getByRole('button', { name: 'Share song', exact: true }),
    ).toBeVisible();
    await menu.click();
  }
  await page.setViewportSize({ width: 1280, height: 800 });
  await expect(
    row.getByRole('link', { name: track.artist, exact: true }),
  ).toHaveAttribute('href', '#artist/artist-1');
  await expect(
    row.getByRole('link', { name: track.album, exact: true }).first(),
  ).toHaveAttribute('href', '#album/album-1');
  await row.getByRole('link', { name: track.artist, exact: true }).click();
  await expect(page).toHaveURL(/#artist\/artist-1$/);
});

test('Discover song artist and album links retain the current search context', async ({
  page,
}) => {
  await signedIn(page);
  await findNewMusic(page);
  const credits = page.locator('.discovery-track .song-credits').first();
  await expect(
    credits.getByRole('link', { name: 'The Morning', exact: true }),
  ).toHaveAttribute('href', /#discover\/artist\?.*q=The\+Morning.*id=789/);
  await expect(
    credits.getByRole('link', { name: 'Next Chapter', exact: true }),
  ).toHaveAttribute('href', /#discover\/release\?.*id=456/);
  await credits.getByRole('link', { name: 'The Morning', exact: true }).click();
  await expect(
    page.getByRole('heading', { name: 'The Morning', exact: true }),
  ).toBeVisible();
  await page
    .getByRole('link', { name: 'Discover', exact: true })
    .last()
    .click();
  await expect(
    page.getByRole('textbox', { name: 'Search for new music' }),
  ).toHaveValue('The Morning');
});

test('playback reports listening automatically without counting pauses or seek jumps', async ({
  page,
}) => {
  await signedIn(page);
  const second = { ...track, id: 'track-2', title: 'Second Light' };
  await page.route('**/wavelength.v1.LibraryService/ListTracks', (route) =>
    route.fulfill({ json: { tracks: [track, second], total: 2 } }),
  );
  const events: { method: string; body: Record<string, string | boolean> }[] =
    [];
  await page.route('**/wavelength.v1.ListeningService/*', async (route) => {
    const body = route.request().postDataJSON();
    const method = new URL(route.request().url()).pathname.split('/').pop()!;
    events.push({ method, body });
    await route.fulfill({ json: { id: body.requestId || body.sessionId } });
  });
  await page.goto('/');
  await page
    .getByRole('button', { name: 'Play First Light', exact: true })
    .click();
  await expect
    .poll(
      () => events.filter((e) => e.method === 'StartListeningSession').length,
    )
    .toBe(1);
  const id = events.find((e) => e.method === 'StartListeningSession')!.body
    .requestId;
  expect(events[0].body.origin).toBe('direct');
  await page.waitForTimeout(650);
  await page.getByRole('button', { name: 'Pause', exact: true }).click();
  await expect
    .poll(() =>
      events.some(
        (e) => e.method === 'ReportListeningProgress' && !e.body.playing,
      ),
    )
    .toBe(true);
  const paused = events
    .filter((e) => e.method === 'ReportListeningProgress' && !e.body.playing)
    .at(-1)!;
  expect(Number(paused.body.listenedMs)).toBeGreaterThan(350);
  expect(Number(paused.body.listenedMs)).toBeLessThan(1800);
  await page.waitForTimeout(350);
  await page.locator('audio').evaluate((audio: HTMLAudioElement) => {
    audio.currentTime = 2.5;
    audio.dispatchEvent(new Event('seeking'));
    audio.dispatchEvent(new Event('seeked'));
  });
  await page.getByRole('button', { name: 'Play', exact: true }).click();
  await expect
    .poll(
      () => events.filter((e) => e.method === 'StartListeningSession').length,
    )
    .toBe(2);
  const ended = events.find(
    (e) => e.method === 'FinishListeningSession' && e.body.sessionId === id,
  )!;
  expect(ended.body.outcome).toBe('completed');
  expect(Number(ended.body.listenedMs)).toBeGreaterThan(
    Number(paused.body.listenedMs),
  );
  expect(Number(ended.body.listenedMs)).toBeLessThan(
    Number(paused.body.listenedMs) + 1000,
  );
  const queued = events.filter((e) => e.method === 'StartListeningSession')[1];
  expect(queued.body.origin).toBe('queue');
  expect(queued.body.trackId).toBe('track-2');
  await page
    .getByRole('button', { name: 'Play First Light', exact: true })
    .click();
  await expect
    .poll(() =>
      events.some(
        (e) =>
          e.method === 'FinishListeningSession' &&
          e.body.sessionId === queued.body.requestId &&
          e.body.outcome === 'skipped',
      ),
    )
    .toBe(true);
  expect(
    events.filter(
      (e) => e.method === 'StartListeningSession' && e.body.requestId === id,
    ),
  ).toHaveLength(1);
});

test('restored playback begins a session only when resumed and retries a failed start with the same ID', async ({
  page,
}) => {
  await signedIn(page);
  await page.route('**/wavelength.v1.UserDataService/GetQueue', (route) =>
    route.fulfill({
      json: {
        queue: {
          trackIds: [track.id],
          currentTrackId: track.id,
          positionMs: '1000',
        },
      },
    }),
  );
  const starts: Record<string, string>[] = [];
  const reports: Record<string, string>[] = [];
  await page.route('**/wavelength.v1.ListeningService/*', async (route) => {
    const body = route.request().postDataJSON();
    if (route.request().url().endsWith('/StartListeningSession')) {
      starts.push(body);
      if (starts.length === 1) {
        await route.abort('failed');
        return;
      }
    } else reports.push(body);
    await route.fulfill({ json: { id: body.requestId || body.sessionId } });
  });
  await page.goto('/');
  await expect(page.locator('.now-playing')).toContainText('First Light');
  expect(starts).toHaveLength(0);
  await page.getByRole('button', { name: 'Play', exact: true }).click();
  await expect.poll(() => starts.length).toBe(1);
  await page.getByRole('button', { name: 'Pause', exact: true }).click();
  await expect.poll(() => starts.length).toBe(2);
  expect(starts[0].origin).toBe('restored');
  expect(starts[0].requestId).toBe(starts[1].requestId);
  await expect.poll(() => reports.length).toBeGreaterThan(0);
});

test('deleting a local song requires confirmation and refreshes the shared library', async ({
  page,
}) => {
  await signedIn(page);
  let deleted = false;
  let calls = 0;
  await page.route('**/wavelength.v1.LibraryService/ListTracks', (route) =>
    route.fulfill({
      json: { tracks: deleted ? [] : [track], total: deleted ? 0 : 1 },
    }),
  );
  await page.route(
    '**/wavelength.v1.LibraryService/DeleteTrack',
    async (route) => {
      calls++;
      expect(route.request().postDataJSON()).toEqual({
        id: track.id,
        confirm: true,
      });
      expect(route.request().headers()['x-csrf-token']).toBe('test-csrf');
      deleted = true;
      await route.fulfill({ json: {} });
      await page.evaluate(
        (id) =>
          window.testMusicEvent({ libraryUpdated: { deletedTrackId: id } }),
        track.id,
      );
    },
  );
  await page.goto('/');
  const row = page.locator(`[data-track-id="${track.id}"]`);
  await row
    .getByRole('button', { name: `More options for ${track.title}` })
    .click();
  await page
    .getByRole('button', { name: 'Delete from library', exact: true })
    .click();
  const dialog = page.getByRole('dialog', { name: 'Delete song from library' });
  await expect(dialog).toContainText('every listener');
  expect(calls).toBe(0);
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  expect(calls).toBe(0);
  await expect(row).toBeVisible();
  await row
    .getByRole('button', { name: `More options for ${track.title}` })
    .click();
  await page
    .getByRole('button', { name: 'Delete from library', exact: true })
    .click();
  await dialog
    .getByRole('button', { name: 'Delete song and files', exact: true })
    .click();
  await expect(row).toHaveCount(0);
  expect(calls).toBe(1);
});

for (const location of ['#album/album-1', '#library?track=track-1']) {
  test(`deletion clears a focused view at ${location}`, async ({ page }) => {
    await signedIn(page);
    let deleted = false;
    for (const method of ['GetTrack', 'GetAlbum'])
      await page.route(`**/wavelength.v1.LibraryService/${method}`, (route) =>
        deleted
          ? route.fulfill({
              status: 404,
              json: { code: 'not_found', message: 'Item deleted' },
            })
          : route.fulfill({
              json:
                method === 'GetTrack' ? { track } : { album, tracks: [track] },
            }),
      );
    await page.route(
      '**/wavelength.v1.LibraryService/DeleteTrack',
      async (route) => {
        deleted = true;
        await route.fulfill({ json: {} });
        await page.evaluate(
          (id) =>
            window.testMusicEvent({ libraryUpdated: { deletedTrackId: id } }),
          track.id,
        );
      },
    );
    await page.goto('/' + location);
    const row = page.locator(`[data-track-id="${track.id}"]`);
    await row
      .getByRole('button', { name: `More options for ${track.title}` })
      .click();
    await page
      .getByRole('button', { name: 'Delete from library', exact: true })
      .click();
    await page
      .getByRole('dialog', { name: 'Delete song from library' })
      .getByRole('button', { name: 'Delete song and files', exact: true })
      .click();
    await expect(row).toHaveCount(0);
  });
}

test('autoplay refills a short buffer, preserves manual choices and attributes explicit skips', async ({
  page,
}) => {
  await signedIn(page);
  await page.addInitScript(() => {
    const playing = new WeakSet<HTMLMediaElement>();
    Object.defineProperty(HTMLMediaElement.prototype, 'paused', {
      get() {
        return !playing.has(this);
      },
    });
    HTMLMediaElement.prototype.play = async function () {
      playing.add(this);
      this.dispatchEvent(new Event('play'));
    };
    HTMLMediaElement.prototype.pause = function () {
      if (playing.delete(this)) this.dispatchEvent(new Event('pause'));
    };
  });
  const songs = [
    track,
    ...[
      'Old One',
      'Manual Choice',
      'Old Two',
      'Feed One',
      'Feed Two',
      'Feed Three',
    ].map((title, index) => ({ ...track, id: `feed-${index}`, title })),
  ];
  await page.route('**/wavelength.v1.LibraryService/GetTrack', (route) =>
    route.fulfill({
      json: {
        track: songs.find((t) => t.id === route.request().postDataJSON().id),
      },
    }),
  );
  await page.route('**/wavelength.v1.UserDataService/GetQueue', (route) =>
    route.fulfill({
      json: {
        queue: {
          trackIds: [track.id, 'feed-1'],
          currentTrackId: track.id,
          recommendations: [],
          playFeedEnabled: true,
        },
      },
    }),
  );
  const requests: Record<string, unknown>[] = [];
  let chosen = ['feed-3', 'feed-4'];
  await page.route('**/wavelength.v1.PlayFeedService/GetPlayFeed', (route) => {
    requests.push(route.request().postDataJSON());
    return route.fulfill({
      json: {
        feedId: 'feed',
        items: chosen.map((id) => ({
          id: 'item-' + id,
          track: songs.find((t) => t.id === id),
        })),
        nextCursor: 'cursor',
        exhausted: false,
      },
    });
  });
  const listening: { method: string; body: Record<string, unknown> }[] = [];
  await page.route('**/wavelength.v1.ListeningService/*', (route) => {
    const body = route.request().postDataJSON();
    listening.push({ method: route.request().url().split('/').pop()!, body });
    return route.fulfill({ json: { id: body.requestId || body.sessionId } });
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Show queue' }).click();
  const queue = page.getByRole('complementary', { name: 'Playback queue' });
  await expect(queue.getByText('Recommended', { exact: true })).toHaveCount(2);
  await expect(
    queue.getByRole('button', { name: 'Feed One', exact: true }),
  ).toBeVisible();
  await expect(
    queue.getByRole('button', { name: 'Manual Choice', exact: true }),
  ).toBeVisible();
  await expect(
    queue.getByRole('button', { name: 'Old One', exact: true }),
  ).toHaveCount(0);
  expect(requests[0]).toMatchObject({
    seedTrackId: track.id,
    queuedTrackIds: [track.id, 'feed-1'],
    pageSize: 2,
  });
  chosen = ['feed-5'];
  await queue.getByRole('button', { name: 'Feed One', exact: true }).click();
  await expect
    .poll(
      () =>
        listening.find((e) => e.method === 'StartListeningSession')?.body
          .playFeedItemId,
    )
    .toBe('item-feed-3');
  await expect(
    queue.getByRole('button', { name: 'Feed Three', exact: true }),
  ).toBeVisible();
  chosen = [];
  await page.getByRole('button', { name: 'Next track', exact: true }).click();
  await expect
    .poll(
      () =>
        listening.find((e) => e.method === 'FinishListeningSession')?.body
          .changeReason,
    )
    .toBe('next');
  await expect(
    queue.getByRole('button', { name: 'Feed Three', exact: true }),
  ).toBeVisible();
  await expect(page.locator('.now-playing strong')).toHaveText('Feed Two');
  await expect
    .poll(() => requests.at(-1))
    .toMatchObject({
      seedTrackId: 'feed-4',
      excludedTrackIds: expect.arrayContaining(['feed-3']),
    });
  await queue.getByRole('button', { name: 'Autoplay on', exact: true }).click();
  await expect(
    queue.getByRole('button', { name: 'Feed Three', exact: true }),
  ).toHaveCount(0);
  await expect(page.locator('.now-playing strong')).toHaveText('Feed Two');
  await expect(
    queue.getByRole('button', { name: 'Autoplay off', exact: true }),
  ).toHaveAttribute('aria-pressed', 'false');
});
