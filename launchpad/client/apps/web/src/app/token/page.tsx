'use client';

import WalletGate from '@/ui/shared/wallet-gate';
import MyTokenView from '@/widgets/my-token/my-token-view';

export default function MyTokenPage() {
  return (
    <WalletGate message="Connect your wallet to manage your coins">
      <MyTokenView />
    </WalletGate>
  );
}
