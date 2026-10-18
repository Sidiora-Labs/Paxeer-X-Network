'use client';
import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { SearchX } from 'lucide-react';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import { PoolNotFoundError, type PoolSnapshot } from '@/core/market/pool-resolver';
import { Button, EmptyState, Skeleton } from '@/ui/primitives';
import TerminalFrame from './frame';
import { terminalPoolPath } from './sub-nav';
import TerminalLeftRail from './left-rail';
import TerminalTokenStrip from './token-strip';
import TerminalChartCard from './chart-card';
import TerminalActivityTabs from './activity-tabs';
import TerminalRightRail from './right-rail';
import TerminalStatusBar from './status-bar';
import { recordRecentPool } from './recent-pools';
import { TerminalLiveProvider } from './live-context';

export { TerminalLiveProvider, useTerminalLive } from './live-context';

export type CoinTarget =
  | { kind: 'pending' }
  | { kind: 'render'; pool: string }
  | { kind: 'redirect'; pool: string }
  | { kind: 'not-found' };

/**
 * What the coin view does with the requested address once the pool resolver
 * (usePoolSnapshot) has answered: a token address redirects to its pool, an
 * unknown address is "not found", any other resolver failure still renders
 * the pool so every panel shows its own real error.
 */
export function coinTarget(requested: string, query: { status: 'pending' | 'error' | 'success'; data?: PoolSnapshot; error?: unknown }): CoinTarget {
  if (query.status === 'pending') return { kind: 'pending' };
  if (query.status === 'error') return query.error instanceof PoolNotFoundError ? { kind: 'not-found' } : { kind: 'render', pool: requested };
  const pool = query.data?.pool.poolAddress.toLowerCase() ?? requested;
  return pool === requested ? { kind: 'render', pool } : { kind: 'redirect', pool };
}

/**
 * The target to show while the resolver refetches. A refetch of a snapshot
 * that failed without data reports `pending` again (every panel that mounts
 * its own observer triggers one), so once a target has settled it is kept
 * until the resolver settles on a different one.
 */
export function heldCoinTarget(settled: CoinTarget, current: CoinTarget): CoinTarget {
  return current.kind === 'pending' ? settled : current;
}

function sameTarget(a: CoinTarget, b: CoinTarget): boolean {
  return a.kind === b.kind && ('pool' in a ? a.pool : null) === ('pool' in b ? b.pool : null);
}

function CenterSkeleton() {
  return (
    <div aria-busy="true" className="flex flex-col gap-1">
      <Skeleton variant="block" height={52} label="pair header" className="rounded-md" />
      <Skeleton variant="block" height={420} label="chart" className="rounded-md" />
      <Skeleton variant="rows" rows={6} label="trades" className="rounded-md bg-surface-1" />
    </div>
  );
}

/** The terminal coin view: discovery rail, token strip + chart + activity, position and trading rail, status bar. */
export default function TerminalCoinView({ poolAddress }: { poolAddress: string }) {
  const requested = poolAddress.toLowerCase();
  const router = useRouter();
  const snapshot = usePoolSnapshot(requested);
  const current = coinTarget(requested, snapshot);
  const [settled, setSettled] = useState<CoinTarget>(current);
  if (current.kind !== 'pending' && !sameTarget(current, settled)) setSettled(current);
  const target = heldCoinTarget(settled, current);

  const redirectTo = target.kind === 'redirect' ? target.pool : null;
  useEffect(() => {
    if (redirectTo) router.replace(terminalPoolPath(redirectTo));
  }, [redirectTo, router]);

  const pool = target.kind === 'render' ? target.pool : null;
  useEffect(() => {
    if (pool) recordRecentPool(pool);
  }, [pool]);

  const left = <TerminalLeftRail activePool={pool ?? requested} />;
  const status = <TerminalStatusBar poolAddress={pool} />;

  if (target.kind === 'not-found') {
    return (
      <TerminalFrame
        left={left}
        center={(
          <div className="rounded-md bg-surface-1">
            <EmptyState
              variant="panel"
              icon={SearchX}
              title="Coin not found"
              body={`No coin matches ${requested}. Check the address and try again.`}
              action={<Button href="/terminal" variant="secondary" size="sm">Back to trending</Button>}
              announce
            />
          </div>
        )}
        status={status}
      />
    );
  }

  if (pool === null) {
    return <TerminalFrame left={left} center={<CenterSkeleton />} right={<Skeleton variant="rows" rows={8} label="position" />} status={status} />;
  }

  return (
    <TerminalLiveProvider poolAddress={pool}>
      <TerminalFrame
        left={left}
        center={(
          <>
            <TerminalTokenStrip poolAddress={pool} />
            <TerminalChartCard poolAddress={pool} />
            <TerminalActivityTabs poolAddress={pool} />
          </>
        )}
        right={<TerminalRightRail poolAddress={pool} />}
        status={status}
      />
    </TerminalLiveProvider>
  );
}
