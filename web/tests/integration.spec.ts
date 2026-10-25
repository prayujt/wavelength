import { expect, test, type Page } from '@playwright/test';

async function login(page: Page) {
  await page.goto('/');
  await page.getByRole('link', { name: 'Sign in', exact: true }).click();
  await expect(
    page.getByRole('button', { name: 'First Light', exact: true }),
  ).toBeVisible();
  await expect(page.locator('.account')).toContainText('Test Listener');
}

test('OIDC login returns to the app, Connect reads succeed, and cookie playback survives reload', async ({
  page,
}) => {
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
  const media = page.waitForResponse(
    (response) => response.url().includes('/media/tracks/') && response.ok(),
  );
  await page
    .getByRole('button', { name: 'Play First Light', exact: true })
    .click();
  await media;
  await expect(page.locator('.now-playing')).toContainText('First Light');
  await expect(
    page.getByRole('button', { name: 'Pause', exact: true }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Pause', exact: true }).click();
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
    page.getByRole('heading', { name: 'A mix with your name on it' }),
  ).toBeVisible();
});

test('discovery selects a source and publishes a playable library track', async ({
  page,
}) => {
  await login(page);
  await page
    .getByRole('navigation')
    .getByRole('link', { name: 'Discover', exact: true })
    .click();
  await page
    .getByRole('textbox', { name: 'Search for new music' })
    .fill('Imported Artist');
  await page.getByRole('button', { name: 'Search', exact: true }).click();
  await page
    .getByRole('button', { name: 'Choose recording for Imported Tone' })
    .click();
  await page.getByRole('button', { name: /99.*Imported Tone/ }).click();
  await page
    .getByRole('button', { name: 'Add to library', exact: true })
    .click();
  await expect(
    page.getByRole('button', { name: /Imported Tone.*completed/ }),
  ).toBeVisible({ timeout: 10000 });
  await page
    .getByRole('navigation')
    .getByRole('link', { name: 'Your library', exact: true })
    .click();
  await expect(
    page.getByRole('button', { name: 'Imported Tone', exact: true }),
  ).toBeVisible();
});
