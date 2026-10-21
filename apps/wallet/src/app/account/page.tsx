'use client';

import Link from 'next/link';
import { AccountView } from '@/account/AccountView';
import { AssetList } from '@/account/AssetList';
import { useWallet } from '@/wallet/WalletProvider';

const LINKS = [
    { href: '/account/history', label: 'History' },
    { href: '/account/deposit', label: 'Deposit' },
    { href: '/account/plan', label: 'Move value' },
] as const;

export default function AccountPage() {
    const wallet = useWallet();
    if (wallet.status !== 'ready' || !wallet.address) {
        return <p className="text-sm text-pax-muted">Sign in to see the unified account.</p>;
    }
    return (
        <>
            <AccountView account={wallet.address} />
            <AssetList account={wallet.address} />
            <nav aria-label="Account" className="grid grid-cols-3 gap-2">
                {LINKS.map((link) => (
                    <Link key={link.href} href={link.href} className="rounded-xl bg-white/[0.06] px-3 py-3 text-center text-xs font-semibold">
                        {link.label}
                    </Link>
                ))}
            </nav>
        </>
    );
}
