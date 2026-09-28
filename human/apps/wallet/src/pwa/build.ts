import { createHash } from 'node:crypto';
import { mkdirSync, readFileSync, readdirSync, statSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { build } from 'vite';
import { PRECACHE_URLS, cacheNames, type CacheNames } from './caching';
import { networkOnlyBases, pwaNetworkEnv } from './config';
import { MANIFEST_URL, serializeManifest } from './manifest';

export interface PwaBuildOptions {
    readonly appDir: string;
    readonly outDir: string;
    readonly env: Record<string, string | undefined>;
}

export interface PwaBuildResult {
    readonly workerPath: string;
    readonly manifestPath: string;
    readonly version: string;
    readonly cacheNames: CacheNames;
    readonly networkOnly: readonly string[];
    readonly precache: readonly string[];
}

function sourceFiles(directory: string): string[] {
    return readdirSync(directory)
        .sort()
        .flatMap((entry) => {
            const full = path.join(directory, entry);
            if (statSync(full).isDirectory()) return sourceFiles(full);
            return /\.(?:tsx?|css)$/.test(entry) && !entry.includes('.test.') ? [full] : [];
        });
}

export function buildVersion(appDir: string, manifest: string, networkOnly: readonly string[]): string {
    const hash = createHash('sha256');
    const srcDir = path.join(appDir, 'src');
    for (const file of sourceFiles(srcDir)) {
        hash.update(path.relative(srcDir, file));
        hash.update('\0');
        hash.update(readFileSync(file));
        hash.update('\0');
    }
    hash.update(manifest);
    hash.update(JSON.stringify(networkOnly));
    hash.update(JSON.stringify(PRECACHE_URLS));
    return hash.digest('hex').slice(0, 16);
}

export async function buildPwa(options: PwaBuildOptions): Promise<PwaBuildResult> {
    const networkOnly = networkOnlyBases(pwaNetworkEnv(options.env));
    const manifest = serializeManifest();
    const version = buildVersion(options.appDir, manifest, networkOnly);
    mkdirSync(options.outDir, { recursive: true });
    const manifestPath = path.join(options.outDir, MANIFEST_URL.slice(1));
    writeFileSync(manifestPath, manifest);
    await build({
        configFile: false,
        root: options.appDir,
        publicDir: false,
        logLevel: 'silent',
        resolve: { alias: { '@': path.join(options.appDir, 'src') } },
        define: { __PAXEER_SW__: JSON.stringify({ version, networkOnly }) },
        build: {
            outDir: options.outDir,
            emptyOutDir: false,
            copyPublicDir: false,
            minify: true,
            target: 'es2020',
            write: true,
            lib: {
                entry: path.join(options.appDir, 'src', 'pwa', 'sw.ts'),
                formats: ['iife'],
                name: 'paxeerServiceWorker',
                fileName: () => 'sw.js',
            },
        },
    });
    return {
        workerPath: path.join(options.outDir, 'sw.js'),
        manifestPath,
        version,
        cacheNames: cacheNames(version),
        networkOnly,
        precache: PRECACHE_URLS,
    };
}
