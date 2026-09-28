// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { ACCENT_IDS, DENSITY_IDS, FONT_IDS, SIZE_IDS, THEME_IDS, themeCatalogue } from './catalogue';
import { AppearanceSettings } from './AppearanceSettings';
import { ThemeEnvironmentStore } from './environment';
import { THEME_STORAGE_KEY } from './persistence';
import { ThemeProvider } from './ThemeProvider';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root | null = null;
let container: HTMLDivElement;

function rootProperty(name: string): string {
    return document.documentElement.style.getPropertyValue(name);
}

function option(group: string, id: string): HTMLButtonElement {
    const element = container.querySelector<HTMLButtonElement>(`[data-group="${group}"] [data-option="${id}"]`);
    if (!element) throw new Error(`option ${group}/${id} is not rendered`);
    return element;
}

async function choose(group: string, id: string) {
    await act(async () => {
        option(group, id).click();
    });
}

beforeEach(async () => {
    window.localStorage.clear();
    document.documentElement.removeAttribute('style');
    container = document.createElement('div');
    document.body.appendChild(container);
    await act(async () => {
        root = createRoot(container);
        root.render(
            <ThemeProvider environment={new ThemeEnvironmentStore({ colorScheme: 'light', reducedMotion: false })}>
                <AppearanceSettings />
            </ThemeProvider>,
        );
    });
});

afterEach(async () => {
    await act(async () => {
        root?.unmount();
    });
    root = null;
    container.remove();
});

describe('AppearanceSettings', () => {
    it('renders a picker for every choice with the current selection checked', () => {
        const groups = [...container.querySelectorAll('[role="radiogroup"]')].map((group) => group.getAttribute('data-group'));
        expect(groups).toEqual(['theme', 'accent', 'font', 'size', 'density']);
        for (const id of ['system', ...THEME_IDS]) option('theme', id);
        for (const id of ACCENT_IDS) option('accent', id);
        for (const id of FONT_IDS) option('font', id);
        for (const id of SIZE_IDS) option('size', id);
        for (const id of DENSITY_IDS) option('density', id);
        expect(option('theme', 'dark').getAttribute('aria-checked')).toBe('true');
        expect(option('theme', 'light').getAttribute('aria-checked')).toBe('false');
        expect(option('accent', 'blue').getAttribute('aria-checked')).toBe('true');
        expect(container.querySelector('[data-testid="theme-preview"]')!.getAttribute('data-theme-preview')).toBe('dark');
    });

    it('changes the applied properties from each picker', async () => {
        await choose('theme', 'contrast-light');
        expect(rootProperty('--color-surface-base')).toBe(themeCatalogue.themes['contrast-light'].colors.surface.base);
        expect(document.documentElement.getAttribute('data-theme')).toBe('contrast-light');
        expect(option('theme', 'contrast-light').getAttribute('aria-checked')).toBe('true');
        expect(container.querySelector('[data-testid="theme-preview"]')!.getAttribute('data-theme-preview')).toBe('contrast-light');

        await choose('theme', 'system');
        expect(document.documentElement.getAttribute('data-theme')).toBe('light');

        await choose('accent', 'rose');
        expect(rootProperty('--color-action-primary')).toBe(themeCatalogue.accents.rose.primary);
        expect(rootProperty('--color-action-on-primary')).toBe(themeCatalogue.accents.rose.onPrimary);

        await choose('font', 'system');
        expect(rootProperty('--font-sans')).toBe(themeCatalogue.fonts.system.sans);

        await choose('size', 'compact');
        expect(rootProperty('--root-font-size')).toBe('87.5%');
        expect(rootProperty('--font-scale')).toBe('0.875');

        await choose('density', 'dense');
        expect(rootProperty('--space-4')).toBe('0.8rem');
        expect(rootProperty('--density-control')).toBe('2.2rem');

        expect(JSON.parse(window.localStorage.getItem(THEME_STORAGE_KEY)!)).toEqual({
            theme: 'system',
            accent: 'rose',
            font: 'system',
            size: 'compact',
            density: 'dense',
        });
    });
});
