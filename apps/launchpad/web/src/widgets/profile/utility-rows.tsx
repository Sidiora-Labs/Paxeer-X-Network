'use client';

import { useState, type ReactNode } from 'react';
import Link from 'next/link';
import { Coins, History } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { queryKeys } from '@/core/query-keys';
import { marketCachePolicy } from '@/core/cache-policy';
import type { V2ContractTypes } from '@kindle/sdk';
import type { ConnectedDiscoveryCallout } from '@kindle/sdk-v2';
import { poolPath } from '@/core/app-foundation/navigation';
import { CORE_DEPLOYMENT } from '@/core/network/deployment';
import { isNotServedError, isSignedOutError, loadable, unsupported, type Loadable } from '@/core/market/unsupported';
import { readRetryFields } from '@/core/transient-retry';
import { dataApiUrl, pnlApiUrl } from '@/core/sdk-config';
import { Button, EmptyState, ErrorState, Skeleton, UnavailableState } from '@/ui/primitives';
import { formatUsdl, relativeAge, shortAddress } from '@/utils/format';
import { accountRequest } from './account-transport';
import { MaskedAmount } from './balance-visibility';
import { useAccountSession } from './use-account-session';
import { usePrivateAccountQuery } from './use-private-account-query';

export interface UtilityRowsProps {
  wallet: string;
  isOwner: boolean;
}

type RewardsResponse = V2ContractTypes['RewardsResponse'];
type Reward = V2ContractTypes['Reward'];

const CASHBACK_PROGRAM = 'cashback';
const cashbackOperation = 'IF_REWARDS GET /v2/me/rewards?programId=cashback';
const calloutsOperation = 'GET /v2/discovery/callouts?author={wallet} (per-user callout history)';
const USDL = CORE_DEPLOYMENT.usdl.toLowerCase();

function UtilityRow({ icon, title, caption, action, children }: { icon: ReactNode; title: string; caption: ReactNode; action: ReactNode; children?: ReactNode }) {
  return (
    <li className="flex flex-col gap-2 rounded-md bg-surface-2 p-3">
      <div className="flex items-center gap-3">
        <span aria-hidden="true" className="inline-flex size-avatar-md shrink-0 items-center justify-center rounded-sm bg-surface-3 text-text-muted [&>svg]:size-icon-md">{icon}</span>
        <div className="min-w-0 flex-1">
          <p className="text-sm font-bold text-text">{title}</p>
          <div className="text-xs text-text-muted">{caption}</div>
        </div>
        <div className="shrink-0">{action}</div>
      </div>
      {children}
    </li>
  );
}

/* ------------------------------------------------------------- cashback */

interface CashbackTotal { assetAddress: string; claimableRaw: bigint }

function cashbackTotals(items: readonly Reward[]): CashbackTotal[] {
  const totals = new Map<string, bigint>();
  for (const row of items) {
    if (row.programId !== CASHBACK_PROGRAM || !/^\d+$/.test(row.claimableRaw)) continue;
    const asset = row.assetAddress.toLowerCase();
    totals.set(asset, (totals.get(asset) ?? 0n) + BigInt(row.claimableRaw));
  }
  return [...totals].map(([assetAddress, claimableRaw]) => ({ assetAddress, claimableRaw }));
}

function cashbackAmount(total: CashbackTotal): string {
  return total.assetAddress === USDL ? formatUsdl(total.claimableRaw) : `${total.claimableRaw.toString()} raw units of ${shortAddress(total.assetAddress)}`;
}

