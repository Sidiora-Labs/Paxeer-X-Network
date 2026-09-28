import { existsSync, readFileSync, readdirSync } from 'node:fs';
import path from 'node:path';
import { describe, expect, it } from 'vitest';
import { buildContentSecurityPolicy } from '@/lib/security/csp';
import { OFFLINE_URL } from './caching';

const APP = path.resolve(__dirname, '..', '..');
const THIRD_PARTY = /progressier/i;

function read(relative: string): string {
    return readFileSync(path.join(APP, relative), 'utf8');
}

describe('self-hosted application policy', () => {
    it('serves the manifest only from its own origin and allows no third-party PWA service', () => {
        const policy = buildContentSecurityPolicy('bm9uY2U=');
        expect(policy).toContain("manifest-src 'self';");
        expect(policy).not.toMatch(THIRD_PARTY);
    });

    it('keeps the third-party PWA service out of the proxy, headers, layout, security sources and public files', () => {
        const security = readdirSync(path.join(APP, 'src', 'lib', 'security'))
            .filter((name) => name.endsWith('.ts'))
            .map((name) => path.join('src', 'lib', 'security', name));
        for (const file of ['src/proxy.ts', 'next.config.mjs', 'src/app/layout.tsx', 'public/manifest.json', ...security]) {
            expect(read(file), file).not.toMatch(THIRD_PARTY);
        }
        expect(existsSync(path.join(APP, 'public', 'progressier.js'))).toBe(false);
    });

    it('ships no native wrapper', () => {
        for (const entry of ['capacitor.config.ts', 'ios', 'android', 'src/lib/capacitor.ts']) {
            expect(existsSync(path.join(APP, entry)), entry).toBe(false);
        }
        const manifest = JSON.parse(read('package.json')) as {
            dependencies?: Record<string, string>;
            devDependencies?: Record<string, string>;
            scripts?: Record<string, string>;
        };
        const names = [...Object.keys(manifest.dependencies ?? {}), ...Object.keys(manifest.devDependencies ?? {})];
        expect(names.filter((name) => name.startsWith('@capacitor/'))).toEqual([]);
        expect(Object.values(manifest.scripts ?? {}).join('\n')).not.toMatch(/\bcap (sync|run|open)\b/);
    });

    it('builds the worker before the application and serves an offline page', () => {
        const manifest = JSON.parse(read('package.json')) as { scripts: Record<string, string> };
        expect(manifest.scripts.build.startsWith('node scripts/build-pwa.mjs && ')).toBe(true);
        expect(existsSync(path.join(APP, 'src', 'app', OFFLINE_URL.slice(1), 'page.tsx'))).toBe(true);
        expect(read('.gitignore').split('\n')).toContain('/public/sw.js');
    });
});
