import { defineConfig, devices } from '@playwright/test';

// Chromium only, headless. Each test starts its own server on a free port, so files can run in
// parallel, but timing checks need a quiet machine, so the worker count stays low.
export default defineConfig({
  testDir: './e2e',
  globalSetup: './e2e/global-setup.ts',
  workers: 2,
  timeout: 30_000,
  forbidOnly: true,
  retries: 0,
  reporter: 'line',
  use: { ...devices['Desktop Chrome'], headless: true, trace: 'retain-on-failure' },
});