function CashbackRow({ wallet }: { wallet: string }) {
  const auth = useAccountSession();
  const query = usePrivateAccountQuery('rewards-cashback', () => accountRequest<RewardsResponse>(pnlApiUrl(`/v2/me/rewards?programId=${CASHBACK_PROGRAM}&limit=50`)), [], wallet);
  const icon = <Coins />;
  const title = 'Cashback';

  if (!query.owner) {
    return (
      <UtilityRow
        icon={icon}
        title={title}
        caption="Sign in to check your cashback"
        action={<Button size="sm" variant="secondary" loading={auth.authenticating} disabled={auth.authenticating} onClick={() => { void auth.authenticate(); }}>Sign in</Button>}
      />
    );
  }
  const state = loadable({ status: query.status, data: query.data, error: query.error, refetch: query.refetch }, cashbackOperation);
  if (state.state === 'loading') {
    return <UtilityRow icon={icon} title={title} caption={<Skeleton width={160} height={12} label="cashback balance" />} action={<Button size="sm" variant="primary" disabled>Claim</Button>} />;
  }
  if (state.state === 'unsupported') {
    return <UtilityRow icon={icon} title={title} caption={<UnavailableState variant="inline" title="Cashback" operation={state.operation} status={state.status} className="text-xs" />} action={<Button size="sm" variant="secondary" onClick={() => { void query.refetch(); }}>Try again</Button>} />;
  }
  if (state.state === 'error' && isSignedOutError(state.error)) {
    return (
      <UtilityRow
        icon={icon}
        title={title}
        caption="Sign in again to check your cashback"
        action={<Button size="sm" variant="secondary" loading={auth.authenticating} disabled={auth.authenticating} onClick={() => { void auth.authenticate(); }}>Sign in</Button>}
      />
    );
  }
  if (state.state === 'error') {
    return <UtilityRow icon={icon} title={title} caption={<ErrorState variant="inline" message="Can't load cashback right now" />} action={<Button size="sm" variant="secondary" onClick={state.retry}>Try again</Button>} />;
  }
  const totals = cashbackTotals(state.data.data.items);
  if (totals.length === 0) {
    return <UtilityRow icon={icon} title={title} caption="No cashback yet. Cashback from your trades shows up here." action={<Button size="sm" variant="primary" disabled>Claim</Button>} />;
  }
  const claimable = totals.some(total => total.claimableRaw > 0n);
  return (
    <UtilityRow
      icon={icon}
      title={title}
      caption={<>Claimable {totals.map((total, index) => <MaskedAmount key={total.assetAddress} className="font-bold text-text tabular-nums">{index > 0 ? ', ' : ''}{cashbackAmount(total)}</MaskedAmount>)}{state.data.nextCursor ? ' (first 50)' : ''}</>}
      action={<Button size="sm" variant="primary" disabled={!claimable} href={claimable ? `/profile/${wallet.toLowerCase()}?tab=rewards` : undefined}>Claim</Button>}
    />
  );
}

/* --------------------------------------------------------- callout history */

/** A failed callout read with the envelope's retry fields, so transient-retry and isNotServedError can read them. */
class CalloutReadError extends Error {
  constructor(readonly status: number, readonly retryable: boolean | null = null, readonly retryAfterMs: number | null = null) {
    super(`core/api answered ${status} for the callout history`);
    this.name = 'CalloutReadError';
  }
}

function isCallout(value: unknown): value is ConnectedDiscoveryCallout {
  if (typeof value !== 'object' || value === null) return false;
  const row = value as Record<string, unknown>;
  return typeof row.ID === 'string' && typeof row.PoolAddress === 'string' && typeof row.Direction === 'string' && typeof row.CreatedAt === 'string' && typeof row.Deleted === 'boolean';
}

async function readCalloutHistory(wallet: string, signal?: AbortSignal): Promise<ConnectedDiscoveryCallout[]> {
  const response = await fetch(dataApiUrl(`/v2/discovery/callouts?author=${wallet}`), { signal, cache: 'no-store', credentials: 'omit', headers: { accept: 'application/json' } });
  if (!response.ok) {
    const { retryable, retryAfterMs } = await readRetryFields(response);
    throw new CalloutReadError(response.status, retryable, retryAfterMs);
  }
  const body = (await response.json()) as { data?: { items?: unknown } | null; items?: unknown } | null;
  // An empty list may arrive as null (Go nil slice) in either envelope: that is no callouts, not a shape error.
  const items = body?.data ? body.data.items ?? [] : body?.items ?? [];
  if (!Array.isArray(items) || !items.every(isCallout)) throw new Error('Callout history response does not match the callout shape');
  return items.filter(item => !item.Deleted);
}

