import { fileURLToPath } from 'node:url';
export default {
  cacheDir: ".cache/L29/vite",
  esbuild: { jsx: 'automatic' },
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('../../web/src', import.meta.url)),
      'react': fileURLToPath(new URL('../../web/node_modules/react', import.meta.url)),
      'react-dom': fileURLToPath(new URL('../../web/node_modules/react-dom', import.meta.url)),
      'vitest': fileURLToPath(new URL('../../web/node_modules/vitest', import.meta.url)),
      '@tanstack/react-query': fileURLToPath(new URL('../../web/node_modules/@tanstack/react-query', import.meta.url)),
    }
  },
  test: { environment: 'jsdom', include: ['client/qualification/foundations/*.test.tsx'], fileParallelism: false },
};
