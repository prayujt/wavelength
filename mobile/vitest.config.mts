import { defineConfig } from 'vitest/config';
export default defineConfig({
  resolve: {
    alias: { 'expo/fetch': new URL('./tests/unit/native-fetch.ts', import.meta.url).pathname },
  },
  test: { include: ['tests/unit/**/*.test.ts'], environment: 'node' },
});
