import { bestInk } from './contrast';
import type {
    AccentId,
    AccentPalette,
    DensityId,
    DensityPreset,
    FontChoice,
    FontId,
    SizeId,
    SizePreset,
    ThemeData,
    ThemeId,
    ThemePalette,
    ThemeSelection,
} from './schema';

export const ACCENT_INKS: readonly string[] = ['#fffdf8', '#111410'];

export const qrColors = { background: '#ffffff', foreground: '#000000' } as const;

const dark: ThemePalette = {
    id: 'dark',
    label: 'Paxeer dark',
    scheme: 'dark',
    contrast: 'standard',
    colors: {
        surface: { base: '#141413', raised: '#1c1c1a', card: '#23231f', control: '#2b2b27', overlay: '#30302b' },
        text: { strong: '#fffdf8', primary: '#eee9e2', secondary: '#aaa69f', tertiary: '#85827c', disabled: '#5e5c57' },
        status: { success: '#33c77a', danger: '#ff6972', warning: '#f3a63b', info: '#77a8f7' },
        border: { subtle: 'rgba(255, 255, 255, 0.12)', strong: 'rgba(255, 255, 255, 0.15)' },
        overlay: {
            scrim: 'rgba(0, 0, 0, 0.6)',
            glass: 'rgba(27, 27, 25, 0.8)',
            nav: 'rgba(22, 22, 21, 0.85)',
            shimmer: 'rgba(227, 217, 212, 0.04)',
            'shimmer-strong': 'rgba(227, 217, 212, 0.08)',
        },
    },
    shadow: { sheet: '0 24px 60px rgb(0 0 0 / 0.45)', menu: '0 12px 32px rgb(0 0 0 / 0.35)' },
};

const light: ThemePalette = {
    id: 'light',
    label: 'Paxeer light',
    scheme: 'light',
    contrast: 'standard',
    colors: {
        surface: { base: '#faf8f4', raised: '#f2efe9', card: '#ebe7df', control: '#e2ddd4', overlay: '#d8d2c7' },
        text: { strong: '#0d0d0c', primary: '#1c1b19', secondary: '#4f4c47', tertiary: '#6b6862', disabled: '#9a968f' },
        status: { success: '#1a7f4b', danger: '#c7303b', warning: '#9a5b00', info: '#2560c4' },
        border: { subtle: 'rgba(0, 0, 0, 0.1)', strong: 'rgba(0, 0, 0, 0.14)' },
        overlay: {
            scrim: 'rgba(0, 0, 0, 0.4)',
            glass: 'rgba(250, 248, 244, 0.8)',
            nav: 'rgba(242, 239, 233, 0.85)',
            shimmer: 'rgba(28, 27, 25, 0.04)',
            'shimmer-strong': 'rgba(28, 27, 25, 0.08)',
        },
    },
    shadow: { sheet: '0 24px 60px rgb(0 0 0 / 0.18)', menu: '0 12px 32px rgb(0 0 0 / 0.12)' },
};

const contrastDark: ThemePalette = {
    id: 'contrast-dark',
    label: 'High contrast dark',
    scheme: 'dark',
    contrast: 'high',
    colors: {
        surface: { base: '#000000', raised: '#0d0d0d', card: '#141414', control: '#1f1f1f', overlay: '#262626' },
        text: { strong: '#ffffff', primary: '#ffffff', secondary: '#e6e6e6', tertiary: '#cccccc', disabled: '#8c8c8c' },
        status: { success: '#5fe39a', danger: '#ff8a91', warning: '#ffc266', info: '#9cc2ff' },
        border: { subtle: 'rgba(255, 255, 255, 0.4)', strong: 'rgba(255, 255, 255, 0.6)' },
        overlay: {
            scrim: 'rgba(0, 0, 0, 0.8)',
            glass: 'rgba(0, 0, 0, 0.9)',
            nav: 'rgba(0, 0, 0, 0.92)',
            shimmer: 'rgba(255, 255, 255, 0.06)',
            'shimmer-strong': 'rgba(255, 255, 255, 0.12)',
        },
    },
    shadow: { sheet: '0 24px 60px rgb(0 0 0 / 0.6)', menu: '0 12px 32px rgb(0 0 0 / 0.5)' },
};

const contrastLight: ThemePalette = {
    id: 'contrast-light',
    label: 'High contrast light',
    scheme: 'light',
    contrast: 'high',
    colors: {
        surface: { base: '#ffffff', raised: '#f2f2f2', card: '#ebebeb', control: '#e0e0e0', overlay: '#d6d6d6' },
        text: { strong: '#000000', primary: '#000000', secondary: '#1f1f1f', tertiary: '#333333', disabled: '#666666' },
        status: { success: '#0b6b3a', danger: '#a8141f', warning: '#7a4300', info: '#0f47a8' },
        border: { subtle: 'rgba(0, 0, 0, 0.45)', strong: 'rgba(0, 0, 0, 0.65)' },
        overlay: {
            scrim: 'rgba(0, 0, 0, 0.6)',
            glass: 'rgba(255, 255, 255, 0.92)',
            nav: 'rgba(255, 255, 255, 0.95)',
            shimmer: 'rgba(0, 0, 0, 0.06)',
            'shimmer-strong': 'rgba(0, 0, 0, 0.12)',
        },
    },
    shadow: { sheet: '0 24px 60px rgb(0 0 0 / 0.25)', menu: '0 12px 32px rgb(0 0 0 / 0.18)' },
};

