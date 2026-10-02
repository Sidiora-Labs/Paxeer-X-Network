import { DEFAULT_THEME_COLOR } from '@/theme/catalogue';

export const MANIFEST_URL = '/wallet/manifest.json';

export type IconPurpose = 'any' | 'maskable';

export interface ManifestIcon {
    readonly src: string;
    readonly sizes: string;
    readonly type: 'image/png';
    readonly purpose: IconPurpose;
}

export interface WebAppManifest {
    readonly id: string;
    readonly name: string;
    readonly short_name: string;
    readonly description: string;
    readonly start_url: string;
    readonly scope: string;
    readonly display: 'standalone';
    readonly orientation: 'portrait';
    readonly background_color: string;
    readonly theme_color: string;
    readonly lang: string;
    readonly dir: 'ltr';
    readonly categories: readonly string[];
    readonly prefer_related_applications: false;
    readonly icons: readonly ManifestIcon[];
}

function icon(file: string, size: number, purpose: IconPurpose): ManifestIcon {
    return { src: `/wallet/icons/app/${file}`, sizes: `${size}x${size}`, type: 'image/png', purpose };
}

export const MANIFEST_ICONS: readonly ManifestIcon[] = [
    icon('icon-192.png', 192, 'any'),
    icon('icon-512.png', 512, 'any'),
    icon('maskable-192.png', 192, 'maskable'),
    icon('maskable-512.png', 512, 'maskable'),
];

export const APPLE_TOUCH_ICON = { src: '/wallet/icons/app/apple-touch-icon-180.png', sizes: '180x180' } as const;

export const FAVICON_ICON = { src: '/wallet/icons/app/favicon-32.png', sizes: '32x32' } as const;

export const ICON_FILES: readonly { readonly src: string; readonly size: number }[] = [
    ...MANIFEST_ICONS.map((entry) => ({ src: entry.src, size: Number(entry.sizes.split('x')[0]) })),
    { src: APPLE_TOUCH_ICON.src, size: 180 },
    { src: FAVICON_ICON.src, size: 32 },
];

export function buildManifest(): WebAppManifest {
    return {
        id: '/wallet/',
        name: 'Paxeer Wallet',
        short_name: 'Paxeer',
        description: 'The Paxeer X Network wallet: one account across the chain and the kernel.',
        start_url: '/wallet/',
        scope: '/wallet/',
        display: 'standalone',
        orientation: 'portrait',
        background_color: DEFAULT_THEME_COLOR,
        theme_color: DEFAULT_THEME_COLOR,
        lang: 'en',
        dir: 'ltr',
        categories: ['finance', 'utilities'],
        prefer_related_applications: false,
        icons: MANIFEST_ICONS,
    };
}

export function serializeManifest(manifest: WebAppManifest = buildManifest()): string {
    return `${JSON.stringify(manifest, null, 2)}\n`;
}
