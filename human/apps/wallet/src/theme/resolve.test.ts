import { describe, expect, it } from 'vitest';
import { DEFAULT_SELECTION, SIZE_IDS, DENSITY_IDS, themeCatalogue } from './catalogue';
import { parseSelection, resolveTheme } from './resolve';
import type { ResolvedTheme, ThemeEnvironment } from './schema';

const DARK: ThemeEnvironment = { colorScheme: 'dark', reducedMotion: false };
const LIGHT: ThemeEnvironment = { colorScheme: 'light', reducedMotion: false };

function computedPx(resolved: ResolvedTheme, property: string): number {
    const value = resolved.properties[property];
    expect(value).toMatch(/rem$/);
    const root = parseFloat(resolved.properties['--root-font-size']) / 100;
    return parseFloat(value) * themeCatalogue.layout.rootPx * root;
}

describe('resolveTheme', () => {
    it('reproduces the current rendering under the default selection', () => {
        const resolved = resolveTheme(DEFAULT_SELECTION, DARK);
        expect(resolved.id).toBe('dark');
        expect(resolved.scheme).toBe('dark');
        expect(resolved.properties).toMatchObject({
            '--color-surface-base': '#141413',
            '--color-surface-raised': '#1c1c1a',
            '--color-text-primary': '#eee9e2',
            '--color-action-primary': '#2262d1',
            '--color-action-primary-muted': '#194796',
            '--pax-bg': 'var(--color-surface-base)',
            '--pax-accent': 'var(--color-action-primary)',
            '--font-size-md': '1rem',
            '--font-size-xl': '1.75rem',
            '--space-4': '1rem',
            '--density-control': '2.75rem',
            '--touch-target': '2.75rem',
            '--radius-lg': '1rem',
            '--root-font-size': '100%',
            '--font-sans': "'Paxeer Sans Rounded', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif",
            '--motion-normal': '200ms',
            '--motion-scale': '1',
        });
    });

    it('follows the system colour scheme only when the selection says system', () => {
        const system = { ...DEFAULT_SELECTION, theme: 'system' as const };
        expect(resolveTheme(system, DARK).id).toBe('dark');
        expect(resolveTheme(system, LIGHT).id).toBe('light');
        expect(resolveTheme(system, LIGHT).scheme).toBe('light');
        expect(resolveTheme(system, LIGHT).properties['--color-surface-base']).toBe(themeCatalogue.themes.light.colors.surface.base);
        expect(resolveTheme(system, LIGHT).properties['--color-action-primary-muted']).toBe(themeCatalogue.accents.blue.muted.light);
        expect(resolveTheme(DEFAULT_SELECTION, LIGHT).id).toBe('dark');
        expect(resolveTheme({ ...DEFAULT_SELECTION, theme: 'contrast-light' }, DARK).id).toBe('contrast-light');
    });

    it('scales the type scale, spacing and control heights together for every size preset', () => {
        const regular = resolveTheme(DEFAULT_SELECTION, DARK);
        for (const size of SIZE_IDS) {
            const resolved = resolveTheme({ ...DEFAULT_SELECTION, size }, DARK);
            const scale = themeCatalogue.sizes[size].scale;
            expect(resolved.properties['--font-scale']).toBe(String(scale));
            for (const property of ['--font-size-xs', '--font-size-md', '--font-size-xl', '--space-2', '--space-4', '--space-8', '--density-control']) {
                expect(computedPx(resolved, property) / computedPx(regular, property), `${size} ${property}`).toBeCloseTo(scale, 6);
            }
        }
        expect(computedPx(resolveTheme({ ...DEFAULT_SELECTION, size: 'large' }, DARK), '--font-size-md')).toBeCloseTo(18, 6);
        expect(computedPx(resolveTheme({ ...DEFAULT_SELECTION, size: 'compact' }, DARK), '--font-size-md')).toBeCloseTo(14, 6);
    });

    it('tightens spacing and controls under dense density, leaving the typography scale untouched', () => {
        const comfortable = resolveTheme(DEFAULT_SELECTION, DARK);
        const dense = resolveTheme({ ...DEFAULT_SELECTION, density: 'dense' }, DARK);
        const factor = themeCatalogue.densities.dense.factor;
        expect(computedPx(dense, '--space-4') / computedPx(comfortable, '--space-4')).toBeCloseTo(factor, 6);
        expect(computedPx(dense, '--density-control') / computedPx(comfortable, '--density-control')).toBeCloseTo(factor, 6);
        expect(dense.properties['--font-size-md']).toBe(comfortable.properties['--font-size-md']);
    });

    it('never lets a touch target fall below the minimum under any size and density', () => {
        for (const size of SIZE_IDS) {
            for (const density of DENSITY_IDS) {
                const resolved = resolveTheme({ ...DEFAULT_SELECTION, size, density }, DARK);
                expect(computedPx(resolved, '--touch-target')).toBeGreaterThanOrEqual(themeCatalogue.layout.touchTargetPx - 0.01);
            }
        }
    });

    it('stops motion when the system asks for reduced motion', () => {
        const reduced = resolveTheme(DEFAULT_SELECTION, { colorScheme: 'dark', reducedMotion: true });
        expect(reduced.reducedMotion).toBe(true);
        expect(reduced.properties['--motion-fast']).toBe('0ms');
        expect(reduced.properties['--motion-normal']).toBe('0ms');
        expect(reduced.properties['--motion-slow']).toBe('0ms');
        expect(reduced.properties['--motion-scale']).toBe('0');
        expect(resolveTheme(DEFAULT_SELECTION, DARK).properties['--motion-fast']).toBe('120ms');
    });

    it('applies the font choice to the text roles and keeps the monospace role for addresses and amounts', () => {
        const system = resolveTheme({ ...DEFAULT_SELECTION, font: 'system' }, DARK);
        expect(system.properties['--font-sans']).toBe(themeCatalogue.fonts.system.sans);
        expect(system.properties['--font-display']).toBe(themeCatalogue.fonts.system.display);
        const mono = resolveTheme({ ...DEFAULT_SELECTION, font: 'mono' }, DARK);
        expect(mono.properties['--font-sans']).toBe(themeCatalogue.typography.mono);
        for (const resolved of [system, mono, resolveTheme(DEFAULT_SELECTION, DARK)]) {
            expect(resolved.properties['--font-mono']).toBe(themeCatalogue.typography.mono);
        }
    });

    it('applies the accent to every action token', () => {
        const teal = resolveTheme({ ...DEFAULT_SELECTION, accent: 'teal' }, DARK);
        expect(teal.properties['--color-action-primary']).toBe(themeCatalogue.accents.teal.primary);
        expect(teal.properties['--color-action-primary-muted']).toBe(themeCatalogue.accents.teal.muted.dark);
        expect(teal.properties['--color-action-on-primary']).toBe(themeCatalogue.accents.teal.onPrimary);
    });
});

describe('parseSelection', () => {
    it('reads a stored selection and replaces unknown fields with the defaults', () => {
        expect(parseSelection(null)).toBeNull();
        expect(parseSelection('not json')).toBeNull();
        expect(parseSelection('[]')).toBeNull();
        expect(parseSelection('"dark"')).toBeNull();
        expect(parseSelection(JSON.stringify({ theme: 'system', accent: 'rose', font: 'mono', size: 'large', density: 'dense' }))).toEqual({
            theme: 'system',
            accent: 'rose',
            font: 'mono',
            size: 'large',
            density: 'dense',
        });
        expect(parseSelection(JSON.stringify({ theme: 'neon', accent: 'toString', size: 'huge' }))).toEqual(DEFAULT_SELECTION);
    });
});
