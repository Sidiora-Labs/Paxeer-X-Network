import { defineConfig } from 'vitest/config';
import path from 'node:path';

const walletSdk = path.resolve(__dirname, '../../wallet/sdk/src/index.ts');
const layerxSdk = path.resolve(__dirname, '../../../agent/sdk/typescript/src/index.ts');

export default defineConfig({
  test: {
    environment: 'node',
    globals: true,
    include: ['src/**/*.test.ts', 'src/**/*.test.tsx'],
  },
  resolve: {
    alias: {
      '@paxeer/wallet': walletSdk,
      '@sidiora/layerx-sdk': layerxSdk,
      '@': path.resolve(__dirname, 'src'),
    },
    dedupe: [
      'viem',
      '@supabase/supabase-js',
      '@noble/curves',
      '@noble/hashes',
      '@scure/bip32',
      '@scure/bip39',
      'ws',
      'react',
    ],
  },
});
