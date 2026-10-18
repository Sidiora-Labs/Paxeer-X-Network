import { createRequire } from 'node:module';
import { fileURLToPath, pathToFileURL } from 'node:url';
const webRequire = createRequire(new URL('../../../apps/web/package.json', import.meta.url));
const { defineConfig } = await import(pathToFileURL(webRequire.resolve('vitest/config')).href);
export default defineConfig({
  resolve: { alias: [
    { find: '@', replacement: fileURLToPath(new URL('../../../apps/web/src', import.meta.url)) },
    { find: /^react$/, replacement: webRequire.resolve('react') },
    { find: 'react/jsx-runtime', replacement: webRequire.resolve('react/jsx-runtime') },
    { find: 'react-dom/client', replacement: webRequire.resolve('react-dom/client') },
    { find: /^vitest$/, replacement: webRequire.resolve('vitest') },
  ] },
  test: { environment: 'jsdom', include: [fileURLToPath(new URL('./browser-lifecycle.test.tsx', import.meta.url))], testTimeout: 60000, hookTimeout: 60000, fileParallelism: false },
});
