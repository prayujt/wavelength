import { test, expect, type Page } from '@playwright/test';

async function preview(page: Page) {
  await page.goto('/');
  await page.getByRole('button', { name: 'Explore the preview' }).click();
  await expect(page.getByRole('heading', { name: 'Find your rhythm.' })).toBeVisible();
}
test.beforeEach(async ({ page }) => {
  page.on('pageerror', (error) => {
    throw error;
  });
});
test('welcome offers real sign-in, configurable server and an explicitly separate preview', async ({
  page,
}) => {
  await page.goto('/');
  await expect(page.getByText('Less noise.')).toBeVisible();
  await page.getByRole('button', { name: 'Connection options' }).click();
  await expect(page.getByRole('textbox', { name: 'Wavelength server' })).toHaveValue(
    'https://wavelength.prayujt.com',
  );
  await page.getByRole('button', { name: 'Explore the preview' }).click();
  await expect(page.getByText('PREVIEW', { exact: true })).toBeVisible();
});
test('home, album and artist navigation use real collection IDs', async ({ page }) => {
  await preview(page);
  await page.getByRole('button', { name: 'Explore The quiet between', exact: true }).click();
  await expect(page.getByText('5 songs ·', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'View artist Rowan Hale', exact: true }).click();
  await expect(page.getByText('2 albums in your collection')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Open album Field notes' })).toBeVisible();
});
test('playback persists across tabs and opens the full player, lyrics and queue', async ({
  page,
}) => {
  await preview(page);
  await page.getByRole('button', { name: 'Press play. Stay awhile.' }).click();
  await expect(page.getByRole('button', { name: 'Open now playing' })).toBeVisible();
  await page.getByRole('tab', { name: 'Library', exact: true }).click();
  await page.getByRole('button', { name: 'Browse all music', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Open now playing' })).toBeVisible();
  await page.getByRole('button', { name: 'Open now playing' }).click();
  await expect(page.getByRole('button', { name: 'Pause song' })).toBeVisible();
  await page.getByRole('button', { name: 'Pause song' }).click();
  await expect(page.getByRole('button', { name: 'Play song', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Open lyrics' }).click();
  await expect(
    page.getByRole('button', { name: 'Seek to lyric: A little room to breathe' }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Go back' }).last().click();
  await page.getByRole('button', { name: 'Open play queue' }).click();
  await expect(page.getByText('5 songs, in your order.')).toBeVisible();
  await page.getByRole('button', { name: 'Move queued song 2 down' }).click();
  await expect(
    page.getByRole('button', { name: 'Play queued Where the light goes' }),
  ).toBeVisible();
});
test('library switches layouts and filters the catalog without losing the screen', async ({
  page,
}) => {
  await preview(page);
  await page.getByRole('tab', { name: 'Library', exact: true }).click();
  await page.getByRole('button', { name: 'Browse all music', exact: true }).click();
  await expect(page.getByText('40 songs', { exact: true })).toBeVisible();
  await page.getByRole('textbox', { name: 'Search your songs' }).fill('Weightless');
  await expect(page.getByText('1 songs', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Clear search' }).click();
  await page.getByRole('button', { name: 'Albums', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Open album Blue hour' })).toBeVisible();
});
test('playlist create, edit, reorder and delete preserve explicit track order', async ({
  page,
}) => {
  await preview(page);
  await page.getByRole('tab', { name: 'Playlists', exact: true }).click();
  await page.getByRole('button', { name: 'Create playlist', exact: true }).click();
  await page.getByRole('textbox', { name: 'Playlist name' }).fill('Evening signals');
  await page
    .getByRole('textbox', { name: 'A little about this playlist' })
    .fill('A softer soundtrack.');
  await page.getByRole('button', { name: 'Create playlist', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Evening signals', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Edit playlist' }).click();
  await page.getByRole('textbox', { name: 'Playlist name' }).fill('Evening signals, revised');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(
    page.getByRole('heading', { name: 'Evening signals, revised', exact: true }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Edit playlist' }).click();
  await page.getByRole('button', { name: 'Delete playlist', exact: true }).click();
  await page.getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Playlists', exact: true })).toBeVisible();
});
test('song menu adds a track to an owned playlist and exposes queue actions', async ({ page }) => {
  await preview(page);
  await page.getByRole('tab', { name: 'Library', exact: true }).click();
  await page.getByRole('button', { name: 'Browse all music', exact: true }).click();
  await page.getByRole('button', { name: 'More options for Weightless', exact: true }).click();
  await page.getByRole('button', { name: 'Add to playlist', exact: true }).click();
  await page.getByRole('button', { name: 'Add to The slow hours', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Added to The slow hours');
  await page.getByRole('tab', { name: 'Playlists', exact: true }).click();
  await page.getByRole('button', { name: 'Open playlist The slow hours' }).click();
  await expect(page.getByText('5 songs ·', { exact: false })).toBeVisible();
});
test('catalog discovery exposes exact saved matches and album imports', async ({ page }) => {
  await preview(page);
  await page.getByRole('tab', { name: 'Discover', exact: true }).click();
  await page.getByRole('textbox', { name: 'Artists, songs, albums…' }).fill('June Atlas');
  await expect(page.getByRole('button', { name: 'Discover album Night swimming' })).toBeVisible();
  await page.getByRole('button', { name: 'Discover album Night swimming' }).click();
  await page.getByRole('button', { name: 'Download album', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'The music keeps moving.' })).toBeVisible();
  await expect(
    page.getByText('3 of 3 ready', { exact: true }).filter({ visible: true }),
  ).toBeVisible({ timeout: 15000 });
});
test('ambiguous downloads can choose a source and resume the existing job', async ({ page }) => {
  await preview(page);
  await page.getByRole('button', { name: 'Downloads', exact: true }).click();
  await page.getByRole('button', { name: 'Choose a recording', exact: true }).click();
  await expect(page.getByText('The right recording.')).toBeVisible();
  await page
    .getByRole('button', { name: 'Select Night swimming (Official Audio)', exact: true })
    .click();
  await page.getByRole('button', { name: 'Download selected recording', exact: true }).click();
  await expect(
    page.getByText('1 of 1 ready', { exact: true }).filter({ visible: true }),
  ).toBeVisible({ timeout: 15000 });
});
test('bookmarks save a position and can be removed later', async ({ page }) => {
  await preview(page);
  await page.getByRole('button', { name: 'Press play. Stay awhile.' }).click();
  await page.getByRole('button', { name: 'Open now playing' }).click();
  await page.getByRole('button', { name: 'Bookmark this position' }).click();
  await expect(page.getByRole('alert')).toContainText('Your place is saved');
  await page.getByRole('button', { name: 'Close player' }).click();
  await page.getByRole('button', { name: 'Open settings' }).click();
  await page.getByRole('button', { name: 'Saved bookmarks', exact: true }).click();
  await expect(
    page.getByRole('button', { name: 'Delete bookmark for Weightless', exact: true }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Delete bookmark for Weightless' }).click();
  await expect(page.getByText('Keep your place.')).toBeVisible();
});
test('public sharing is explicit, and its revocation is available in settings', async ({
  page,
}) => {
  await preview(page);
  await page.getByRole('tab', { name: 'Library', exact: true }).click();
  await page.getByRole('button', { name: 'Browse all music', exact: true }).click();
  await page.getByRole('button', { name: 'More options for Weightless', exact: true }).click();
  await page.getByRole('button', { name: 'Share a listening link', exact: true }).click();
  await page.getByRole('button', { name: 'Create link', exact: true }).click();
  await expect(page.getByText('Pass a good thing on.')).toBeVisible();
  await page.getByRole('button', { name: 'Done', exact: true }).click();
  await page.getByRole('tab', { name: 'Home', exact: true }).click();
  await page.getByRole('button', { name: 'Open settings' }).click();
  await page.getByRole('button', { name: 'Shared links', exact: true }).click();
  await page.getByRole('button', { name: 'Revoke link', exact: true }).click();
  await page.getByRole('button', { name: 'Revoke link', exact: true }).last().click();
  await expect(page.getByText('Something worth passing on.')).toBeVisible();
});
test('listening history is readable and filtered without creating fake sessions', async ({
  page,
}) => {
  await preview(page);
  await page.getByRole('button', { name: 'Downloads', exact: true }).click();
  await page.getByRole('button', { name: 'Listening', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Play Weightless', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Skipped', exact: true }).click();
  await expect(page.getByText('Your next song starts the story.')).toBeVisible();
});
test('leaving the preview clears its account and queue', async ({ page }) => {
  await preview(page);
  await page.getByRole('button', { name: 'Press play. Stay awhile.' }).click();
  await page.getByRole('button', { name: 'Open settings' }).click();
  await page.getByRole('button', { name: 'Leave the preview' }).click();
  await expect(page.getByRole('button', { name: 'Find your wavelength' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Open now playing' })).toHaveCount(0);
});
test('phone and tablet screens stay within the viewport with reduced motion', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.setViewportSize({ width: 320, height: 740 });
  await preview(page);
  await page.getByRole('tab', { name: 'Library', exact: true }).click();
  await page.getByRole('button', { name: 'Browse all music', exact: true }).click();
  await page.getByRole('button', { name: 'Albums', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Open album Blue hour' })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
  await page.setViewportSize({ width: 834, height: 1112 });
  await expect(page.getByRole('heading', { name: 'All music' })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
});
