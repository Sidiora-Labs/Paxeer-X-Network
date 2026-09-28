import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests/wallet-app',
  testMatch: '**/*.spec.ts',
  timeout: 120_000,
  fullyParallel: false,
  workers: 1,
  use: {
    baseURL: 'http://127.0.0.1:3099',
    browserName: 'chromium',
    headless: true,
  },
  webServer: {
    command: 'npx next dev --hostname 127.0.0.1 --port 3099',
    url: 'http://127.0.0.1:3099',
    reuseExistingServer: true,
    timeout: 180_000,
  },
});
