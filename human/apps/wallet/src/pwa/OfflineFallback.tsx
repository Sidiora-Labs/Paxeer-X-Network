import { SHELL_URL } from './caching';

export function OfflineFallback() {
    return (
        <main className="min-h-screen flex flex-col items-center justify-center gap-4 px-6 text-center bg-pax-bg text-pax-light">
            <img src="/wallet/icons/app/icon-192.png" alt="" width={72} height={72} className="rounded-2xl" />
            <h1 className="text-xl font-semibold text-pax-off-white">You are offline</h1>
            <p className="max-w-xs text-sm text-pax-subtle">
                Paxeer Wallet needs a connection to read your balances and sign. Nothing was sent while you were offline.
            </p>
            <a
                href={SHELL_URL}
                className="rounded-xl bg-pax-accent px-5 py-2.5 text-sm font-semibold text-pax-on-accent"
            >
                Try again
            </a>
        </main>
    );
}
