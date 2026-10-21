import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import vm from 'node:vm';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { buildPwa, type PwaBuildResult } from './build';
import { serializeManifest } from './manifest';

const APP_DIR = path.resolve(__dirname, '..', '..');
const ORIGIN = 'http://127.0.0.1:3099';

const ENV = {
    NEXT_PUBLIC_PAXEER_WALLET_API: 'http://127.0.0.1:3098',
    NEXT_PUBLIC_PAXEER_RPC_URL: `${ORIGIN}/rpc`,
    NEXT_PUBLIC_SUPABASE_URL: 'http://127.0.0.1:3096',
    NEXT_PUBLIC_PAXEER_ATTESTOR_URL: `${ORIGIN}/attestor`,
};

type Listener = (event: unknown) => void;

interface LoadedWorker {
    readonly listeners: Map<string, Listener>;
    readonly skipWaitingCalls: number[];
}

let outDir: string;
let result: PwaBuildResult;
let source: string;

function load(): LoadedWorker {
    const listeners = new Map<string, Listener>();
    const skipWaitingCalls: number[] = [];
    const scope = {
        location: new URL(`${ORIGIN}/sw.js`),
        addEventListener(type: string, listener: Listener) {
            listeners.set(type, listener);
        },
        skipWaiting() {
            skipWaitingCalls.push(Date.now());
            return Promise.resolve();
        },
    };
    vm.runInNewContext(source, { self: scope, URL, Request, Response, console });
    return { listeners, skipWaitingCalls };
}

function dispatchFetch(worker: LoadedWorker, url: string, method = 'GET', mode = 'cors'): boolean {
    let responded = false;
    worker.listeners.get('fetch')?.({
        request: { url, method, mode },
        respondWith(response: Promise<Response>) {
            responded = true;
            response.catch(() => undefined);
        },
    });
    return responded;
}

beforeAll(async () => {
    outDir = mkdtempSync(path.join(tmpdir(), 'paxeer-pwa-'));
    result = await buildPwa({ appDir: APP_DIR, outDir, env: ENV });
    source = readFileSync(result.workerPath, 'utf8');
}, 120_000);

afterAll(() => {
    rmSync(outDir, { recursive: true, force: true });
});

describe('built service worker', () => {
    it('is written with a content version and the manifest beside it', () => {
        expect(result.version).toMatch(/^[0-9a-f]{16}$/);
        expect(result.cacheNames).toEqual({
            shell: `paxeer-shell-${result.version}`,
            static: `paxeer-static-${result.version}`,
        });
        expect(source).toContain(result.version);
        expect(readFileSync(result.manifestPath, 'utf8')).toBe(serializeManifest());
        expect(result.networkOnly).toEqual([
            'http://127.0.0.1:3096',
            'http://127.0.0.1:3098',
            `${ORIGIN}/attestor`,
            `${ORIGIN}/rpc`,
        ]);
    });

    it('embeds the network-only bases from the environment and no other origin', () => {
        for (const base of result.networkOnly) expect(source).toContain(base);
        const origins = new Set(source.match(/https?:\/\/[A-Za-z0-9.-]+(?::\d+)?/g) ?? []);
        expect([...origins].sort()).toEqual(['http://127.0.0.1:3096', 'http://127.0.0.1:3098', ORIGIN]);
    });

    it('changes version when the network-only configuration changes', async () => {
        const other = mkdtempSync(path.join(tmpdir(), 'paxeer-pwa-'));
        try {
            const rebuilt = await buildPwa({ appDir: APP_DIR, outDir: other, env: {} });
            expect(rebuilt.version).not.toBe(result.version);
            expect(rebuilt.networkOnly).toEqual([]);
        } finally {
            rmSync(other, { recursive: true, force: true });
        }
    }, 120_000);

    it('registers the lifecycle, fetch, message and push listeners', () => {
        const worker = load();
        expect([...worker.listeners.keys()].sort()).toEqual([
            'activate',
            'fetch',
            'install',
            'message',
            'notificationclick',
            'push',
            'pushsubscriptionchange',
        ]);
    });

    it('leaves the gateway, chain RPC, identity provider and attestor to the network', () => {
        const worker = load();
        expect(dispatchFetch(worker, 'http://127.0.0.1:3098/v1/wallet/me')).toBe(false);
        expect(dispatchFetch(worker, `${ORIGIN}/rpc`)).toBe(false);
        expect(dispatchFetch(worker, `${ORIGIN}/rpc/logo.png`)).toBe(false);
        expect(dispatchFetch(worker, 'http://127.0.0.1:3096/auth/v1/user')).toBe(false);
        expect(dispatchFetch(worker, `${ORIGIN}/attestor/health`)).toBe(false);
        expect(dispatchFetch(worker, `${ORIGIN}/api/health`)).toBe(false);
        expect(dispatchFetch(worker, `${ORIGIN}/_next/static/chunk.js`, 'POST')).toBe(false);
    });

    it('answers navigations and static assets of its own origin', () => {
        const worker = load();
        expect(dispatchFetch(worker, `${ORIGIN}/`, 'GET', 'navigate')).toBe(true);
        expect(dispatchFetch(worker, `${ORIGIN}/_next/static/chunks/main.js`)).toBe(true);
        expect(dispatchFetch(worker, `${ORIGIN}/icons/app/icon-192.png`)).toBe(true);
    });

    it('activates a waiting version only when the client asks for it', () => {
        const worker = load();
        const waits: Promise<unknown>[] = [];
        const message = (data: unknown) =>
            worker.listeners.get('message')?.({ data, waitUntil: (promise: Promise<unknown>) => waits.push(promise) });
        message({ type: 'PING' });
        message(null);
        expect(worker.skipWaitingCalls).toHaveLength(0);
        message({ type: 'SKIP_WAITING' });
        expect(worker.skipWaitingCalls).toHaveLength(1);
        expect(waits).toHaveLength(1);
    });
});
