import { chromium, devices } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { spawn } from 'node:child_process';
const base = 'http://127.0.0.1:4174';
let server;
try {
  await fetch(base);
} catch {
  server = spawn(process.execPath, ['scripts/serve.mjs'], { stdio: 'ignore' });
  for (let i = 0; i < 50; i++) {
    try {
      await fetch(base);
      break;
    } catch {
      await new Promise((r) => setTimeout(r, 100));
    }
  }
}
mkdirSync('docs/screenshots', { recursive: true });
const browser = await chromium.launch({ headless: true });
const shots = {
  welcome: async () => {},
  home: async () => {},
  library: async (page) => {
    await page.getByRole('tab', { name: 'Library', exact: true }).click();
  },
  discover: async (page) => {
    await page.getByRole('tab', { name: 'Discover', exact: true }).click();
    await page.getByRole('textbox', { name: 'Artists, songs, albums…' }).fill('June Atlas');
    await page.getByRole('button', { name: 'Discover album Night swimming' }).waitFor();
  },
  player: async (page) => {
    await page.getByRole('button', { name: 'Open album The quiet between', exact: true }).click();
    await page.getByRole('button', { name: 'Play', exact: true }).click();
    await page.getByRole('button', { name: 'Go back', exact: true }).last().click();
    await page.getByRole('button', { name: 'Open now playing' }).click();
    await page.getByRole('button', { name: 'Pause song' }).click();
  },
  activity: async (page) => {
    await page.getByRole('button', { name: 'Downloads', exact: true }).click();
    await page.getByRole('button', { name: 'Choose a recording', exact: true }).waitFor();
  },
};
try {
  for (const [name, action] of Object.entries(shots)) {
    const context = await browser.newContext({
      ...devices['iPhone 13'],
      viewport: { width: 390, height: 844 },
      deviceScaleFactor: 2,
      reducedMotion: 'reduce',
    });
    const page = await context.newPage();
    await page.goto(base);
    if (name !== 'welcome') {
      await page.getByRole('button', { name: 'Explore the preview' }).click();
      await page.getByRole('heading', { name: 'Find your rhythm.' }).waitFor();
    } else await page.getByRole('button', { name: 'Find your wavelength' }).waitFor();
    await action(page);
    await page.locator('body').click({ position: { x: 16, y: 16 } });
    await page.waitForTimeout(650);
    await page.screenshot({ path: `docs/screenshots/${name}.png`, animations: 'disabled' });
    await context.close();
    console.log('Captured ' + name);
  }
} finally {
  await browser.close();
  server?.kill('SIGTERM');
}
