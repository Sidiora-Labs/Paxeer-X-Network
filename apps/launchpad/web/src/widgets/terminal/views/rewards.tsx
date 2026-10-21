'use client';

import WalletGate from '@/ui/shared/wallet-gate';
import RewardsView from '@/widgets/rewards/rewards-view';

/** Rewards view: the same entitlement and claim flow as /rewards, rendered inside the terminal frame. */
export default function TerminalRewardsView() {
  return (
    <div className="h-full min-h-0 overflow-y-auto p-2">
      <WalletGate message="Connect your wallet to view rewards">
        <RewardsView headingLevel={2} />
      </WalletGate>
    </div>
  );
}
