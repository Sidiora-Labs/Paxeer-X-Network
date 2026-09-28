import { afterEach, describe, expect, it } from 'vitest';
import {
    registerServiceWorker,
    type ContainerLike,
    type RegistrationLike,
    type ServiceWorkerHandle,
    type WorkerLike,
} from './register';

class Worker extends EventTarget implements WorkerLike {
    state = 'installing';
    readonly messages: unknown[] = [];

    postMessage(message: unknown): void {
        this.messages.push(message);
    }

    moveTo(state: string): void {
        this.state = state;
        this.dispatchEvent(new Event('statechange'));
    }
}

class Registration extends EventTarget implements RegistrationLike {
    waiting: Worker | null = null;
    installing: Worker | null = null;
    updates = 0;

    async update(): Promise<void> {
        this.updates += 1;
    }

    beginInstall(worker: Worker): void {
        this.installing = worker;
        this.dispatchEvent(new Event('updatefound'));
    }
}

class Container extends EventTarget implements ContainerLike {
    controller: Worker | null = null;
    readonly registrations: { url: string; options: unknown }[] = [];

    constructor(readonly registration: Registration) {
        super();
    }

    async register(url: string, options: { scope: string; updateViaCache: 'none' }): Promise<Registration> {
        this.registrations.push({ url, options });
        return this.registration;
    }

    activate(worker: Worker): void {
        this.controller = worker;
        this.dispatchEvent(new Event('controllerchange'));
    }
}

class Visibility extends EventTarget {
    visibilityState = 'hidden';

    show(): void {
        this.visibilityState = 'visible';
        this.dispatchEvent(new Event('visibilitychange'));
    }
}

const handles: ServiceWorkerHandle[] = [];

afterEach(() => {
    while (handles.length > 0) handles.pop()?.dispose();
});

async function setup(controlled: boolean) {
    const registration = new Registration();
    const container = new Container(registration);
    if (controlled) {
        const current = new Worker();
        current.state = 'activated';
        container.controller = current;
    }
    const visibility = new Visibility();
    const events = { updates: 0, reloads: 0 };
    const handle = await registerServiceWorker(container, {
        onUpdateReady: () => {
            events.updates += 1;
        },
        reload: () => {
            events.reloads += 1;
        },
        visibility,
        intervalMs: 3_600_000,
    });
    if (!handle) throw new Error('registration returned no handle');
    handles.push(handle);
    return { registration, container, visibility, events, handle };
}

describe('registerServiceWorker', () => {
    it('returns nothing where the browser has no service worker container', async () => {
        await expect(registerServiceWorker(undefined, { onUpdateReady: () => undefined, reload: () => undefined })).resolves.toBeNull();
    });

    it('registers the built worker at the root scope bypassing the HTTP cache', async () => {
        const { container } = await setup(false);
        expect(container.registrations).toEqual([{ url: '/sw.js', options: { scope: '/', updateViaCache: 'none' } }]);
    });

    it('does not offer a reload for the first install of an uncontrolled page', async () => {
        const { registration, events, handle } = await setup(false);
        const first = new Worker();
        registration.beginInstall(first);
        first.moveTo('installed');
        expect(events.updates).toBe(0);
        expect(handle.updateReady).toBe(false);
        expect(handle.applyUpdate()).toBe(false);
    });

    it('offers a reload when a new worker finishes installing and reloads once after it takes control', async () => {
        const { registration, container, events, handle } = await setup(true);
        const next = new Worker();
        registration.beginInstall(next);
        expect(events.updates).toBe(0);
        next.moveTo('installed');
        next.moveTo('installed');
        expect(events.updates).toBe(1);
        expect(handle.updateReady).toBe(true);

        expect(handle.applyUpdate()).toBe(true);
        expect(next.messages).toEqual([{ type: 'SKIP_WAITING' }]);
        expect(events.reloads).toBe(0);
        next.moveTo('activated');
        container.activate(next);
        container.activate(next);
        expect(events.reloads).toBe(1);
    });

    it('offers a reload for a worker already waiting at registration', async () => {
        const registration = new Registration();
        registration.waiting = new Worker();
        registration.waiting.state = 'installed';
        const container = new Container(registration);
        const current = new Worker();
        current.state = 'activated';
        container.controller = current;
        let offered = 0;
        const handle = await registerServiceWorker(container, {
            onUpdateReady: () => {
                offered += 1;
            },
            reload: () => undefined,
        });
        if (!handle) throw new Error('registration returned no handle');
        handles.push(handle);
        expect(offered).toBe(1);
        expect(handle.applyUpdate()).toBe(true);
        expect(registration.waiting.messages).toEqual([{ type: 'SKIP_WAITING' }]);
    });

    it('does not reload on a controller change the user did not ask for', async () => {
        const { container, events } = await setup(true);
        container.activate(new Worker());
        expect(events.reloads).toBe(0);
    });

    it('checks for an update on demand and whenever the page becomes visible', async () => {
        const { registration, visibility, handle } = await setup(true);
        await handle.checkForUpdate();
        expect(registration.updates).toBe(1);
        visibility.show();
        await Promise.resolve();
        expect(registration.updates).toBe(2);
        handle.dispose();
        visibility.show();
        await Promise.resolve();
        expect(registration.updates).toBe(2);
    });
});
