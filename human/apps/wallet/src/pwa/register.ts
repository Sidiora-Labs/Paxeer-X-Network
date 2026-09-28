import { SERVICE_WORKER_URL } from './caching';

export const UPDATE_CHECK_INTERVAL_MS = 60 * 60 * 1000;

export interface WorkerLike extends EventTarget {
    readonly state: string;
    postMessage(message: unknown): void;
}

export interface RegistrationLike extends EventTarget {
    readonly waiting: WorkerLike | null;
    readonly installing: WorkerLike | null;
    update(): Promise<unknown>;
}

export interface ContainerLike extends EventTarget {
    readonly controller: WorkerLike | null;
    register(url: string, options: { scope: string; updateViaCache: 'none' }): Promise<RegistrationLike>;
}

export interface RegisterOptions {
    readonly onUpdateReady: () => void;
    readonly reload: () => void;
    readonly intervalMs?: number;
    readonly visibility?: EventTarget & { readonly visibilityState: string };
}

export interface ServiceWorkerHandle {
    readonly registration: RegistrationLike;
    readonly updateReady: boolean;
    checkForUpdate(): Promise<void>;
    applyUpdate(): boolean;
    dispose(): void;
}

export async function registerServiceWorker(
    container: ContainerLike | undefined,
    options: RegisterOptions,
): Promise<ServiceWorkerHandle | null> {
    if (!container) return null;
    const registration = await container.register(SERVICE_WORKER_URL, { scope: '/', updateViaCache: 'none' });
    let waiting: WorkerLike | null = null;
    let reloadRequested = false;
    let reloaded = false;

    const offer = (worker: WorkerLike) => {
        if (waiting === worker) return;
        waiting = worker;
        options.onUpdateReady();
    };

    const track = (worker: WorkerLike) => {
        const onState = () => {
            if (worker.state === 'installed' && container.controller) offer(worker);
            if (worker.state === 'redundant' || worker.state === 'activated') {
                worker.removeEventListener('statechange', onState);
            }
        };
        worker.addEventListener('statechange', onState);
        onState();
    };

    const onUpdateFound = () => {
        if (registration.installing) track(registration.installing);
    };

    const onControllerChange = () => {
        if (!reloadRequested || reloaded) return;
        reloaded = true;
        options.reload();
    };

    const checkForUpdate = async () => {
        await registration.update();
    };

    const onVisibility = () => {
        if (options.visibility?.visibilityState === 'visible') void checkForUpdate().catch(() => undefined);
    };

    registration.addEventListener('updatefound', onUpdateFound);
    container.addEventListener('controllerchange', onControllerChange);
    options.visibility?.addEventListener('visibilitychange', onVisibility);
    const timer = setInterval(() => {
        void checkForUpdate().catch(() => undefined);
    }, options.intervalMs ?? UPDATE_CHECK_INTERVAL_MS);

    if (registration.waiting && container.controller) offer(registration.waiting);
    if (registration.installing) track(registration.installing);

    return {
        registration,
        get updateReady() {
            return waiting !== null;
        },
        checkForUpdate,
        applyUpdate() {
            if (!waiting) return false;
            reloadRequested = true;
            waiting.postMessage({ type: 'SKIP_WAITING' });
            return true;
        },
        dispose() {
            clearInterval(timer);
            registration.removeEventListener('updatefound', onUpdateFound);
            container.removeEventListener('controllerchange', onControllerChange);
            options.visibility?.removeEventListener('visibilitychange', onVisibility);
        },
    };
}
