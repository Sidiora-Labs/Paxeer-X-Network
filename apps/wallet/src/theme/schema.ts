export type ColorScheme = 'dark' | 'light';
export type ContrastLevel = 'standard' | 'high';

export type SurfaceToken = 'base' | 'raised' | 'card' | 'control' | 'overlay';
export type TextToken = 'strong' | 'primary' | 'secondary' | 'tertiary' | 'disabled';
export type ActionToken = 'primary' | 'primary-muted' | 'on-primary';
export type StatusToken = 'success' | 'danger' | 'warning' | 'info';
export type LineToken = 'subtle' | 'strong' | 'separator';
export type OverlayToken = 'scrim' | 'glass' | 'nav' | 'shimmer' | 'shimmer-strong';
export type ShadowToken = 'sheet' | 'menu';

export const SURFACE_TOKENS: readonly SurfaceToken[] = ['base', 'raised', 'card', 'control', 'overlay'];
export const TEXT_TOKENS: readonly TextToken[] = ['strong', 'primary', 'secondary', 'tertiary', 'disabled'];
export const ACTION_TOKENS: readonly ActionToken[] = ['primary', 'primary-muted', 'on-primary'];
export const STATUS_TOKENS: readonly StatusToken[] = ['success', 'danger', 'warning', 'info'];
export const LINE_TOKENS: readonly LineToken[] = ['subtle', 'strong', 'separator'];
export const OVERLAY_TOKENS: readonly OverlayToken[] = ['scrim', 'glass', 'nav', 'shimmer', 'shimmer-strong'];
export const SHADOW_TOKENS: readonly ShadowToken[] = ['sheet', 'menu'];

export interface ThemeColors {
    surface: Record<SurfaceToken, string>;
    text: Record<TextToken, string>;
    status: Record<StatusToken, string>;
    border: Record<LineToken, string>;
    overlay: Record<OverlayToken, string>;
}

export type ColorGroup = keyof ThemeColors | 'action';

export const COLOR_GROUP_TOKENS: Readonly<Record<ColorGroup, readonly string[]>> = {
    surface: SURFACE_TOKENS,
    text: TEXT_TOKENS,
    action: ACTION_TOKENS,
    status: STATUS_TOKENS,
    border: LINE_TOKENS,
    overlay: OVERLAY_TOKENS,
};

export const COLOR_PROPERTIES: readonly string[] = (Object.keys(COLOR_GROUP_TOKENS) as ColorGroup[]).flatMap((group) =>
    COLOR_GROUP_TOKENS[group].map((token) => `--color-${group}-${token}`),
);

export type ThemeId = 'dark' | 'light' | 'contrast-dark' | 'contrast-light';
export type ThemeChoice = ThemeId | 'system';
export type AccentId = 'blue' | 'teal' | 'green' | 'amber' | 'rose';
export type FontId = 'paxeer' | 'system' | 'mono';
export type SizeId = 'compact' | 'regular' | 'large';
export type DensityId = 'comfortable' | 'dense';

export interface ThemePalette {
    id: ThemeId;
    label: string;
    scheme: ColorScheme;
    contrast: ContrastLevel;
    colors: ThemeColors;
    shadow: Record<ShadowToken, string>;
}

export interface AccentPalette {
    id: AccentId;
    label: string;
    primary: string;
    muted: Record<ColorScheme, string>;
    onPrimary: string;
}

export interface FontChoice {
    id: FontId;
    label: string;
    sans: string;
    display: string;
}

export interface SizePreset {
    id: SizeId;
    label: string;
    scale: number;
}

export interface DensityPreset {
    id: DensityId;
    label: string;
    factor: number;
}

export interface TypographyScale {
    sizes: Record<'xs' | 'sm' | 'md' | 'lg' | 'xl', number>;
    lineHeights: Record<'tight' | 'body', number>;
    weights: Record<'light' | 'regular' | 'medium' | 'semibold' | 'bold', number>;
    mono: string;
}

export interface LayoutScale {
    space: Record<'1' | '2' | '3' | '4' | '5' | '6' | '8', number>;
    control: number;
    touchTargetPx: number;
    rootPx: number;
    radius: Record<'sm' | 'md' | 'lg' | 'xl', number>;
}

export interface MotionScale {
    fast: number;
    normal: number;
    slow: number;
    ease: string;
}

export interface ThemeData {
    themes: Record<ThemeId, ThemePalette>;
    accents: Record<AccentId, AccentPalette>;
    fonts: Record<FontId, FontChoice>;
    sizes: Record<SizeId, SizePreset>;
    densities: Record<DensityId, DensityPreset>;
    typography: TypographyScale;
    layout: LayoutScale;
    motion: MotionScale;
    aliases: ReadonlyArray<readonly [string, string]>;
    defaults: ThemeSelection;
}

export interface ThemeSelection {
    theme: ThemeChoice;
    accent: AccentId;
    font: FontId;
    size: SizeId;
    density: DensityId;
}

export interface ThemeEnvironment {
    colorScheme: ColorScheme;
    reducedMotion: boolean;
}

export interface ResolvedTheme {
    id: ThemeId;
    scheme: ColorScheme;
    reducedMotion: boolean;
    properties: Record<string, string>;
}

export const DEFAULT_ENVIRONMENT: ThemeEnvironment = { colorScheme: 'dark', reducedMotion: false };
