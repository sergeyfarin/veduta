import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// Separate from vite.config.ts on purpose: test-only settings (jsdom, setup files) never leak
// into the dev server or the production build.
export default defineConfig({
  plugins: [svelte()],
  // Without this, Vite resolves svelte's package exports using its default (server/node)
  // condition even under jsdom, and mounting a component fails with "mount(...) is not
  // available on the server" - Svelte still thinks it is being asked to run in SSR mode.
  resolve: { conditions: ['browser'] },
  test: {
    environment: 'jsdom',
    setupFiles: ['./vitest-setup.ts'],
    include: ['src/**/*.{test,spec}.{js,ts}']
  }
});
