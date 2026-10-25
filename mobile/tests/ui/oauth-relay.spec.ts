import { expect, test } from '@playwright/test';
function relay(extra: Record<string, string> = {}) {
  return (
    '/mobile-auth.html?' +
    new URLSearchParams({
      return_uri: 'exp://192.168.1.4:8081/--/oauth/callback',
      state: 'test-state',
      iss: 'http://127.0.0.1:4174',
      code: 'one-use-test-code',
      ...extra,
    })
  );
}
test('Expo Go receives a state-bound code and the browser URL is scrubbed', async ({ page }) => {
  await page.goto(relay());
  const link = page.getByRole('link', { name: 'Return to Wavelength' });
  await expect(link).toBeVisible();
  const url = new URL((await link.getAttribute('href'))!);
  expect(url.protocol).toBe('exp:');
  expect(url.searchParams.get('state')).toBe('test-state');
  expect(url.searchParams.get('code')).toBe('one-use-test-code');
  expect(await page.evaluate(() => location.search)).toBe('');
});
test('standalone builds can return through the app-specific scheme', async ({ page }) => {
  await page.goto(relay({ return_uri: 'wavelength://oauth/callback' }));
  await expect(page.getByRole('link', { name: 'Return to Wavelength' })).toHaveAttribute(
    'href',
    /^wavelength:\/\/oauth\/callback\?/,
  );
});
test('denial returns a safe error instead of claiming authentication succeeded', async ({
  page,
}) => {
  const url = new URL('http://127.0.0.1:4174' + relay({ error: 'access_denied' }));
  url.searchParams.delete('code');
  await page.goto(url.toString());
  await expect(page.getByText('No problem. Try again anytime.')).toBeVisible();
  await expect(page.getByRole('link', { name: 'Return to Wavelength' })).toHaveAttribute(
    'href',
    /error=access_denied/,
  );
});
for (const target of [
  'https://evil.example/callback',
  'javascript:alert(1)',
  'exp://user:password@host/--/oauth/callback',
  'wavelength://other-host/callback',
  'exp://host/--/oauth/callback?code=preexisting',
])
  test(`rejects an unsafe return target ${target}`, async ({ page }) => {
    await page.goto(relay({ return_uri: target }));
    await expect(page.getByRole('link', { name: 'Return to Wavelength' })).toHaveCount(0);
    await expect(page.getByText('Start again from the app.')).toBeVisible();
  });
test('rejects incorrect issuers and duplicate state/code parameters', async ({ page }) => {
  for (const path of [
    relay({ iss: 'https://evil.example' }),
    relay() + '&state=other',
    relay() + '&code=other',
  ]) {
    await page.goto(path);
    await expect(page.getByRole('link', { name: 'Return to Wavelength' })).toHaveCount(0);
  }
});
