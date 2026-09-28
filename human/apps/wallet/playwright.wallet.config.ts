import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests/wallet-browser',
  testMatch: '**/*.spec.ts',
  timeout: 60_000,
  fullyParallel: false,
  workers: 1,
  use: {
    baseURL: 'http://127.0.0.1:4173',
    browserName: 'chromium',
    headless: true,
  },
  webServer: {
    command:
      'npx vite --config vite.wallet.config.ts --host 127.0.0.1 --port 4173',
    url: 'http://127.0.0.1:4173/tests/wallet-browser/harness.html',
    reuseExistingServer: false,
    timeout: 60_000,
  },
});
