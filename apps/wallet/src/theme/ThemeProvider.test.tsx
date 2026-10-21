// @vitest-environment jsdom
import { act, useLayoutEffect } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { DEFAULT_SELECTION, themeCatalogue } from './catalogue';
import { ThemeEnvironmentStore, browserEnvironment } from './environment';
import { THEME_ACCOUNT_POINTER_KEY, THEME_STORAGE_KEY, accountStorageKey } from './persistence';
import { resolveTheme } from './resolve';
import { ThemeProvider, useTheme, useThemeAccount, type ThemeContextValue } from './ThemeProvider';
import { DEFAULT_ENVIRONMENT, type ThemeEnvironment, type ThemeSelection } from './schema';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const ACCOUNT = '0x00000000000000000000000000000000000000bb';

let root: Root | null = null;
let container: HTMLDivElement;
let current: ThemeContextValue | null = null;

function Probe({ account }: { account?: string | null }) {
    const value = useTheme();
    useThemeAccount(account);
    useLayoutEffect(() => {
        current = value;
    });
    return null;
}

function theme(): ThemeContextValue {
    if (!current) throw new Error('the theme context did not render');
    return current;
}

async function mount(environment: ThemeEnvironmentStore, account?: string | null) {
    await act(async () => {
        root = createRoot(container);
        root.render(
            <ThemeProvider environment={environment}>
                <Probe account={account} />
            </ThemeProvider>,
        );
    });
}

async function rerender(environment: ThemeEnvironmentStore, account?: string | null) {
    await act(async () => {
        root!.render(
            <ThemeProvider environment={environment}>
                <Probe account={account} />
            </ThemeProvider>,
        );
    });
}

function expectRoot(selection: ThemeSelection, env: ThemeEnvironment) {
    const resolved = resolveTheme(selection, env);
    const element = document.documentElement;
    for (const [name, value] of Object.entries(resolved.properties)) {
        expect(element.style.getPropertyValue(name), name).toBe(value);
    }
    expect(element.getAttribute('data-theme')).toBe(resolved.id);
    expect(element.getAttribute('data-motion')).toBe(env.reducedMotion ? 'reduced' : 'full');
    expect(element.style.colorScheme).toBe(resolved.scheme);
    expect(element.classList.contains('dark')).toBe(resolved.scheme === 'dark');
}

beforeEach(() => {
    window.localStorage.clear();
    document.documentElement.removeAttribute('style');
    document.documentElement.className = 'dark';
    container = document.createElement('div');
    document.body.appendChild(container);
    current = null;
});

afterEach(async () => {
    await act(async () => {
        root?.unmount();
    });
    root = null;
    container.remove();
});

describe('ThemeProvider', () => {
    it('applies the default selection on the root element', async () => {
        await mount(new ThemeEnvironmentStore());
        expect(theme().selection).toEqual(DEFAULT_SELECTION);
        expect(theme().resolved.id).toBe('dark');
        expectRoot(DEFAULT_SELECTION, DEFAULT_ENVIRONMENT);
    });

    it('applies and persists a changed selection', async () => {
        await mount(new ThemeEnvironmentStore());
        await act(async () => {
            theme().setSelection({ theme: 'light', accent: 'teal', size: 'large' });
        });
        const expected: ThemeSelection = { ...DEFAULT_SELECTION, theme: 'light', accent: 'teal', size: 'large' };
        expect(theme().selection).toEqual(expected);
        expectRoot(expected, DEFAULT_ENVIRONMENT);
        expect(document.documentElement.style.getPropertyValue('--root-font-size')).toBe('112.5%');
        expect(JSON.parse(window.localStorage.getItem(THEME_STORAGE_KEY)!)).toEqual(expected);
    });

    it('starts from the stored selection', async () => {
        const stored: ThemeSelection = { ...DEFAULT_SELECTION, theme: 'contrast-light', font: 'mono' };
        window.localStorage.setItem(THEME_STORAGE_KEY, JSON.stringify(stored));
        await mount(new ThemeEnvironmentStore());
        expect(theme().selection).toEqual(stored);
        expectRoot(stored, DEFAULT_ENVIRONMENT);
    });

    it('follows the system scheme and reduced motion when the selection says system', async () => {
        const environment = new ThemeEnvironmentStore({ colorScheme: 'dark', reducedMotion: false });
        await mount(environment);
        await act(async () => {
            theme().setSelection({ theme: 'system' });
        });
        expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
        const light: ThemeEnvironment = { colorScheme: 'light', reducedMotion: true };
        await act(async () => {
            environment.set(light);
        });
        expect(theme().environment).toEqual(light);
        expectRoot({ ...DEFAULT_SELECTION, theme: 'system' }, light);
        expect(document.documentElement.style.getPropertyValue('--color-surface-base')).toBe(
            themeCatalogue.themes.light.colors.surface.base,
        );
        expect(document.documentElement.style.getPropertyValue('--motion-normal')).toBe('0ms');
        await act(async () => {
            theme().setSelection({ theme: 'contrast-dark' });
        });
        expect(document.documentElement.getAttribute('data-theme')).toBe('contrast-dark');
    });

    it('keys the selection by the signed-in account and returns to the device selection on sign-out', async () => {
        const environment = new ThemeEnvironmentStore();
        await mount(environment, undefined);
        await act(async () => {
            theme().setSelection({ theme: 'light' });
        });
        await rerender(environment, ACCOUNT);
        expect(theme().account).toBe(ACCOUNT);
        expect(window.localStorage.getItem(THEME_ACCOUNT_POINTER_KEY)).toBe(ACCOUNT);
        expect(theme().selection.theme).toBe('light');
        await act(async () => {
            theme().setSelection({ theme: 'contrast-dark', accent: 'green' });
        });
        const accountSelection: ThemeSelection = { ...DEFAULT_SELECTION, theme: 'contrast-dark', accent: 'green' };
        expect(JSON.parse(window.localStorage.getItem(accountStorageKey(ACCOUNT))!)).toEqual(accountSelection);
        expect(JSON.parse(window.localStorage.getItem(THEME_STORAGE_KEY)!).theme).toBe('light');
        expectRoot(accountSelection, DEFAULT_ENVIRONMENT);

        await rerender(environment, null);
        expect(theme().account).toBeNull();
        expect(window.localStorage.getItem(THEME_ACCOUNT_POINTER_KEY)).toBeNull();
        expect(theme().selection).toEqual({ ...DEFAULT_SELECTION, theme: 'light' });
        expectRoot({ ...DEFAULT_SELECTION, theme: 'light' }, DEFAULT_ENVIRONMENT);

        await rerender(environment, ACCOUNT);
        expect(theme().selection).toEqual(accountSelection);
    });
});

describe('browserEnvironment', () => {
    it('reads the default environment when the browser offers no media queries', () => {
        const source = browserEnvironment(window);
        expect(typeof window.matchMedia).toBe('undefined');
        expect(source.read()).toEqual(DEFAULT_ENVIRONMENT);
        expect(browserEnvironment(undefined).read()).toEqual(DEFAULT_ENVIRONMENT);
    });

    it('notifies subscribers only when the environment changes', () => {
        const store = new ThemeEnvironmentStore();
        let calls = 0;
        const unsubscribe = store.subscribe(() => {
            calls += 1;
        });
        store.set(DEFAULT_ENVIRONMENT);
        expect(calls).toBe(0);
        store.set({ colorScheme: 'light', reducedMotion: false });
        expect(calls).toBe(1);
        unsubscribe();
        store.set({ colorScheme: 'dark', reducedMotion: false });
        expect(calls).toBe(1);
    });
});
