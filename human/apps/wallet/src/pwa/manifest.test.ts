import { existsSync, readFileSync } from 'node:fs';
import path from 'node:path';
import { describe, expect, it } from 'vitest';
import { themeCatalogue } from '@/theme/catalogue';
import { APPLE_TOUCH_ICON, FAVICON_ICON, ICON_FILES, MANIFEST_URL, buildManifest, serializeManifest } from './manifest';

const APP = path.resolve(__dirname, '..', '..');
const PUBLIC = path.join(APP, 'public');

function pngSize(file: string): { width: number; height: number } {
    const bytes = readFileSync(file);
    expect(bytes.subarray(0, 8).equals(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]))).toBe(true);
    expect(bytes.toString('latin1', 12, 16)).toBe('IHDR');
    return { width: bytes.readUInt32BE(16), height: bytes.readUInt32BE(20) };
}

describe('web app manifest', () => {
    const committed = readFileSync(path.join(PUBLIC, MANIFEST_URL.slice(1)), 'utf8');
    const manifest = JSON.parse(committed) as ReturnType<typeof buildManifest>;

    it('is the committed output of buildManifest', () => {
        expect(committed).toBe(serializeManifest());
        expect(manifest).toEqual(buildManifest());
    });

    it('carries every field an installable application needs', () => {
        expect(manifest.name).toBe('Paxeer Wallet');
        expect(manifest.short_name).toBe('Paxeer');
        expect(manifest.id).toBe('/');
        expect(manifest.start_url).toBe('/');
        expect(manifest.scope).toBe('/');
        expect(manifest.display).toBe('standalone');
        expect(manifest.prefer_related_applications).toBe(false);
        expect(manifest.description.length).toBeGreaterThan(0);
    });

    it('takes its theme and background colours from the theme tokens', () => {
        const base = themeCatalogue.themes.dark.colors.surface.base;
        expect(themeCatalogue.defaults.theme).toBe('dark');
        expect(manifest.theme_color).toBe(base);
        expect(manifest.background_color).toBe(base);
    });

    it('declares any and maskable icons at 192 and 512 pixels', () => {
        for (const size of ['192x192', '512x512']) {
            for (const purpose of ['any', 'maskable']) {
                expect(manifest.icons.filter((icon) => icon.sizes === size && icon.purpose === purpose)).toHaveLength(1);
            }
        }
        for (const icon of manifest.icons) {
            expect(icon.type).toBe('image/png');
            expect(icon.src.startsWith('/icons/app/')).toBe(true);
        }
    });

    it('references no origin other than its own', () => {
        expect(committed).not.toMatch(/https?:\/\//);
    });
});

describe('icon files', () => {
    it('exist under public at the size each one declares', () => {
        expect(ICON_FILES.length).toBe(6);
        for (const icon of ICON_FILES) {
            const file = path.join(PUBLIC, icon.src.slice(1));
            expect(existsSync(file), icon.src).toBe(true);
            expect(pngSize(file), icon.src).toEqual({ width: icon.size, height: icon.size });
        }
    });

    it('include the apple touch icon and favicon the layout links', () => {
        expect(APPLE_TOUCH_ICON.sizes).toBe('180x180');
        expect(FAVICON_ICON.sizes).toBe('32x32');
        const layout = readFileSync(path.join(APP, 'src', 'app', 'layout.tsx'), 'utf8');
        expect(layout).toContain('manifest: MANIFEST_URL');
        expect(layout).toContain('rel="apple-touch-icon" sizes={APPLE_TOUCH_ICON.sizes} href={APPLE_TOUCH_ICON.src}');
        expect(layout).toContain('<meta name="apple-mobile-web-app-capable" content="yes" />');
        expect(layout).toContain("statusBarStyle: 'black-translucent'");
    });
});
