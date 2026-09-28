import { readFileSync } from 'node:fs';
import path from 'node:path';
import { describe, expect, it } from 'vitest';
import { ACCENT_IDS, ACCENT_INKS, DEFAULT_SELECTION, THEME_IDS, themeCatalogue } from './catalogue';
import {
    CONTROL_CONTRAST,
    TEXT_CONTRAST,
    bestInk,
    contrastRatio,
    parseColor,
    relativeLuminance,
} from './contrast';
import { resolveTheme } from './resolve';
import {
    COLOR_PROPERTIES,
    DEFAULT_ENVIRONMENT,
    LINE_TOKENS,
    OVERLAY_TOKENS,
    SHADOW_TOKENS,
    STATUS_TOKENS,
    SURFACE_TOKENS,
    TEXT_TOKENS,
} from './schema';

describe('contrast', () => {
    it('implements the WCAG relative luminance contrast ratio', () => {
        expect(relativeLuminance('#ffffff')).toBeCloseTo(1, 10);
        expect(relativeLuminance('#000000')).toBe(0);
        expect(contrastRatio('#ffffff', '#000000')).toBeCloseTo(21, 10);
        expect(contrastRatio('#000000', '#ffffff')).toBeCloseTo(21, 10);
        expect(contrastRatio('#777777', '#ffffff')).toBeCloseTo(4.48, 2);
        expect(contrastRatio('#fff', '#ffffff')).toBe(1);
    });

    it('parses hex and functional colours and refuses anything else', () => {
        expect(parseColor('#2262d1')).toEqual({ r: 34, g: 98, b: 209, a: 1 });
        expect(parseColor('rgba(255, 255, 255, 0.12)')).toEqual({ r: 255, g: 255, b: 255, a: 0.12 });
        expect(parseColor('rgb(0 0 0 / 0.45)')).toEqual({ r: 0, g: 0, b: 0, a: 0.45 });
        expect(() => parseColor('blue')).toThrow();
        expect(() => parseColor('rgb(300, 0, 0)')).toThrow();
        expect(() => relativeLuminance('rgba(0, 0, 0, 0.5)')).toThrow();
    });

    it('picks the ink with the highest contrast', () => {
        expect(bestInk('#000000', ['#111111', '#ffffff'])).toBe('#ffffff');
        expect(bestInk('#ffffff', ['#111111', '#eeeeee'])).toBe('#111111');
        expect(() => bestInk('#ffffff', [])).toThrow();
    });
});

describe('theme catalogue', () => {
    it('ships the four named colour themes', () => {
        expect(THEME_IDS).toEqual(['dark', 'light', 'contrast-dark', 'contrast-light']);
        expect(themeCatalogue.themes.dark.scheme).toBe('dark');
        expect(themeCatalogue.themes.light.scheme).toBe('light');
        expect(themeCatalogue.themes['contrast-dark'].contrast).toBe('high');
        expect(themeCatalogue.themes['contrast-light'].contrast).toBe('high');
    });

    it('defines every colour and shadow token in every theme with a valid colour', () => {
        for (const id of THEME_IDS) {
            const theme = themeCatalogue.themes[id];
            expect(Object.keys(theme.colors.surface).sort()).toEqual([...SURFACE_TOKENS].sort());
            expect(Object.keys(theme.colors.text).sort()).toEqual([...TEXT_TOKENS].sort());
            expect(Object.keys(theme.colors.status).sort()).toEqual([...STATUS_TOKENS].sort());
            expect(Object.keys(theme.colors.border).sort()).toEqual([...LINE_TOKENS].sort());
            expect(Object.keys(theme.colors.overlay).sort()).toEqual([...OVERLAY_TOKENS].sort());
            expect(Object.keys(theme.shadow).sort()).toEqual([...SHADOW_TOKENS].sort());
            for (const group of Object.values(theme.colors)) {
                for (const value of Object.values(group as Record<string, string>)) {
                    expect(() => parseColor(value)).not.toThrow();
                }
            }
        }
    });

    it('resolves every colour property for every theme and accent', () => {
        for (const theme of THEME_IDS) {
            for (const accent of ACCENT_IDS) {
                const resolved = resolveTheme({ ...DEFAULT_SELECTION, theme, accent }, DEFAULT_ENVIRONMENT);
                for (const property of COLOR_PROPERTIES) {
                    expect(resolved.properties[property], `${theme}/${accent} ${property}`).toBeDefined();
                    expect(() => parseColor(resolved.properties[property])).not.toThrow();
                }
            }
        }
    });

    it('keeps body text at text contrast on every surface of every theme', () => {
        for (const id of THEME_IDS) {
            const { surface, text } = themeCatalogue.themes[id].colors;
            for (const token of SURFACE_TOKENS) {
                for (const ink of [text.primary, text.strong]) {
                    expect(contrastRatio(ink, surface[token]), `${id} ${ink} on ${token}`).toBeGreaterThanOrEqual(TEXT_CONTRAST);
                }
            }
        }
    });

    it('keeps every on-accent pair at text contrast and every accent at control contrast on each theme', () => {
        for (const id of ACCENT_IDS) {
            const accent = themeCatalogue.accents[id];
            expect(ACCENT_INKS).toContain(accent.onPrimary);
            expect(contrastRatio(accent.onPrimary, accent.primary), `${id} on-accent`).toBeGreaterThanOrEqual(TEXT_CONTRAST);
            for (const theme of THEME_IDS) {
                const resolved = resolveTheme({ ...DEFAULT_SELECTION, theme, accent: id }, DEFAULT_ENVIRONMENT);
                expect(
                    contrastRatio(resolved.properties['--color-action-on-primary'], resolved.properties['--color-action-primary']),
                ).toBeGreaterThanOrEqual(TEXT_CONTRAST);
                expect(
                    contrastRatio(resolved.properties['--color-action-primary'], resolved.properties['--color-surface-base']),
                    `${id} on ${theme}`,
                ).toBeGreaterThanOrEqual(CONTROL_CONTRAST);
            }
        }
    });

    it('keeps status colours at control contrast on the base surface of every theme', () => {
        for (const id of THEME_IDS) {
            const { surface, status } = themeCatalogue.themes[id].colors;
            for (const token of STATUS_TOKENS) {
                expect(contrastRatio(status[token], surface.base), `${id} ${token}`).toBeGreaterThanOrEqual(CONTROL_CONTRAST);
            }
        }
    });

    it('generates the root block of globals.css from the default selection', () => {
        const css = readFileSync(path.resolve(__dirname, '../app/globals.css'), 'utf8');
        const block = /:root\s*\{([^}]*)\}/.exec(css);
        expect(block).not.toBeNull();
        const declared = new Map<string, string>();
        for (const line of block![1].split(';')) {
            const match = /^\s*(--[\w-]+)\s*:\s*([\s\S]+?)\s*$/.exec(line);
            if (match) declared.set(match[1], match[2]);
        }
        const resolved = resolveTheme(DEFAULT_SELECTION, DEFAULT_ENVIRONMENT);
        expect(resolved.id).toBe('dark');
        for (const [name, value] of Object.entries(resolved.properties)) {
            expect(declared.get(name), name).toBe(value);
        }
    });
});
