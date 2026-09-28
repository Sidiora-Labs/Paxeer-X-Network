'use client';

import { useCallback, useEffect, useState } from 'react';

export interface BeforeInstallPromptEvent extends Event {
    prompt(): Promise<void>;
    readonly userChoice: Promise<{ readonly outcome: 'accepted' | 'dismissed' }>;
}

export type InstallState = 'installed' | 'available' | 'ios' | 'unavailable';

export interface InstallEnvironment {
    readonly target: EventTarget;
    readonly standalone: () => boolean;
    readonly userAgent: string;
}

export function browserInstallEnvironment(): InstallEnvironment | null {
    if (typeof window === 'undefined') return null;
    return {
        target: window,
        standalone: () =>
            (typeof window.matchMedia === 'function' && window.matchMedia('(display-mode: standalone)').matches) ||
            (navigator as Navigator & { standalone?: boolean }).standalone === true,
        userAgent: navigator.userAgent,
    };
}

function isIos(userAgent: string): boolean {
    return /iPad|iPhone|iPod/.test(userAgent);
}

export function useInstallPrompt(environment: InstallEnvironment | null) {
    const [deferred, setDeferred] = useState<BeforeInstallPromptEvent | null>(null);
    const [installed, setInstalled] = useState(false);
    const [pending, setPending] = useState(false);

    useEffect(() => {
        if (!environment) return;
        setInstalled(environment.standalone());
        const onPrompt = (event: Event) => {
            event.preventDefault();
            setDeferred(event as BeforeInstallPromptEvent);
        };
        const onInstalled = () => {
            setInstalled(true);
            setDeferred(null);
        };
        environment.target.addEventListener('beforeinstallprompt', onPrompt);
        environment.target.addEventListener('appinstalled', onInstalled);
        return () => {
            environment.target.removeEventListener('beforeinstallprompt', onPrompt);
            environment.target.removeEventListener('appinstalled', onInstalled);
        };
    }, [environment]);

    const install = useCallback(async () => {
        if (!deferred) return false;
        setPending(true);
        try {
            await deferred.prompt();
            const choice = await deferred.userChoice;
            setDeferred(null);
            if (choice.outcome === 'accepted') setInstalled(true);
            return choice.outcome === 'accepted';
        } finally {
            setPending(false);
        }
    }, [deferred]);

    let state: InstallState = 'unavailable';
    if (installed) state = 'installed';
    else if (deferred) state = 'available';
    else if (environment && isIos(environment.userAgent)) state = 'ios';

    return { state, install, pending };
}

export function InstallPrompt({ environment }: { environment?: InstallEnvironment | null }) {
    const [resolved] = useState<InstallEnvironment | null>(() =>
        environment === undefined ? browserInstallEnvironment() : environment,
    );
    const { state, install, pending } = useInstallPrompt(resolved);

    return (
        <div className="px-4 py-3" data-install-state={state}>
            {state === 'installed' && (
                <p className="text-sm text-pax-success">Paxeer Wallet is installed on this device.</p>
            )}
            {state === 'available' && (
                <button
                    type="button"
                    onClick={() => void install()}
                    disabled={pending}
                    className="w-full py-2.5 rounded-xl bg-pax-accent text-pax-on-accent text-sm font-semibold press-scale disabled:opacity-60"
                >
                    {pending ? 'Installing…' : 'Install app'}
                </button>
            )}
            {state === 'ios' && (
                <p className="text-sm text-pax-subtle">
                    Tap Share in Safari, then Add to Home Screen, to install Paxeer Wallet.
                </p>
            )}
            {state === 'unavailable' && (
                <p className="text-sm text-pax-subtle">
                    Your browser offers installation from its menu once this site meets its install criteria.
                </p>
            )}
        </div>
    );
}
