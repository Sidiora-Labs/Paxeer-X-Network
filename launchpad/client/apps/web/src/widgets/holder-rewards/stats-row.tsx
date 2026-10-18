'use client';

import type { ReactNode } from 'react';
import type { Loadable } from '@/core/market/unsupported';
import { cn } from '@/lib/utils';
import { ErrorState, StateNotice } from '@/ui/primitives';
import type { HolderRewardsStatsResponse } from './holder-rewards-contract';
import { usdlAmountText } from './holder-rewards-presentation';
import { technicalDetail } from '@/core/user-facing-error';

export interface StatsRowProps {
  stats: Loadable<HolderRewardsStatsResponse>;
}

type TileState =
  | { kind: 'loading' }
  | { kind: 'value'; value: string; unit?: string; caption?: string }
  | { kind: 'unavailable'; title: string; plural?: boolean };

const numeral = 'text-[36px] font-bold leading-[40px] tracking-[-.01em] [font-variant-numeric:proportional-nums] sm:text-[44px] sm:leading-[48px]';

function DisplayTile({ label, state, tone = 'text', detail, onRetry, className }: { label: string; state: TileState; tone?: 'text' | 'success'; detail: string | null; onRetry?: () => void; className?: string }) {
  let body: ReactNode;
  if (state.kind === 'loading') {
    body = <span aria-hidden="true" className="sl-skeleton inline-block h-[0.8em] w-40 max-w-full align-middle" />;
  } else if (state.kind === 'unavailable') {
    // Holds the numeral slot at display height so the bar keeps its shape; no number is invented.
    body = <span aria-label="Not available yet" className="text-text-faint">{'—'}</span>;
  } else {
    body = (
      <>
        <span className={tone === 'success' ? 'text-success' : 'text-text'}>{state.value}</span>
        {state.unit ? <span className="ml-1.5 text-md font-semibold tracking-normal text-text-faint">{state.unit}</span> : null}
      </>
    );
  }
  return (
    <div className={cn('flex min-w-0 flex-col gap-2 rounded-md bg-surface-2 px-5 py-4', className)}>
      <dt className="text-2xs font-semibold uppercase tracking-[.1em] text-text-faint">{label}</dt>
      <dd data-numeric className={cn('truncate', numeral)} aria-busy={state.kind === 'loading' || undefined}>{body}</dd>
      {state.kind === 'unavailable' ? (
        <dd className="flex min-w-0"><StateNotice kind="unavailable" surface={2} title={state.title} plural={state.plural} detail={detail} onRetry={onRetry} /></dd>
      ) : state.kind === 'value' && state.caption ? (
        <dd className="text-xs text-text-faint">{state.caption}</dd>
      ) : null}
    </div>
  );
}

/**
 * Three display-size protocol tiles from IF_HOLDER_REWARDS_STATS.
 *
 * "At today's prices": rewards are paid in USDL and USDL is the unit every
 * price on Paxeer is quoted in, so the cumulative amount valued at today's
 * price is that same USDL amount. It is derived from the served total and
 * shares its availability: when the total is absent the tile renders the
 * shared unavailable state, never an estimate.
 */
export default function StatsRow({ stats }: StatsRowProps) {
  const loading = stats.state === 'loading';
  const data = stats.state === 'ready' ? stats.data.data : null;
  // Developer detail only (attributes outside production); the pill copy never carries codes.
  const detail = stats.state === 'unsupported' ? technicalDetail(stats.operation, stats.status) : null;
  const onRetry = stats.state === 'error' ? stats.retry : undefined;
  const tile = (value: string | null, missing: { title: string; plural?: boolean }, extra?: { unit?: string; caption?: string }): TileState =>
    loading ? { kind: 'loading' } : value === null ? { kind: 'unavailable', ...missing } : { kind: 'value', value, ...extra };
  const distributed = data ? usdlAmountText(data.cumulativeDistributedRaw) : null;
  return (
    <section aria-labelledby="holder-rewards-stats-title" className="flex flex-col gap-2">
      <h2 id="holder-rewards-stats-title" className="sr-only">Holder rewards totals</h2>
      <dl className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
        <DisplayTile label="Holder rewards" tone="success" detail={detail} onRetry={onRetry}
          state={tile(distributed, { title: 'Holder rewards', plural: true }, { unit: 'USDL' })} />
        <DisplayTile label="At today's prices" detail={detail} onRetry={onRetry}
          state={tile(distributed, { title: "Today's value" }, { unit: 'USDL', caption: 'Valued in USDL' })} />
        <DisplayTile label="Wallets rewarded" className="sm:col-span-2 lg:col-span-1" detail={detail} onRetry={onRetry}
          state={tile(data ? data.walletsRewarded.toLocaleString('en-US') : null, { title: 'Wallet count' })} />
      </dl>
      {stats.state === 'error' ? (
        <ErrorState variant="chip" message="Totals can't load right now" onRetry={stats.retry} className="self-start" />
      ) : data ? (
        <p className="px-1 text-xs text-text-faint">
          Updated <time dateTime={data.generatedAt}>{new Date(data.generatedAt).toLocaleString()}</time>
        </p>
      ) : null}
    </section>
  );
}
