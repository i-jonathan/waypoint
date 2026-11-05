import { readdirSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { defineConfig, type Plugin } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { svelteTesting } from '@testing-library/svelte/vite';

const outDir = '../internal/http/dist';
const API = 'http://127.0.0.1:4747';
const API_HOST = '127.0.0.1:4747';

// The output folder also holds the committed placeholder.html, which lets the
// Go server build before the UI exists. So the folder is not emptied wholesale
// (emptyOutDir: false); this clears everything else before each build.
function cleanOutDir(): Plugin {
  let dir = '';
  return {
    name: 'waypoint-clean-out-dir',
    apply: 'build',
    configResolved(config) {
      dir = join(config.root, config.build.outDir);
    },
    buildStart() {
      for (const name of readdirSync(dir)) {
        if (name !== 'placeholder.html') rmSync(join(dir, name), { recursive: true, force: true });
      }
    },
  };
}

export default defineConfig({
  plugins: [svelte(), svelteTesting(), cleanOutDir()],
  build: { outDir, emptyOutDir: false },
  server: {
    port: 5173,
    proxy: {
      // The API only answers its own host and origin, so the proxy presents
      // itself as the Go server's own page. Server-sent events pass straight
      // through: no websocket upgrade, and responses are not buffered.
      '/api': {
        target: API,
        changeOrigin: true,
        ws: false,
        headers: { Host: API_HOST, 'X-Waypoint': '1' },
        configure(proxy) {
          proxy.on('proxyReq', (proxyReq) => {
            if (proxyReq.getHeader('origin')) proxyReq.setHeader('Origin', API);
          });
          // Node holds response headers back until the first body byte, and an
          // idle event stream sends none for up to 25s, so EventSource would
          // not open. Send the headers as soon as they are set.
          proxy.on('proxyRes', (proxyRes, _req, res) => {
            if (String(proxyRes.headers['content-type']).startsWith('text/event-stream')) {
              setImmediate(() => res.flushHeaders());
            }
          });
        },
      },
    },
  },
  test: {
    include: ['tests/**/*.test.ts'],
    setupFiles: ['tests/setup-dom.ts'],
  },
});
