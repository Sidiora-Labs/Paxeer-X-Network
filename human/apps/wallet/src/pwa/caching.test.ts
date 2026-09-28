import { describe, expect, it } from 'vitest';
import { WALLET_ENV } from '@/wallet/config';
import { ACCOUNT_ENV } from '@/account/config';
import {
    OFFLINE_URL,
    PRECACHE_URLS,
    SHELL_URL,
    cacheNames,
    staleCaches,
    strategyFor,
    type RequestFacts,
} from './caching';
import { PWA_NETWORK_ENV, PwaConfigError, networkOnlyBases, pwaNetworkEnv } from './config';
import { ICON_FILES, MANIFEST_URL } from './manifest';

const APP = 'http://127.0.0.1:3099';

const ENV = {
    NEXT_PUBLIC_PAXEER_WALLET_API: 'http://127.0.0.1:3098/',
    NEXT_PUBLIC_PAXEER_RPC_URL: 'http://127.0.0.1:3097/rpc',
    NEXT_PUBLIC_SUPABASE_URL: 'http://127.0.0.1:3096',
    NEXT_PUBLIC_PAXEER_ATTESTOR_URL: `${APP}/attestor/`,
    NEXT_PUBLIC_UNRELATED: 'http://127.0.0.1:3095',
};

const scope = { origin: APP, networkOnly: networkOnlyBases(pwaNetworkEnv(ENV)) };

function get(url: string, mode = 'cors'): RequestFacts {
    return { url, method: 'GET', mode };
}

describe('network-only configuration', () => {
    it('reads the gateway, chain RPC, identity provider and attestor variables the app already uses', () => {
        expect(PWA_NETWORK_ENV.gateway).toBe(WALLET_ENV.gatewayUrl);
        expect(PWA_NETWORK_ENV.rpc).toBe(WALLET_ENV.rpcUrl);
        expect(PWA_NETWORK_ENV.rpc).toBe(ACCOUNT_ENV.endpointUrl);
        expect(PWA_NETWORK_ENV.identity).toBe(WALLET_ENV.identityUrl);
        expect(scope.networkOnly).toEqual([
            'http://127.0.0.1:3096',
            'http://127.0.0.1:3097/rpc',
            'http://127.0.0.1:3098',
            `${APP}/attestor`,
        ]);
    });

    it('skips unset variables and refuses malformed or credential-bearing URLs', () => {
        expect(networkOnlyBases({})).toEqual([]);
        expect(networkOnlyBases({ NEXT_PUBLIC_SUPABASE_URL: '  ' })).toEqual([]);
        expect(() => networkOnlyBases({ NEXT_PUBLIC_PAXEER_WALLET_API: 'not a url' })).toThrow(PwaConfigError);
        expect(() => networkOnlyBases({ NEXT_PUBLIC_PAXEER_RPC_URL: 'ftp://127.0.0.1/rpc' })).toThrow(
            'NEXT_PUBLIC_PAXEER_RPC_URL must be an http or https URL',
        );
        expect(() => networkOnlyBases({ NEXT_PUBLIC_SUPABASE_URL: 'http://user:pass@127.0.0.1' })).toThrow(
            'NEXT_PUBLIC_SUPABASE_URL must not carry credentials',
        );
    });
});

describe('caching rule per origin', () => {
    it('never caches the gateway, chain RPC, identity provider or attestor', () => {
        expect(strategyFor(get('http://127.0.0.1:3098/v1/wallet/me'), scope)).toBe('network-only');
        expect(strategyFor(get('http://127.0.0.1:3098/icons/app/icon-192.png'), scope)).toBe('network-only');
        expect(strategyFor(get('http://127.0.0.1:3097/rpc'), scope)).toBe('network-only');
        expect(strategyFor(get('http://127.0.0.1:3096/auth/v1/token'), scope)).toBe('network-only');
        expect(strategyFor(get(`${APP}/attestor/health`), scope)).toBe('network-only');
        expect(strategyFor(get(`${APP}/attestor/logo.png`), scope)).toBe('network-only');
    });

    it('matches a configured base by path segment, not by string prefix', () => {
        expect(strategyFor(get(`${APP}/attestor-guide.png`), scope)).toBe('cache-first');
        expect(strategyFor(get('http://127.0.0.1:3097/rpc2/logo.png'), scope)).toBe('network-only');
    });

    it('leaves every other origin to the network', () => {
        expect(strategyFor(get('http://127.0.0.1:4000/_next/static/chunk.js'), scope)).toBe('network-only');
        expect(strategyFor(get('http://127.0.0.1:4000/', 'navigate'), scope)).toBe('network-only');
    });

    it('sends same-origin API, auth, data, worker and non-GET requests to the network', () => {
        expect(strategyFor(get(`${APP}/api/health`), scope)).toBe('network-only');
        expect(strategyFor(get(`${APP}/auth/callback`, 'navigate'), scope)).toBe('network-only');
        expect(strategyFor(get(`${APP}/_next/data/build/index.json`), scope)).toBe('network-only');
        expect(strategyFor(get(`${APP}/sw.js`), scope)).toBe('network-only');
        expect(strategyFor({ url: `${APP}/_next/static/chunk.js`, method: 'POST', mode: 'cors' }, scope)).toBe(
            'network-only',
        );
        expect(strategyFor(get(`${APP}/report?format=csv`), scope)).toBe('network-only');
        expect(strategyFor(get(`${APP}/wallet/state`), scope)).toBe('network-only');
    });

    it('serves same-origin navigations through the offline-aware handler', () => {
        expect(strategyFor(get(`${APP}/`, 'navigate'), scope)).toBe('navigation');
        expect(strategyFor(get(`${APP}/?screen=send`, 'navigate'), scope)).toBe('navigation');
    });

    it('serves the shell and static assets cache-first', () => {
        expect(strategyFor(get(`${APP}/_next/static/chunks/main.js?dpl=abc`), scope)).toBe('cache-first');
        expect(strategyFor(get(`${APP}/icons/app/icon-512.png`), scope)).toBe('cache-first');
        expect(strategyFor(get(`${APP}/manifest.json`), scope)).toBe('cache-first');
        expect(strategyFor(get(`${APP}/pns.svg`), scope)).toBe('cache-first');
    });
});

describe('cache names and precache list', () => {
    it('versions every cache and purges only stale caches of this app', () => {
        const current = cacheNames('0123456789abcdef');
        expect(current).toEqual({ shell: 'paxeer-shell-0123456789abcdef', static: 'paxeer-static-0123456789abcdef' });
        expect(
            staleCaches(
                ['paxeer-shell-old', 'paxeer-static-old', current.shell, current.static, 'another-app-cache'],
                current,
            ),
        ).toEqual(['paxeer-shell-old', 'paxeer-static-old']);
    });

    it('precaches the shell, the offline page, the manifest and every icon', () => {
        expect(PRECACHE_URLS).toContain(SHELL_URL);
        expect(PRECACHE_URLS).toContain(OFFLINE_URL);
        expect(PRECACHE_URLS).toContain(MANIFEST_URL);
        for (const icon of ICON_FILES) expect(PRECACHE_URLS).toContain(icon.src);
        expect(new Set(PRECACHE_URLS).size).toBe(PRECACHE_URLS.length);
    });
});
