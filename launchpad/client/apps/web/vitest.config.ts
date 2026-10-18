import { defineConfig } from 'vitest/config';
import { fileURLToPath } from 'node:url';

export default defineConfig({
  esbuild: { jsx: 'automatic', jsxImportSource: 'react' },
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  test: {
    environment: 'node',
    // Wallet suites share one private EVM and exercise chain/account changes.
    fileParallelism: false,
    include: ['src/**/*.test.ts', 'src/**/*.test.tsx'],
    globals: false,
    // The four required backend origins have no fallback under any NODE_ENV
    // (core/backend-origins.ts); suites run against the same public Railway
    // values as client/apps/web/.env.local, and individual suites override them.
    env: {
      NEXT_PUBLIC_DATA_API_URL: 'https://api-production-e4bd.up.railway.app',
      NEXT_PUBLIC_GATEWAY_URL: 'https://gateway-production-800c.up.railway.app',
      NEXT_PUBLIC_METADATA_API_URL: 'https://gateway-production-800c.up.railway.app',
      NEXT_PUBLIC_PNL_API_URL: 'https://pnl-production-72f3.up.railway.app',
    },
    // @kindle/sdk dist validators read `.default` off CJS ajv runtime modules;
    // vitest's default interop unwraps it and breaks every SDK-importing suite.
    deps: { interopDefault: false },
  },
});
