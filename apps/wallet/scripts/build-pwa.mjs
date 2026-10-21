import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createServer } from 'vite';

const appDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');

const loader = await createServer({
    configFile: false,
    root: appDir,
    publicDir: false,
    logLevel: 'error',
    appType: 'custom',
    server: { middlewareMode: true, hmr: false, ws: false },
    optimizeDeps: { noDiscovery: true, include: [] },
    resolve: { alias: { '@': path.join(appDir, 'src') } },
});

try {
    const { buildPwa } = await loader.ssrLoadModule('/src/pwa/build.ts');
    const result = await buildPwa({ appDir, outDir: path.join(appDir, 'public'), env: process.env });
    console.log(
        `build-pwa: worker ${path.relative(appDir, result.workerPath)} version ${result.version}, ` +
            `manifest ${path.relative(appDir, result.manifestPath)}, ` +
            `${result.precache.length} precached, ${result.networkOnly.length} network-only bases`,
    );
} finally {
    await loader.close();
}
