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
          },
        ],
      },
      GetReleases: {
        releases: [
          {
            ref: { provider: 'itunes', id: '789' },
            title: 'Next Chapter',
            artist: 'The Morning',
            tracks: [external],
          },
        ],
      },
      GetRelease: {
        release: {
          ref: { provider: 'itunes', id: '789' },
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
  await page
    .getByRole('button', { name: 'Choose recording for New Light' })
    .click();
  const sources = page.getByRole('dialog', {
    name: 'Choose a recording for New Light',
  });
  await expect(
    sources.getByRole('link', { name: /Open New Light.*on YouTube/ }),
  ).toHaveAttribute('href', 'https://www.youtube.com/watch?v=BaW_jenozKc');
  await expect(sources.getByRole('link')).toHaveAttribute('target', '_blank');
  await page.getByRole('button', { name: /98.*New Light/ }).click();
  const dialog = page.getByRole('dialog', { name: 'Review download' });
  await expect(dialog).toContainText('New Light — Official Audio');
  await dialog.getByRole('button', { name: 'Download 1 song' }).hover();
  await page.screenshot({ path: info.outputPath('review-light.png') });
  await dialog.getByRole('button', { name: 'Back', exact: true }).click();
  await page.getByRole('button', { name: 'Toggle color theme' }).click();
  await page.getByRole('button', { name: 'Review download' }).click();
  await dialog.getByRole('button', { name: 'Download 1 song' }).hover();
  await page.screenshot({ path: info.outputPath('review-dark.png') });
  await dialog
    .getByRole('combobox', { name: 'Download playlist' })
    .selectOption('playlist-1');
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
  await page.getByRole('link', { name: 'The Morning', exact: true }).click();
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
  await expect(palette.locator('.library-summary')).toHaveText(
    '1 of 2 songs in library',
  );
  await page.evaluate(
    (ref) =>
      window.testMusicEvent({
        libraryUpdated: { track: { metadataRef: ref, trackId: 'track-2' } },
      }),
    another.ref,
  );
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
