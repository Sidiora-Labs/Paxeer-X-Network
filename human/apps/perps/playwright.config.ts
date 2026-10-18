import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./tests/browser",
  timeout: 90_000,
  expect: { timeout: 20_000 },
  workers: 1,
  fullyParallel: false,
  reporter: [
    ["list"],
    ["html", { outputFolder: "artifacts/playwright", open: "never" }],
  ],
  outputDir: "artifacts/test-results",
  use: {
    baseURL: process.env.PAXEER_NETWORK_BASE_URL || "http://localhost:3000",
    viewport: { width: 1920, height: 1080 },
    trace: "off",
    screenshot: "only-on-failure",
  },
});
