import { fileURLToPath } from 'node:url';
const config = {
  cacheDir: '.cache/L37/vite',
  esbuild: { jsx: 'automatic' },
  resolve: { alias: {
    react: fileURLToPath(new URL('../../../node_modules/react', import.meta.url)),
    'react-dom': fileURLToPath(new URL('../../../node_modules/react-dom', import.meta.url)),
    vitest: fileURLToPath(new URL('../../../node_modules/vitest', import.meta.url)),
  } },
  test: { environment: 'jsdom', include: ['client/apps/web/src/widgets/community-interactions/*.test.tsx'], fileParallelism: false },
};
export default config;
