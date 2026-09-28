import { defineConfig } from 'vite';

export default defineConfig({
  define: {
    'process.env': '{}',
  },
  resolve: {
    alias: {
      '@': new URL('./src', import.meta.url).pathname,
    },
  },
  optimizeDeps: {
    include: [
      '@scure/bip32',
      '@scure/bip39',
      'crypto-js',
      'ethers',
    ],
  },
  server: {
    watch: {
      ignored: ['**/test-results/**'],
    },
  },
});
