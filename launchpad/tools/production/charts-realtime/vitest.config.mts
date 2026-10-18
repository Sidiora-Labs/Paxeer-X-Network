import { defineConfig } from '../../../client/apps/web/node_modules/vitest/dist/config.js';
import { fileURLToPath } from 'node:url';
export default defineConfig({
  cacheDir: fileURLToPath(new URL('../../../evidence/production-system/L12/phase1/.vite', import.meta.url)),
  resolve: { alias: { '@': fileURLToPath(new URL('../../../client/apps/web/src', import.meta.url)) } },
  test: {
    cache: false,
    environment: 'node',
    fileParallelism: false,
    include: ['src/widgets/trading/chart-datafeed*.test.ts'],
  },
});
