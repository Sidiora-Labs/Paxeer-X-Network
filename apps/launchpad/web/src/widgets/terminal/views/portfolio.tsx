'use client';

import { useState } from 'react';
import { Wallet } from 'lucide-react';
import { openAuthModal } from '@/shell/auth-modal-store';
import { useAccountSession } from '@/widgets/profile/use-account-session';
import { BalanceVisibilityProvider, BalanceVisibilityToggle } from '@/widgets/profile/balance-visibility';
import { PortfolioBlock } from '@/widgets/profile/portfolio-block';
import { PnlSummary } from '@/widgets/profile/pnl-summary';
import { TopTrades } from '@/widgets/profile/top-trades';
import { PositionsOpenClosed } from '@/widgets/profile/positions-open-closed';
import { Button, EmptyState, TabStrip } from '@/ui/primitives';

export type PortfolioPositionsTab = 'open' | 'closed';

export const portfolioTabs: { id: PortfolioPositionsTab; label: string }[] = [
  { id: 'open', label: 'Open positions' },
  { id: 'closed', label: 'Closed positions' },
];

const idPrefix = 'terminal-portfolio';

/** Portfolio view: the signed-in account's own portfolio blocks (value, PnL, top trades, positions) inside the terminal. */
export default function TerminalPortfolioView() {
  const { session } = useAccountSession();
  const wallet = session?.session.wallet.toLowerCase() ?? null;
  const [tab, setTab] = useState<PortfolioPositionsTab>('open');
  if (!wallet) {
    return (
      <div className="flex h-full items-center justify-center rounded-md bg-surface-1">
        <EmptyState icon={Wallet} title="Sign in to see your portfolio" body="Positions, value and PnL are private to your account and load once you sign in with your wallet." action={<Button variant="primary" size="sm" onClick={() => openAuthModal('Sign in to see your portfolio')}>Sign in</Button>} />
      </div>
    );
  }
  return (
    <BalanceVisibilityProvider>
      <div className="flex h-full min-h-0 flex-col gap-1 overflow-y-auto">
        <div className="flex h-10 shrink-0 items-center justify-between rounded-md bg-surface-1 px-3">
          <h1 className="text-sm font-bold text-text">Portfolio</h1>
          <BalanceVisibilityToggle />
        </div>
        <div className="grid shrink-0 grid-cols-1 gap-1 xl:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
          <div className="flex min-w-0 flex-col gap-1">
            <PortfolioBlock wallet={wallet} isOwner />
            <PnlSummary wallet={wallet} isOwner />
          </div>
          <TopTrades wallet={wallet} />
        </div>
        <section aria-label="Positions" className="flex flex-col rounded-md bg-surface-1">
          <TabStrip<PortfolioPositionsTab> tabs={portfolioTabs} active={tab} onChange={setTab} label="Positions" idPrefix={idPrefix} className="px-2" />
          <div id={`${idPrefix}-panel-${tab}`} role="tabpanel" aria-labelledby={`${idPrefix}-tab-${tab}`} className="p-2">
            <PositionsOpenClosed key={tab} wallet={wallet} mode={tab} isOwner />
          </div>
        </section>
      </div>
    </BalanceVisibilityProvider>
  );
}
