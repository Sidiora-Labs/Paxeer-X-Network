export const CACHE_PREFIX = 'paxeer-wallet-';

export const SERVICE_WORKER_URL = '/wallet/sw.js';

export const SHELL_URL = '/wallet/';

export const OFFLINE_URL = '/wallet/offline/';

export const PRECACHE_URLS: readonly string[] = [
    SHELL_URL,
    OFFLINE_URL,
    '/wallet/manifest.json',
    '/wallet/icons/app/icon-192.png',
    '/wallet/icons/app/icon-512.png',
    '/wallet/icons/app/maskable-192.png',
    '/wallet/icons/app/maskable-512.png',
    '/wallet/icons/app/apple-touch-icon-180.png',
    '/wallet/icons/app/favicon-32.png',
];

const NETWORK_PATH_PREFIXES: readonly string[] = ['/wallet/api/', '/wallet/auth/', '/wallet/_next/data/', '/wallet/_next/webpack-hmr'];

const STATIC_PATH_PREFIXES: readonly string[] = [
    '/wallet/_next/static/',
    '/wallet/icons/',
    '/wallet/splash_screens/',
    '/wallet/Paxeer_Sans_Rounded/',
    '/wallet/ui_icons/',
];

const STATIC_EXTENSION = /\.(?:png|jpe?g|gif|webp|avif|svg|ico|woff2?|ttf|otf|css|js)$/i;

export type CacheStrategy = 'network-only' | 'navigation' | 'cache-first';

export interface CacheNames {
    readonly shell: string;
    readonly static: string;
}

export interface RequestFacts {
    readonly url: string;
    readonly method: string;
    readonly mode: string;
}

export interface WorkerScopeFacts {
    readonly origin: string;
    readonly networkOnly: readonly string[];
}

export function cacheNames(version: string): CacheNames {
    return { shell: `${CACHE_PREFIX}shell-${version}`, static: `${CACHE_PREFIX}static-${version}` };
}

export function staleCaches(existing: readonly string[], current: CacheNames): string[] {
    const keep = new Set([current.shell, current.static]);
    return existing.filter((name) => name.startsWith(CACHE_PREFIX) && !keep.has(name));
}

export function underBase(url: URL, base: string): boolean {
    const href = `${url.origin}${url.pathname}`;
    if (href === base) return true;
    return href.startsWith(base.endsWith('/') ? base : `${base}/`);
}

export function strategyFor(request: RequestFacts, scope: WorkerScopeFacts): CacheStrategy {
    if (request.method !== 'GET') return 'network-only';
    let url: URL;
    try {
        url = new URL(request.url);
    } catch {
        return 'network-only';
    }
    if (url.protocol !== 'https:' && url.protocol !== 'http:') return 'network-only';
    if (scope.networkOnly.some((base) => underBase(url, base))) return 'network-only';
    if (url.origin !== scope.origin) return 'network-only';
    if (!url.pathname.startsWith('/wallet/')) return 'network-only';
    if (url.pathname === SERVICE_WORKER_URL) return 'network-only';
    if (NETWORK_PATH_PREFIXES.some((prefix) => url.pathname.startsWith(prefix))) return 'network-only';
    if (request.mode === 'navigate') return 'navigation';
    if (STATIC_PATH_PREFIXES.some((prefix) => url.pathname.startsWith(prefix))) return 'cache-first';
    if (url.search) return 'network-only';
    if (url.pathname === '/wallet/manifest.json') return 'cache-first';
    if (STATIC_EXTENSION.test(url.pathname)) return 'cache-first';
    return 'network-only';
}
