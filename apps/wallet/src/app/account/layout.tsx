'use client';

import { useMemo, type ReactNode } from 'react';
import { useRouter } from 'next/navigation';
import { UniversalHeader } from '@/components/nav/UniversalHeader';
import { AccountProvider } from '@/account/AccountProvider';
import { resolveAccountConfig } from '@/account/config';
import { IdentitySession } from '@/wallet/identity';
import { resolveWalletConfig } from '@/wallet/config';
import { WalletProvider } from '@/wallet/WalletProvider';

export default function AccountLayout({ children }: { children: ReactNode }) {
    const router = useRouter();
    const wallet = useMemo(() => resolveWalletConfig(), []);
    const account = useMemo(() => resolveAccountConfig(), []);
    const identity = useMemo(
        () => (wallet.ok && typeof window !== 'undefined' ? IdentitySession.create(wallet.config) : null),
        [wallet],
    );
    const walletConfig = wallet.ok ? wallet.config : null;
    const authorization = useMemo(
        () => (identity ? async () => (await identity.gatewayToken()) ?? null : undefined),
        [identity],
    );

    return (
        <WalletProvider config={walletConfig} identity={identity}>
            <div className="mx-auto min-h-screen max-w-md bg-pax-bg text-pax-light">
                <UniversalHeader title="Account" showBack onBack={() => router.back()} />
                <main className="space-y-4 p-4">
                    {account.ok ? (
                        <AccountProvider config={account.config} authorization={authorization}>
                            {children}
                        </AccountProvider>
                    ) : (
                        <p role="alert" className="rounded-2xl bg-red-950 p-4 text-sm text-red-50">
                            {account.error.message}
                        </p>
                    )}
                </main>
            </div>
        </WalletProvider>
    );
}
