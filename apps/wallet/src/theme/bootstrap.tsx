import { themeCatalogue } from './catalogue';
import { COLOR_SCHEME_QUERY, REDUCED_MOTION_QUERY } from './environment';
import { THEME_ACCOUNT_POINTER_KEY, THEME_STORAGE_KEY } from './persistence';
import { applyResolvedTheme, parseSelectionWith, resolveThemeWith } from './resolve';
import type { ThemeData } from './schema';

export interface BootstrapKeys {
    device: string;
    pointer: string;
    schemeQuery: string;
    motionQuery: string;
}

export function bootstrapTheme(
    resolve: typeof resolveThemeWith,
    parse: typeof parseSelectionWith,
    apply: typeof applyResolvedTheme,
    data: ThemeData,
    keys: BootstrapKeys,
): void {
    try {
        const storage = window.localStorage;
        const account = storage.getItem(keys.pointer);
        let raw = account ? storage.getItem(keys.device + ':' + account) : null;
        if (raw === null) raw = storage.getItem(keys.device);
        const selection = parse(data, raw);
        if (!selection) return;
        const media = typeof window.matchMedia === 'function';
        const light = media && window.matchMedia(keys.schemeQuery).matches;
        const reduced = media && window.matchMedia(keys.motionQuery).matches;
        apply(document, resolve(data, selection, { colorScheme: light ? 'light' : 'dark', reducedMotion: reduced }));
    } catch (error) {
        return;
    }
}

function embed(value: unknown): string {
    return JSON.stringify(value).replace(/</g, '\\u003c');
}

export function themeBootstrapScript(): string {
    const keys: BootstrapKeys = {
        device: THEME_STORAGE_KEY,
        pointer: THEME_ACCOUNT_POINTER_KEY,
        schemeQuery: COLOR_SCHEME_QUERY,
        motionQuery: REDUCED_MOTION_QUERY,
    };
    return `(${bootstrapTheme.toString()})(${resolveThemeWith.toString()},${parseSelectionWith.toString()},${applyResolvedTheme.toString()},${embed(themeCatalogue)},${embed(keys)});`;
}

export function ThemeBootstrap({ nonce }: { nonce?: string }) {
    return <script id="theme-bootstrap" nonce={nonce} dangerouslySetInnerHTML={{ __html: themeBootstrapScript() }} />;
}
