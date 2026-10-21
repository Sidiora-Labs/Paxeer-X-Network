import { DEFAULT_ENVIRONMENT, type ThemeEnvironment } from './schema';

export interface ThemeEnvironmentSource {
    read: () => ThemeEnvironment;
    subscribe: (listener: () => void) => () => void;
}

export const COLOR_SCHEME_QUERY = '(prefers-color-scheme: light)';
export const REDUCED_MOTION_QUERY = '(prefers-reduced-motion: reduce)';

export class ThemeEnvironmentStore implements ThemeEnvironmentSource {
    private current: ThemeEnvironment;
    private readonly listeners = new Set<() => void>();

    constructor(initial: ThemeEnvironment = DEFAULT_ENVIRONMENT) {
        this.current = initial;
    }

    read = (): ThemeEnvironment => this.current;

    subscribe = (listener: () => void): (() => void) => {
        this.listeners.add(listener);
        return () => {
            this.listeners.delete(listener);
        };
    };

    set(next: ThemeEnvironment): void {
        if (next.colorScheme === this.current.colorScheme && next.reducedMotion === this.current.reducedMotion) return;
        this.current = next;
        this.listeners.forEach((listener) => listener());
    }
}

export function browserEnvironment(win: Window | undefined): ThemeEnvironmentSource {
    if (!win || typeof win.matchMedia !== 'function') return new ThemeEnvironmentStore(DEFAULT_ENVIRONMENT);
    const scheme = win.matchMedia(COLOR_SCHEME_QUERY);
    const motion = win.matchMedia(REDUCED_MOTION_QUERY);
    const store = new ThemeEnvironmentStore({
        colorScheme: scheme.matches ? 'light' : 'dark',
        reducedMotion: motion.matches,
    });
    const update = () => store.set({ colorScheme: scheme.matches ? 'light' : 'dark', reducedMotion: motion.matches });
    return {
        read: store.read,
        subscribe: (listener) => {
            const unsubscribe = store.subscribe(listener);
            scheme.addEventListener('change', update);
            motion.addEventListener('change', update);
            update();
            return () => {
                scheme.removeEventListener('change', update);
                motion.removeEventListener('change', update);
                unsubscribe();
            };
        },
    };
}
