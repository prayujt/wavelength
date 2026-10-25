import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  testMatch: 'integration.spec.ts',
  workers: 1,
  timeout: 20000,
  use: {
    baseURL: process.env.WAVELENGTH_WEB_URL,
    browserName: 'chromium',
    trace: 'retain-on-failure',
  },
});
