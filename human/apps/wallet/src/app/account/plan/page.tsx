'use client';

import { KernelPlan } from '@/account/KernelPlan';
import { useWallet } from '@/wallet/WalletProvider';

export default function AccountPlanPage() {
    const wallet = useWallet();
    if (wallet.status !== 'ready' || !wallet.address) {
        return <p className="text-sm text-pax-muted">Sign in to move value across the network.</p>;
    }
    return <KernelPlan account={wallet.address} />;
}
