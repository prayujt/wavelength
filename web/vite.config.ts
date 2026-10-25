import { defineConfig, loadEnv } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');
  const target = env.WAVELENGTH_BACKEND_URL || 'http://127.0.0.1:8080';
  const proxy = Object.fromEntries(
    [
      '/auth/',
      '/oauth/',
      '/.well-known/',
      '/wavelength.v1.',
      '/media/',
      '/play/',
      '^/s/[^/]+/',
    ].map((path) => [path, { target, changeOrigin: false }]),
  );
  return {
    plugins: [svelte()],
    server: { port: 5173, strictPort: true, proxy },
  };
});
