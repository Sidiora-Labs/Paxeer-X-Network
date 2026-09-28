import { DEFAULT_THEME_COLOR } from './catalogue';
import type { ColorScheme } from './schema';

export function currentThemeColor(doc: Document | undefined = typeof document === 'undefined' ? undefined : document): string {
    const applied = doc?.documentElement.style.getPropertyValue('--color-surface-base').trim();
    return applied ? applied : DEFAULT_THEME_COLOR;
}

export function currentThemeScheme(doc: Document | undefined = typeof document === 'undefined' ? undefined : document): ColorScheme {
    return doc?.documentElement.style.colorScheme === 'light' ? 'light' : 'dark';
}