function calloutState(query: ReturnType<typeof useCalloutHistory>): Loadable<ConnectedDiscoveryCallout[]> {
  // 405 (no read route) and a non-retryable 503 (no backing service, e.g. development) both mean "not served": the unavailable state, never an error.
  if (query.status === 'error' && query.error instanceof CalloutReadError && (query.error.status === 405 || isNotServedError(query.error))) {
    return unsupported(calloutsOperation, query.error.status, 'Check back in a bit');
  }
  return loadable(query, calloutsOperation);
}

function useCalloutHistory(wallet: string, enabled: boolean) {
  return useQuery({
    queryKey: queryKeys.calloutHistory(wallet),
    queryFn: ({ signal }) => readCalloutHistory(wallet.toLowerCase(), signal),
    enabled,
    ...marketCachePolicy.calloutHistory,
  });
}

function CalloutHistoryRow({ wallet }: { wallet: string }) {
  const [open, setOpen] = useState(false);
  const query = useCalloutHistory(wallet, open);
  const state = calloutState(query);
  const panelId = `callout-history-${wallet.toLowerCase()}`;
  let detail: ReactNode = null;
  if (open) {
    if (state.state === 'loading') detail = <Skeleton variant="rows" rows={2} label="callout history" />;
    else if (state.state === 'unsupported') detail = <UnavailableState variant="panel" title="Callout history" operation={state.operation} status={state.status} message="Check back in a bit" onRetry={() => { void query.refetch(); }} className="p-4" />;
    else if (state.state === 'error') detail = <ErrorState variant="inline" message="Can't load your callouts right now" />;
    else if (state.data.length === 0) detail = <EmptyState icon={History} title="No callouts yet" body="Post a callout on a coin and track how it runs here" action={<Button href="/explore" variant="secondary" size="sm">Browse coins</Button>} className="p-4" />;
    else {
      const now = Date.now();
      detail = (
        <ul className="flex flex-col gap-1">
          {state.data.map(callout => (
            <li key={callout.ID} className="flex items-center justify-between gap-3 rounded-sm bg-surface-3 px-3 py-2 text-sm">
              {/^0x[0-9a-fA-F]{40}$/.test(callout.PoolAddress)
                ? <Link href={poolPath(125, callout.PoolAddress)} className="truncate font-mono text-text hover:text-accent">{shortAddress(callout.PoolAddress)}</Link>
                : <span className="truncate font-mono text-text-muted">{callout.PoolAddress}</span>}
              <span className="shrink-0 text-xs text-text-muted">{callout.Direction} · {relativeAge(callout.CreatedAt, now)} ago</span>
            </li>
          ))}
        </ul>
      );
    }
  }
  return (
    <UtilityRow
      icon={<History />}
      title="Callout history"
      caption="How the coins you called have done since"
      action={<Button size="sm" variant="secondary" aria-expanded={open} aria-controls={panelId} onClick={() => setOpen(value => !value)}>{open ? 'Hide' : 'View'}</Button>}
    >
      <div id={panelId} hidden={!open} className="rounded-sm bg-surface-1 p-2">{detail}</div>
    </UtilityRow>
  );
}

/** Self-only utility rows above the activity ledger: callout history and cashback. Visitors see nothing. */
export function UtilityRows({ wallet, isOwner }: UtilityRowsProps) {
  if (!isOwner) return null;
  return (
    <ul aria-label="Cashback and callouts" className="flex flex-col gap-1 rounded-lg bg-surface-1 p-1">
      <CalloutHistoryRow wallet={wallet} />
      <CashbackRow wallet={wallet} />
    </ul>
  );
}
