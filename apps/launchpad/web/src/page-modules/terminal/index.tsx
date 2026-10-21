'use client';
import dynamic from 'next/dynamic';
import type { ComponentType } from 'react';
import { EmptyState, Button, Skeleton } from '@/ui/primitives';
import TerminalFrame from '@/widgets/terminal/frame';
import TerminalCoinView from '@/widgets/terminal/coin-view';
import TerminalStatusBar from '@/widgets/terminal/status-bar';
import TerminalTrendingView from '@/widgets/terminal/views/trending';
import { parseTerminalSlug, type TerminalTabView } from '@/widgets/terminal/sub-nav';

function ViewSkeleton() {
  return <Skeleton variant="rows" rows={10} label="terminal view" className="rounded-md bg-surface-1" />;
}

const TerminalTrenchesView = dynamic(() => import('@/widgets/terminal/views/trenches'), { ssr: false, loading: ViewSkeleton });
const TerminalTrackView = dynamic(() => import('@/widgets/terminal/views/track'), { ssr: false, loading: ViewSkeleton });
const TerminalPortfolioView = dynamic(() => import('@/widgets/terminal/views/portfolio'), { ssr: false, loading: ViewSkeleton });
const TerminalRewardsView = dynamic(() => import('@/widgets/terminal/views/rewards'), { ssr: false, loading: ViewSkeleton });

const views: Record<TerminalTabView, ComponentType> = {
  trending: TerminalTrendingView,
  trenches: TerminalTrenchesView,
  track: TerminalTrackView,
  portfolio: TerminalPortfolioView,
  rewards: TerminalRewardsView,
};

/** /terminal (trending), /terminal/<view> and /terminal/<pool>; the app route validates the slug first. */
export default function TerminalModule({ slug }: { slug?: string }) {
  const target = parseTerminalSlug(slug);
  if (target.kind === 'pool') return <TerminalCoinView key={target.pool} poolAddress={target.pool} />;
  const status = <TerminalStatusBar poolAddress={null} />;
  if (target.kind === 'invalid') {
    return (
      <TerminalFrame
        center={<div className="rounded-md bg-surface-1"><EmptyState variant="panel" title="Page not found" body="Open a coin by its address or pick a view from the tabs above." action={<Button href="/terminal" variant="secondary" size="sm">Back to trending</Button>} /></div>}
        status={status}
      />
    );
  }
  const View = views[target.view];
  return <TerminalFrame center={<View />} status={status} />;
}
