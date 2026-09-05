import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// Plain Vite SPA, not SvelteKit: Go serves everything and embeds this build via embed.FS,
// so a router/prerender/adapter layer would buy nothing. See docs/01-architecture.md section 13.
export default defineConfig({
  plugins: [svelte()],
  build: {
    // One level below the embed root: //go:embed needs a committed file to match on a clean
    // clone, and Vite empties its outDir on every build - including dotfiles.
    outDir: 'build/app',
    emptyOutDir: true,
    target: 'es2022'
  },
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://127.0.0.1:8099', changeOrigin: false }
    }
  }
});
