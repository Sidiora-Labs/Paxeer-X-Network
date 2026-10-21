// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { InstallPrompt, type BeforeInstallPromptEvent, type InstallEnvironment } from './InstallPrompt';
import { OfflineFallback } from './OfflineFallback';
import { OFFLINE_URL, SHELL_URL } from './caching';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const DESKTOP = 'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36';
const IPHONE = 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1';

let root: Root | null = null;
let host: HTMLDivElement;

beforeEach(() => {
    host = document.createElement('div');
    document.body.appendChild(host);
});

afterEach(async () => {
    await act(async () => root?.unmount());
    root = null;
    host.remove();
});

function environment(userAgent: string, standalone = false): InstallEnvironment {
    return { target: new EventTarget(), standalone: () => standalone, userAgent };
}

async function mount(env: InstallEnvironment) {
    await act(async () => {
        root = createRoot(host);
        root.render(<InstallPrompt environment={env} />);
    });
}

function state(): string | null {
    return host.querySelector('[data-install-state]')?.getAttribute('data-install-state') ?? null;
}

function promptEvent(outcome: 'accepted' | 'dismissed') {
    const calls: string[] = [];
    const event = Object.assign(new Event('beforeinstallprompt', { cancelable: true }), {
        prompt: async () => {
            calls.push('prompt');
        },
        userChoice: Promise.resolve({ outcome }),
    }) as BeforeInstallPromptEvent;
    return { event, calls };
}

describe('InstallPrompt', () => {
    it('explains browser installation until the browser offers a prompt', async () => {
        await mount(environment(DESKTOP));
        expect(state()).toBe('unavailable');
        expect(host.querySelector('button')).toBeNull();
    });

    it('captures the browser prompt, shows an install button and installs on acceptance', async () => {
        const env = environment(DESKTOP);
        await mount(env);
        const { event, calls } = promptEvent('accepted');
        await act(async () => {
            env.target.dispatchEvent(event);
        });
        expect(event.defaultPrevented).toBe(true);
        expect(state()).toBe('available');
        const button = host.querySelector('button');
        expect(button?.textContent).toBe('Install app');
        await act(async () => {
            button?.click();
        });
        expect(calls).toEqual(['prompt']);
        expect(state()).toBe('installed');
    });

    it('drops the used prompt when the user dismisses it', async () => {
        const env = environment(DESKTOP);
        await mount(env);
        const { event, calls } = promptEvent('dismissed');
        await act(async () => {
            env.target.dispatchEvent(event);
        });
        await act(async () => {
            host.querySelector('button')?.click();
        });
        expect(calls).toEqual(['prompt']);
        expect(state()).toBe('unavailable');
    });

    it('reports installation when the browser announces it', async () => {
        const env = environment(DESKTOP);
        await mount(env);
        await act(async () => {
            env.target.dispatchEvent(promptEvent('accepted').event);
        });
        await act(async () => {
            env.target.dispatchEvent(new Event('appinstalled'));
        });
        expect(state()).toBe('installed');
    });

    it('shows the installed state when running standalone', async () => {
        await mount(environment(DESKTOP, true));
        expect(state()).toBe('installed');
    });

    it('gives the home screen steps on iOS, which has no install prompt', async () => {
        await mount(environment(IPHONE));
        expect(state()).toBe('ios');
        expect(host.textContent).toContain('Add to Home Screen');
    });

    it('stops listening once unmounted', async () => {
        const env = environment(DESKTOP);
        await mount(env);
        await act(async () => root?.unmount());
        root = null;
        const { event } = promptEvent('accepted');
        env.target.dispatchEvent(event);
        expect(event.defaultPrevented).toBe(false);
    });
});

describe('OfflineFallback', () => {
    it('renders the offline notice with a way back to the shell', () => {
        const markup = renderToStaticMarkup(<OfflineFallback />);
        expect(markup).toContain('You are offline');
        expect(markup).toContain(`href="${SHELL_URL}"`);
        expect(markup).toContain('/icons/app/icon-192.png');
        expect(OFFLINE_URL).toBe('/offline');
    });
});
