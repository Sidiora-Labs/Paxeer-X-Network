import { defineConfig } from '@playwright/test';
import { APP_ENV } from './e2e/environment';

export default defineConfig({
  testDir: './e2e',
  testMatch: '**/*.spec.ts',
  globalSetup: './e2e/global-setup.ts',
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
    env: APP_ENV,
  },
});