function accent(id: AccentId, label: string, primary: string, mutedDark: string, mutedLight: string): AccentPalette {
    return { id, label, primary, muted: { dark: mutedDark, light: mutedLight }, onPrimary: bestInk(primary, ACCENT_INKS) };
}

const accents: Record<AccentId, AccentPalette> = {
    blue: accent('blue', 'Blue', '#2262d1', '#194796', '#cddcf6'),
    teal: accent('teal', 'Teal', '#0e7a80', '#0a585c', '#c8e6e7'),
    green: accent('green', 'Green', '#1a7d46', '#135a33', '#cae6d5'),
    amber: accent('amber', 'Amber', '#a85a00', '#7a4100', '#f3dcc0'),
    rose: accent('rose', 'Rose', '#c43a62', '#8e2a47', '#f3cfd9'),
};

const MONO_STACK = "'JetBrains Mono', ui-monospace, monospace";

const fonts: Record<FontId, FontChoice> = {
    paxeer: {
        id: 'paxeer',
        label: 'Paxeer Sans Rounded',
        sans: "'Paxeer Sans Rounded', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif",
        display: "'Paxeer Sans Rounded', sans-serif",
    },
    system: {
        id: 'system',
        label: 'System',
        sans: "system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
        display: "system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
    },
    mono: {
        id: 'mono',
        label: 'Monospace',
        sans: MONO_STACK,
        display: MONO_STACK,
    },
};

const sizes: Record<SizeId, SizePreset> = {
    compact: { id: 'compact', label: 'Compact', scale: 0.875 },
    regular: { id: 'regular', label: 'Regular', scale: 1 },
    large: { id: 'large', label: 'Large', scale: 1.125 },
};

const densities: Record<DensityId, DensityPreset> = {
    comfortable: { id: 'comfortable', label: 'Comfortable', factor: 1 },
    dense: { id: 'dense', label: 'Dense', factor: 0.8 },
};

export const DEFAULT_SELECTION: ThemeSelection = {
    theme: 'dark',
    accent: 'blue',
    font: 'paxeer',
    size: 'regular',
    density: 'comfortable',
};

export const themeCatalogue: ThemeData = {
    themes: { dark, light, 'contrast-dark': contrastDark, 'contrast-light': contrastLight },
    accents,
    fonts,
    sizes,
    densities,
    typography: {
        sizes: { xs: 0.75, sm: 0.875, md: 1, lg: 1.25, xl: 1.75 },
        lineHeights: { tight: 1.2, body: 1.5 },
        weights: { light: 300, regular: 400, medium: 500, semibold: 600, bold: 700 },
        mono: MONO_STACK,
    },
    layout: {
        space: { '1': 0.25, '2': 0.5, '3': 0.75, '4': 1, '5': 1.25, '6': 1.5, '8': 2 },
        control: 2.75,
        touchTargetPx: 44,
        rootPx: 16,
        radius: { sm: 0.5, md: 0.75, lg: 1, xl: 1.25 },
    },
    motion: { fast: 120, normal: 200, slow: 350, ease: 'cubic-bezier(0.2, 0, 0, 1)' },
    aliases: [
        ['--pax-bg', '--color-surface-base'],
        ['--pax-surface', '--color-surface-raised'],
        ['--pax-card', '--color-surface-card'],
        ['--pax-accent', '--color-action-primary'],
        ['--pax-accent-dim', '--color-action-primary-muted'],
        ['--pax-mid-dark', '--color-text-disabled'],
        ['--pax-mid', '--color-text-tertiary'],
        ['--pax-subtle', '--color-text-secondary'],
        ['--pax-light', '--color-text-primary'],
        ['--pax-off-white', '--color-text-strong'],
        ['--pax-success', '--color-status-success'],
        ['--pax-error', '--color-status-danger'],
        ['--pax-warning', '--color-status-warning'],
    ],
    defaults: DEFAULT_SELECTION,
};

export const THEME_IDS = Object.keys(themeCatalogue.themes) as ThemeId[];
export const ACCENT_IDS = Object.keys(themeCatalogue.accents) as AccentId[];
export const FONT_IDS = Object.keys(themeCatalogue.fonts) as FontId[];
export const SIZE_IDS = Object.keys(themeCatalogue.sizes) as SizeId[];
export const DENSITY_IDS = Object.keys(themeCatalogue.densities) as DensityId[];

export const DEFAULT_THEME_COLOR = dark.colors.surface.base;
