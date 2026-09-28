'use client';

import { HistoryView } from '@/account/HistoryView';
import { useWallet } from '@/wallet/WalletProvider';

export default function AccountHistoryPage() {
    const wallet = useWallet();
    if (wallet.status !== 'ready' || !wallet.address) {
        return <p className="text-sm text-pax-muted">Sign in to see the unified history.</p>;
    }
    return <HistoryView account={wallet.address} />;
}
