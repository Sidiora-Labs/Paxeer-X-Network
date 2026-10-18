import { defineConfig, mergeConfig } from 'vitest/config';
import { resolve } from 'node:path';
import baseline from '../../../vitest.config';
// node_modules may be an immutable shared dependency symlink. Keep all caches private.
export default mergeConfig(baseline, defineConfig({
  cacheDir: process.env.L25_VITEST_CACHE_DIR ?? resolve(process.cwd(), '../../../evidence/production-system/L25/phase1/vite-cache'),
  test: { maxWorkers: 1, minWorkers: 1 },
}));
