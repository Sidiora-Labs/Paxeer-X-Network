'use client';
import WalletGate from '@/ui/shared/wallet-gate';
import RewardsView from '@/widgets/rewards/rewards-view';
export default function RewardsModule() {
  return (
    <WalletGate message="Connect wallet to see your rewards">
      <RewardsView />
    </WalletGate>
  );
}
