'use client';

/**
 * Activity widget — 2-tab view for native transactions and token transfers.
 *
 * Replaces the legacy `TransactionsPage`. Data flows through
 * `useTxHistoryQuery` instead of an ad-hoc `useEffect` so the response is
 * cached, deduped on focus refetch, and shareable across screens.
 */

import { useState } from 'react';
import { useWalletState } from '@/providers/WalletProvider';
import { EmptyState } from '@/components/ui/EmptyState';
import { TransactionListSkeleton } from '@/components/ui/Skeletons';
import { ErrorBanner } from '@/components/ui/NetworkErrorScreen';
import { useTxHistoryQuery } from '@/lib/queries';
import { TransactionRow } from './TransactionRow';

type ActivityTab = 'tx' | 'transfers';

export interface TransactionsWidgetProps {
  onTxDetail?: (hash: string) => void;
}

const EMPTY_CONFIG: Record<ActivityTab, { title: string; subtitle: string }> = {
  tx: { title: 'No transactions yet', subtitle: 'Your on-chain activity will appear here' },
  transfers: { title: 'No token transfers yet', subtitle: 'Token sends and receives will appear here' },
};

export function TransactionsWidget({ onTxDetail }: TransactionsWidgetProps) {
  const { activeAccount } = useWalletState();
  const [tab, setTab] = useState<ActivityTab>('tx');

  const txQuery = useTxHistoryQuery(activeAccount?.address);
  const loading = txQuery.isPending;
  const hasError = txQuery.isError;
  const transactions = txQuery.data?.transactions ?? [];
  const transfers = txQuery.data?.transfers ?? [];

  const walletAddress = activeAccount?.address ?? '';
  const rows = tab === 'tx' ? transactions : transfers;

  return (
    <div className="px-4 pt-4 pb-4">
      {/* 2-tab bar */}
      <div className="flex gap-1 p-1 rounded-xl bg-white/5 mb-4">
        {(
          [
            { key: 'tx' as const, label: 'TX' },
            { key: 'transfers' as const, label: 'Transfers' },
          ]
        ).map(({ key, label }) => (
          <button
            key={key}
            onClick={() => setTab(key)}
            className={`flex-1 py-2 rounded-lg text-xs font-medium transition-all ${
              tab === key ? 'bg-pax-accent/15 text-pax-accent' : 'text-pax-muted'
            }`}
          >
            {label}
          </button>
        ))}
      </div>

      {hasError && (
        <ErrorBanner
          message="Could not load activity. Showing cached data."
          onRetry={() => txQuery.refetch()}
        />
      )}

      {loading ? (
        <TransactionListSkeleton rows={8} />
      ) : rows.length === 0 ? (
        <EmptyState
          icon="activity"
          title={EMPTY_CONFIG[tab].title}
          subtitle={EMPTY_CONFIG[tab].subtitle}
        />
      ) : (
        <div className="glass-card  divide-white/5">
          {rows.map((row, i) => (
            <TransactionRow
              key={row.hash + i}
              row={row}
              walletAddress={walletAddress}
              onClick={onTxDetail}
              showSymbolInLabel={tab === 'transfers'}
            />
          ))}
        </div>
      )}
    </div>
  );
}
