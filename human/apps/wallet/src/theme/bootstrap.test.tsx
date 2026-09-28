// @vitest-environment jsdom
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { DEFAULT_SELECTION, themeCatalogue } from './catalogue';
import { ThemeBootstrap, themeBootstrapScript } from './bootstrap';
import { currentThemeColor, currentThemeScheme } from './dom';
import { THEME_ACCOUNT_POINTER_KEY, THEME_STORAGE_KEY, accountStorageKey } from './persistence';
import { resolveTheme } from './resolve';
import { DEFAULT_ENVIRONMENT, type ThemeSelection } from './schema';

const ACCOUNT = '0x00000000000000000000000000000000000000aa';

function resetRoot() {
    const root = document.documentElement;
    root.removeAttribute('style');
    root.removeAttribute('data-theme');
    root.removeAttribute('data-motion');
    root.className = 'dark';
    document.head.innerHTML = '<meta name="theme-color" content="initial">';
}

function runBootstrap() {
    new Function(themeBootstrapScript())();
}

function expectApplied(selection: ThemeSelection) {
    const resolved = resolveTheme(selection, DEFAULT_ENVIRONMENT);
    const root = document.documentElement;
    for (const [name, value] of Object.entries(resolved.properties)) {
        expect(root.style.getPropertyValue(name), name).toBe(value);
    }
    expect(root.getAttribute('data-theme')).toBe(resolved.id);
    expect(root.getAttribute('data-motion')).toBe('full');
    expect(root.style.colorScheme).toBe(resolved.scheme);
    expect(root.classList.contains('dark')).toBe(resolved.scheme === 'dark');
    expect(document.querySelector('meta[name="theme-color"]')!.getAttribute('content')).toBe(
        resolved.properties['--color-surface-base'],
    );
}

beforeEach(() => {
    window.localStorage.clear();
    resetRoot();
});

afterEach(() => {
    resetRoot();
});

describe('theme bootstrap script', () => {
    it('renders one inline script carrying the nonce and the catalogue', () => {
        const markup = renderToStaticMarkup(<ThemeBootstrap nonce="abc123" />);
        expect(markup.startsWith('<script id="theme-bootstrap" nonce="abc123">')).toBe(true);
        expect(markup).toContain(THEME_STORAGE_KEY);
        expect(markup).toContain(THEME_ACCOUNT_POINTER_KEY);
        expect(markup).toContain(themeCatalogue.themes['contrast-light'].colors.surface.base);
        expect(markup.slice('<script'.length)).not.toContain('<script');
        expect(themeBootstrapScript()).not.toContain('</');
    });

    it('leaves the server-rendered default untouched when nothing is stored', () => {
        runBootstrap();
        expect(document.documentElement.getAttribute('style')).toBeNull();
        expect(document.documentElement.getAttribute('data-theme')).toBeNull();
        expect(currentThemeColor()).toBe(themeCatalogue.themes.dark.colors.surface.base);
        expect(currentThemeScheme()).toBe('dark');
    });

    it('applies the stored device selection before first paint', () => {
        const selection: ThemeSelection = { theme: 'light', accent: 'amber', font: 'system', size: 'large', density: 'dense' };
        window.localStorage.setItem(THEME_STORAGE_KEY, JSON.stringify(selection));
        runBootstrap();
        expectApplied(selection);
        expect(currentThemeColor()).toBe(themeCatalogue.themes.light.colors.surface.base);
        expect(currentThemeScheme()).toBe('light');
    });

    it('prefers the selection of the signed-in account', () => {
        const device: ThemeSelection = { ...DEFAULT_SELECTION, theme: 'light' };
        const account: ThemeSelection = { ...DEFAULT_SELECTION, theme: 'contrast-dark', accent: 'rose' };
        window.localStorage.setItem(THEME_STORAGE_KEY, JSON.stringify(device));
        window.localStorage.setItem(accountStorageKey(ACCOUNT), JSON.stringify(account));
        window.localStorage.setItem(THEME_ACCOUNT_POINTER_KEY, ACCOUNT);
        runBootstrap();
        expectApplied(account);
    });

    it('falls back to the device selection when the account has none', () => {
        const device: ThemeSelection = { ...DEFAULT_SELECTION, theme: 'contrast-light' };
        window.localStorage.setItem(THEME_STORAGE_KEY, JSON.stringify(device));
        window.localStorage.setItem(THEME_ACCOUNT_POINTER_KEY, ACCOUNT);
        runBootstrap();
        expectApplied(device);
    });

    it('resolves a stored system selection to dark when the browser offers no media queries', () => {
        window.localStorage.setItem(THEME_STORAGE_KEY, JSON.stringify({ ...DEFAULT_SELECTION, theme: 'system' }));
        runBootstrap();
        expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
    });

    it('is inert when stored data is corrupt', () => {
        window.localStorage.setItem(THEME_STORAGE_KEY, '{not json');
        runBootstrap();
        expect(document.documentElement.getAttribute('style')).toBeNull();
    });

    it('is inert when storage is unavailable', () => {
        const original = Object.getOwnPropertyDescriptor(window, 'localStorage');
        Object.defineProperty(window, 'localStorage', {
            configurable: true,
            get() {
                throw new DOMException('storage is disabled', 'SecurityError');
            },
        });
        try {
            expect(() => runBootstrap()).not.toThrow();
            expect(document.documentElement.getAttribute('style')).toBeNull();
            expect(document.documentElement.getAttribute('data-theme')).toBeNull();
        } finally {
            if (original) Object.defineProperty(window, 'localStorage', original);
            else delete (window as { localStorage?: Storage }).localStorage;
        }
        expect(() => window.localStorage.getItem(THEME_STORAGE_KEY)).not.toThrow();
    });
});
