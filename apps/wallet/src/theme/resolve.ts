import { themeCatalogue } from './catalogue';
import type {
    ResolvedTheme,
    ThemeColors,
    ThemeData,
    ThemeEnvironment,
    ThemeId,
    ThemeSelection,
} from './schema';

export function resolveThemeWith(data: ThemeData, selection: ThemeSelection, env: ThemeEnvironment): ResolvedTheme {
    const themeId: ThemeId =
        selection.theme === 'system' ? (env.colorScheme === 'light' ? 'light' : 'dark') : selection.theme;
    const theme = data.themes[themeId];
    const accent = data.accents[selection.accent];
    const font = data.fonts[selection.font];
    const size = data.sizes[selection.size];
    const density = data.densities[selection.density];
    const fmt = function (value: number): string {
        return String(Math.round(value * 10000) / 10000);
    };
    const rem = function (value: number): string {
        return fmt(value) + 'rem';
    };
    const properties: Record<string, string> = {};

    (Object.keys(theme.colors) as Array<keyof ThemeColors>).forEach(function (group) {
        const tokens = theme.colors[group] as Record<string, string>;
        Object.keys(tokens).forEach(function (token) {
            properties['--color-' + group + '-' + token] = tokens[token];
        });
    });
    properties['--color-action-primary'] = accent.primary;
    properties['--color-action-primary-muted'] = accent.muted[theme.scheme];
    properties['--color-action-on-primary'] = accent.onPrimary;
    data.aliases.forEach(function (alias) {
        properties[alias[0]] = 'var(' + alias[1] + ')';
    });

    const typeSizes = data.typography.sizes as Record<string, number>;
    Object.keys(typeSizes).forEach(function (step) {
        properties['--font-size-' + step] = rem(typeSizes[step]);
    });
    const lineHeights = data.typography.lineHeights as Record<string, number>;
    Object.keys(lineHeights).forEach(function (name) {
        properties['--line-height-' + name] = fmt(lineHeights[name]);
    });
    const weights = data.typography.weights as Record<string, number>;
    Object.keys(weights).forEach(function (name) {
        properties['--font-weight-' + name] = String(weights[name]);
    });
    properties['--font-sans'] = font.sans;
    properties['--font-display'] = font.display;
    properties['--font-mono'] = data.typography.mono;
    properties['--font-scale'] = fmt(size.scale);
    properties['--root-font-size'] = fmt(size.scale * 100) + '%';

    const space = data.layout.space as Record<string, number>;
    Object.keys(space).forEach(function (step) {
        properties['--space-' + step] = rem(space[step] * density.factor);
    });
    const control = data.layout.control * density.factor;
    properties['--density-control'] = rem(control);
    properties['--touch-target'] = rem(Math.max(control, data.layout.touchTargetPx / (data.layout.rootPx * size.scale)));
    const radius = data.layout.radius as Record<string, number>;
    Object.keys(radius).forEach(function (name) {
        properties['--radius-' + name] = rem(radius[name]);
    });

    properties['--elevation-sheet'] = theme.shadow.sheet;
    properties['--elevation-menu'] = theme.shadow.menu;

    properties['--motion-fast'] = env.reducedMotion ? '0ms' : data.motion.fast + 'ms';
    properties['--motion-normal'] = env.reducedMotion ? '0ms' : data.motion.normal + 'ms';
    properties['--motion-slow'] = env.reducedMotion ? '0ms' : data.motion.slow + 'ms';
    properties['--motion-scale'] = env.reducedMotion ? '0' : '1';
    properties['--pax-ease'] = data.motion.ease;

    return { id: themeId, scheme: theme.scheme, reducedMotion: env.reducedMotion, properties: properties };
}

export function parseSelectionWith(data: ThemeData, raw: string | null): ThemeSelection | null {
    if (raw === null) return null;
    let value: unknown;
    try {
        value = JSON.parse(raw);
    } catch (error) {
        return null;
    }
    if (!value || typeof value !== 'object' || Array.isArray(value)) return null;
    const record = value as Record<string, unknown>;
    const known = function (table: object, candidate: unknown): boolean {
        return typeof candidate === 'string' && Object.prototype.hasOwnProperty.call(table, candidate);
    };
    return {
        theme:
            record.theme === 'system' || known(data.themes, record.theme)
                ? (record.theme as ThemeSelection['theme'])
                : data.defaults.theme,
        accent: known(data.accents, record.accent) ? (record.accent as ThemeSelection['accent']) : data.defaults.accent,
        font: known(data.fonts, record.font) ? (record.font as ThemeSelection['font']) : data.defaults.font,
        size: known(data.sizes, record.size) ? (record.size as ThemeSelection['size']) : data.defaults.size,
        density: known(data.densities, record.density)
            ? (record.density as ThemeSelection['density'])
            : data.defaults.density,
    };
}

export function applyResolvedTheme(doc: Document, resolved: ResolvedTheme): void {
    const root = doc.documentElement;
    Object.keys(resolved.properties).forEach(function (name) {
        root.style.setProperty(name, resolved.properties[name]);
    });
    root.setAttribute('data-theme', resolved.id);
    root.setAttribute('data-motion', resolved.reducedMotion ? 'reduced' : 'full');
    root.style.colorScheme = resolved.scheme;
    if (resolved.scheme === 'dark') root.classList.add('dark');
    else root.classList.remove('dark');
    const meta = doc.querySelector('meta[name="theme-color"]');
    if (meta) meta.setAttribute('content', resolved.properties['--color-surface-base']);
}

export function resolveTheme(selection: ThemeSelection, env: ThemeEnvironment): ResolvedTheme {
    return resolveThemeWith(themeCatalogue, selection, env);
}

export function parseSelection(raw: string | null): ThemeSelection | null {
    return parseSelectionWith(themeCatalogue, raw);
}
