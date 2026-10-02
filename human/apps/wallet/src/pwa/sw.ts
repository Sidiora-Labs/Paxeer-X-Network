import {
    OFFLINE_URL,
    PRECACHE_URLS,
    cacheNames,
    staleCaches,
    strategyFor,
} from './caching';
import { MANIFEST_ICONS } from './manifest';

declare const __PAXEER_SW__: { readonly version: string; readonly networkOnly: readonly string[] };

interface WorkerExtendableEvent extends Event {
    waitUntil(promise: Promise<unknown>): void;
}

interface WorkerFetchEvent extends WorkerExtendableEvent {
    readonly request: Request;
    respondWith(response: Promise<Response>): void;
}

interface WorkerMessageEvent extends WorkerExtendableEvent {
    readonly data: unknown;
}

interface WorkerPushEvent extends WorkerExtendableEvent {
    readonly data: { json(): unknown; text(): string } | null;
}

interface WorkerNotificationEvent extends WorkerExtendableEvent {
    readonly notification: Notification;
}

interface WorkerSubscriptionChangeEvent extends WorkerExtendableEvent {
    readonly oldSubscription: PushSubscription | null;
}

interface WorkerWindowClient {
    readonly url: string;
    postMessage(message: unknown): void;
    focus(): Promise<unknown>;
}

interface WorkerScope {
    readonly location: Location;
    readonly registration: ServiceWorkerRegistration;
    readonly clients: {
        claim(): Promise<void>;
        matchAll(options?: { type?: string; includeUncontrolled?: boolean }): Promise<WorkerWindowClient[]>;
        openWindow(url: string): Promise<unknown>;
    };
    skipWaiting(): Promise<void>;
    addEventListener(type: 'install' | 'activate', listener: (event: WorkerExtendableEvent) => void): void;
    addEventListener(type: 'fetch', listener: (event: WorkerFetchEvent) => void): void;
    addEventListener(type: 'message', listener: (event: WorkerMessageEvent) => void): void;
    addEventListener(type: 'push', listener: (event: WorkerPushEvent) => void): void;
    addEventListener(type: 'notificationclick', listener: (event: WorkerNotificationEvent) => void): void;
    addEventListener(type: 'pushsubscriptionchange', listener: (event: WorkerSubscriptionChangeEvent) => void): void;
}

interface PushPayload {
    title?: string;
    body?: string;
    icon?: string;
    badge?: string;
    image?: string;
    url?: string;
    tag?: string;
    requireInteraction?: boolean;
}

const scope = self as unknown as WorkerScope;
const names = cacheNames(__PAXEER_SW__.version);
const networkOnly = __PAXEER_SW__.networkOnly;
const notificationIcon = MANIFEST_ICONS[0].src;

async function precache(): Promise<void> {
    const cache = await caches.open(names.shell);
    await cache.addAll(PRECACHE_URLS.map((url) => new Request(url, { cache: 'reload' })));
}

async function purge(): Promise<void> {
    const existing = await caches.keys();
    await Promise.all(staleCaches(existing, names).map((name) => caches.delete(name)));
    await scope.clients.claim();
}

async function navigate(request: Request): Promise<Response> {
    try {
        return await fetch(request);
    } catch (error) {
        const shell = await caches.open(names.shell);
        const offline = await shell.match(OFFLINE_URL);
        if (offline) return offline;
        throw error;
    }
}

async function cacheFirst(request: Request): Promise<Response> {
    const cache = await caches.open(names.static);
    const cached = (await cache.match(request)) ?? (await caches.open(names.shell).then((shell) => shell.match(request)));
    if (cached) return cached;
    const response = await fetch(request);
    if (response.ok && response.type === 'basic') {
        await cache.put(request, response.clone());
    }
    return response;
}

function notificationTarget(raw: unknown): string {
    if (typeof raw !== 'string' || raw.length > 2048) return '/wallet/';
    try {
        const candidate = new URL(raw, scope.location.origin);
        if (
            candidate.origin === scope.location.origin &&
            candidate.pathname === '/wallet/' &&
            !candidate.username &&
            !candidate.password
        ) {
            return `${candidate.pathname}${candidate.search}`;
        }
    } catch {
        return '/wallet/';
    }
    return '/wallet/';
}

scope.addEventListener('install', (event) => {
    event.waitUntil(precache());
});

scope.addEventListener('activate', (event) => {
    event.waitUntil(purge());
});

scope.addEventListener('fetch', (event) => {
    const request = event.request;
    const strategy = strategyFor(
        { url: request.url, method: request.method, mode: request.mode },
        { origin: scope.location.origin, networkOnly },
    );
    if (strategy === 'navigation') {
        event.respondWith(navigate(request));
    } else if (strategy === 'cache-first') {
        event.respondWith(cacheFirst(request));
    }
});

scope.addEventListener('message', (event) => {
    const data = event.data as { type?: unknown } | null;
    if (data?.type === 'SKIP_WAITING') {
        event.waitUntil(scope.skipWaiting());
    }
});

scope.addEventListener('push', (event) => {
    let payload: PushPayload = { title: 'Paxeer Wallet', body: 'You have a new notification.' };
    if (event.data) {
        try {
            payload = event.data.json() as PushPayload;
        } catch {
            payload = { ...payload, body: event.data.text() };
        }
    }
    event.waitUntil(
        scope.registration.showNotification(payload.title || 'Paxeer Wallet', {
            body: payload.body || '',
            icon: payload.icon || notificationIcon,
            badge: payload.badge || notificationIcon,
            data: { url: payload.url || '/wallet/' },
            tag: payload.tag || 'paxeer-default',
            requireInteraction: payload.requireInteraction || false,
        }),
    );
});

scope.addEventListener('notificationclick', (event) => {
    event.notification.close();
    const target = notificationTarget((event.notification.data as { url?: unknown } | null)?.url);
    event.waitUntil(
        scope.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clients) => {
            for (const client of clients) {
                if (new URL(client.url).origin === scope.location.origin && new URL(client.url).pathname.startsWith('/wallet/')) {
                    client.postMessage({ type: 'PAXPORT_NAVIGATE', route: target });
                    return client.focus();
                }
            }
            return scope.clients.openWindow(target);
        }),
    );
});

scope.addEventListener('pushsubscriptionchange', (event) => {
    const previous = event.oldSubscription;
    if (!previous) return;
    event.waitUntil(
        scope.registration.pushManager.subscribe(previous.options).then(async (subscription) => {
            const clients = await scope.clients.matchAll();
            for (const client of clients) {
                client.postMessage({ type: 'PUSH_SUBSCRIPTION_CHANGED', subscription: subscription.toJSON() });
            }
        }),
    );
});
