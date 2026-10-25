import { expect, test, type Page } from '@playwright/test';

async function login(page: Page) {
  await page.goto('/');
  await page.getByRole('link', { name: 'Sign in', exact: true }).click();
  await expect(
    page.getByRole('button', { name: 'First Light', exact: true }),
  ).toBeVisible();
  await expect(page.locator('.account')).toContainText('Test Listener');
}

test('OIDC, embedded artwork, timed lyrics, playback and saved queue work together', async ({
  page,
}, info) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await login(page);
  const session = (await page.context().cookies()).find(
    (cookie) => cookie.name === 'wavelength_session',
  );
  expect(session?.httpOnly).toBe(true);
  expect(await page.evaluate(() => document.cookie)).not.toContain(
    'wavelength_session',
  );
  await expect
    .poll(() =>
      page
        .locator('.track-title img')
        .evaluate((img: HTMLImageElement) => img.naturalWidth),
    )
    .toBe(16);
  await page.screenshot({
    path: info.outputPath('library.png'),
    fullPage: true,
  });
  const media = page.waitForResponse(
    (response) =>
      /\/media\/tracks\/[^/]+$/.test(response.url()) && response.ok(),
  );
  await page
    .getByRole('button', { name: 'Play First Light', exact: true })
    .click();
  await media;
  await expect(
    page.getByRole('button', { name: 'Pause', exact: true }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Pause', exact: true }).click();
  await expect
    .poll(() =>
      page
        .locator('.now-playing img')
        .evaluate((img: HTMLImageElement) => img.naturalWidth),
    )
    .toBe(16);
  await page.getByRole('button', { name: 'Show lyrics' }).click();
  const lyrics = page.getByRole('complementary', {
    name: 'Lyrics',
    exact: true,
  });
  await expect(
    lyrics.getByRole('button', { name: 'First line', exact: true }),
  ).toBeVisible();
  const saved = page.waitForResponse(
    (response) =>
      response.url().endsWith('/SaveQueue') &&
      response.request().postDataJSON()?.positionMs === '2000',
  );
  await lyrics
    .getByRole('button', { name: 'Second line', exact: true })
    .click();
  await saved;
  await expect(
    lyrics.getByRole('button', { name: 'Second line', exact: true }),
  ).toHaveAttribute('aria-current', 'true');
  await page.screenshot({
    path: info.outputPath('lyrics.png'),
    fullPage: true,
  });
  await page.reload();
  await expect(page.locator('.now-playing')).toContainText('First Light');
  await expect(
    page.getByRole('button', { name: 'Play', exact: true }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(
    page.getByRole('link', { name: 'Sign in', exact: true }),
  ).toBeVisible();
  expect(errors).toEqual([]);
});

test('artist and album pages can be opened directly and survive reload', async ({
  page,
}, info) => {
  await login(page);
  await page
    .locator('.tabs')
    .getByRole('link', { name: 'Albums', exact: true })
    .click();
  await page.getByRole('link', { name: /Small Hours.*The Morning/ }).click();
  await expect(page).toHaveURL(/#album\/[^/]+$/);
  await expect(
    page.getByRole('heading', { name: 'Small Hours', exact: true }),
  ).toBeVisible();
  await page.reload();
  await expect(page.getByRole('button', { name: 'Play album' })).toBeVisible();
  await page.screenshot({ path: info.outputPath('album.png'), fullPage: true });
  await page
    .locator('.entity-header')
    .getByRole('link', { name: 'The Morning', exact: true })
    .click();
  await expect(page).toHaveURL(/#artist\/[^/]+$/);
  await expect(
    page.getByRole('heading', { name: 'The Morning', exact: true }),
  ).toBeVisible();
});

test('playlist create, add, edit, remove and delete work through Connect', async ({
  page,
}) => {
  await login(page);
  await page
    .getByRole('button', { name: 'Add First Light to playlist' })
    .click();
  const dialog = page.getByRole('dialog', { name: 'Add to playlist' });
  await dialog.getByPlaceholder('Playlist name').fill('Browser mix');
  await dialog.getByRole('button', { name: 'Create & add' }).click();
  await expect(dialog).not.toBeVisible();
  await page
    .getByRole('navigation')
    .getByRole('link', { name: 'Playlists', exact: true })
    .click();
  await page.getByRole('button', { name: /Browser mix.*1 song/ }).click();
  await expect(
    page.getByRole('button', { name: 'First Light', exact: true }),
  ).toBeVisible();
  await page
    .getByRole('textbox', { name: 'Playlist name', exact: true })
    .fill('Evening mix');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(
    page.getByRole('heading', { name: 'Evening mix', exact: true }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Remove First Light' }).click();
  await expect(
    page.getByRole('button', { name: 'First Light', exact: true }),
  ).toHaveCount(0);
  await page
    .getByRole('button', { name: 'Delete playlist', exact: true })
    .click();
  await page
    .getByRole('dialog')
    .getByRole('button', { name: 'Delete playlist', exact: true })
    .click();
  await expect(
    page.getByRole('heading', { name: 'No playlists yet' }),
  ).toBeVisible();
});

test('a download in another tab updates Discover through the shared stream', async ({
  page,
}) => {
  const subscription = page.waitForResponse(
    (response) =>
      response.url().endsWith('/wavelength.v1.RealtimeService/Subscribe') &&
      response.ok(),
  );
  await login(page);
  await subscription;
  await page
    .getByRole('navigation')
    .getByRole('link', { name: 'Discover', exact: true })
    .click();
  await page
    .getByRole('textbox', { name: 'Search for new music' })
    .fill('Streamed Artist');
  await page.getByRole('button', { name: 'Search', exact: true }).click();
  await expect(
    page.getByRole('checkbox', { name: 'Select Streamed Tone' }),
  ).toBeEnabled();
  const downloader = await page.context().newPage();
  try {
    await downloader.goto('/#discover');
    await downloader
      .getByRole('textbox', { name: 'Search for new music' })
      .fill('Streamed Artist');
    await downloader
      .getByRole('button', { name: 'Search', exact: true })
      .click();
    await downloader
      .getByRole('checkbox', { name: 'Select Streamed Tone' })
      .check();
    await downloader.getByRole('button', { name: 'Review download' }).click();
    await downloader
      .getByRole('dialog', { name: 'Review download' })
      .getByRole('button', { name: 'Download 1 song' })
      .click();
    await expect(
      downloader.getByRole('heading', { name: 'Download complete' }),
    ).toBeVisible({ timeout: 10000 });
    await expect(
      page.getByRole('checkbox', { name: 'Select Streamed Tone' }),
    ).toBeDisabled();
    await expect(page.locator('.discovery-track')).toContainText('In library');
    await downloader.locator('.track-link').click();
    await expect(
      downloader.getByRole('heading', { name: 'Streamed Album', exact: true }),
    ).toBeVisible();
  } finally {
    await downloader.close();
  }
});

test('reviewed downloads enter the library and selected playlist and can be played', async ({
  page,
}, info) => {
  await login(page);
  await page
    .getByRole('navigation')
    .getByRole('link', { name: 'Playlists', exact: true })
    .click();
  await page.getByRole('button', { name: 'New playlist', exact: true }).click();
  const create = page.getByRole('dialog', {
    name: 'New playlist',
    exact: true,
  });
  await create
    .getByRole('textbox', { name: 'Playlist name' })
    .fill('Downloads mix');
  await create
    .getByRole('button', { name: 'Create playlist', exact: true })
    .click();
  await expect(create).not.toBeVisible();
  await page
    .getByRole('navigation')
    .getByRole('link', { name: 'Discover', exact: true })
    .click();
  await page
    .getByRole('textbox', { name: 'Search for new music' })
    .fill('Imported Artist');
  await page.getByRole('button', { name: 'Search', exact: true }).click();
  await page.getByRole('checkbox', { name: 'Select Imported Tone' }).check();
  await page.getByRole('button', { name: 'Review download' }).click();
  await page
    .getByRole('button', { name: 'Choose recording for Imported Tone' })
    .click();
  await page.getByRole('button', { name: /99.*Imported Tone/ }).click();
  const review = page.getByRole('dialog', { name: 'Review download' });
  await review
    .getByRole('combobox', { name: 'Download playlist' })
    .selectOption({ label: 'Downloads mix' });
  await review.getByRole('button', { name: 'Download 1 song' }).click();
  await expect(page).toHaveURL(/#imports\/[^/]+$/);
  await expect(
    page.getByRole('heading', { name: 'Download complete' }),
  ).toBeVisible({ timeout: 10000 });
  await page.screenshot({
    path: info.outputPath('download.png'),
    fullPage: true,
  });
  await page.getByRole('button', { name: 'Play downloaded' }).click();
  await expect(page.locator('.now-playing')).toContainText('Imported Tone');
  await page
    .getByRole('navigation')
    .getByRole('link', { name: 'Playlists', exact: true })
    .click();
  await page.getByRole('button', { name: /Downloads mix.*1 song/ }).click();
  await expect(
    page.getByRole('button', { name: 'Imported Tone', exact: true }),
  ).toBeVisible();
});

test('global search recognizes an imported provider reference and plays it without a second job', async ({
  page,
}, info) => {
  await login(page);
  const writes: string[] = [];
  page.on('request', (request) => {
    if (request.url().endsWith('/CreateImport')) writes.push(request.url());
  });
  await page.keyboard.press('Control+k');
  const dialog = page.getByRole('dialog', {
    name: 'Search music',
    exact: true,
  });
  await dialog.getByRole('combobox').fill('Imported');
  const playable = dialog.getByRole('option', {
    name: /Imported Tone.*In library.*Play/,
  });
  await expect(playable).toHaveCount(2);
  await expect(
    dialog.getByRole('option', {
      name: /Imported Tone.*Not downloaded.*Download/,
    }),
  ).toHaveCount(0);
  await page.screenshot({ path: info.outputPath('search.png') });
  await playable.last().click();
  await expect(page.locator('.now-playing')).toContainText('Imported Tone');
  expect(writes).toEqual([]);
});
