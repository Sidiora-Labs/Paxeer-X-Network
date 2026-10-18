'use client';

import Link from 'next/link';
import { ArrowUpRight } from 'lucide-react';
import { cn } from '@/lib/utils';
import { poolPath } from '@/core/app-foundation/navigation';
import { explorerTransactionUrl } from '@/core/clients/explorer-api';
import { Avatar, Tooltip } from '@/ui/primitives';
import { formatCompactUsd, formatTokenAmount, formatUsdl, relativeAge, shortAddress } from '@/utils/format';
import { ActionBadge } from './action-badge';
import type { ActivityEvent, ActivityValue } from './activity-transport';
import { MaskedAmount } from './balance-visibility';

/** Token identity resolved for one event: launchpad metadata first, then the explorer, else "Unknown". */
export interface ActivityTokenIdentity {
  name: string | null;
  symbol: string | null;
  logo: string | null;
  decimals: number | null;
  poolAddress: string | null;
}

export function signedTokenAmount(amountRaw: bigint, decimals: number | null, symbol: string | null): string {
  const sign = amountRaw < 0n ? '-' : amountRaw > 0n ? '+' : '';
  const abs = amountRaw < 0n ? -amountRaw : amountRaw;
  const body = decimals === null ? `${abs.toString()} raw` : formatTokenAmount(abs, decimals, { maxFraction: 2 });
  return `${sign}${body}${symbol ? ` ${symbol}` : ''}`;
}

export function activityValueText(value: ActivityValue | null): string {
  if (!value) return '—';
  return value.unit === 'usdl' ? formatUsdl(value.raw, { compact: true }) : formatCompactUsd(value.value);
}

function absoluteTime(ms: number): string {
  return new Date(ms).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
}

export function ActivityRow({ event, identity, now }: { event: ActivityEvent; identity: ActivityTokenIdentity; now: number }) {
  const known = !!identity.name;
  const title = identity.name ?? 'Unknown';
  const subtitle = known ? identity.symbol ?? shortAddress(event.tokenAddress) : shortAddress(event.tokenAddress);
  const inflow = event.amountRaw > 0n;
  const explorer = explorerTransactionUrl(event.txHash);
  const coin = (
    <>
      <Avatar src={identity.logo} name={identity.symbol ?? title} size="lg" shape="square" decorative />
      <span className="min-w-0">
        <span className="block truncate text-sm font-bold text-text">{title}</span>
        <span className={cn('block truncate text-xs text-text-faint', !known || !identity.symbol ? 'font-mono' : null)}>{subtitle}</span>
      </span>
    </>
  );
  const time = (
    <Tooltip content={absoluteTime(event.occurredAt)}>
      <time dateTime={new Date(event.occurredAt).toISOString()} className="text-xs text-text-muted tabular-nums">{relativeAge(event.occurredAt, now)} ago</time>
    </Tooltip>
  );

  return (
    <tr className="group h-table-row-comfortable transition-colors duration-fast hover:bg-surface-hover">
      <td className="max-w-0 py-2 pl-3 pr-2 sm:w-[38%]">
        {identity.poolAddress ? (
          <Link href={poolPath(125, identity.poolAddress)} className="flex min-w-0 items-center gap-3 rounded-sm">{coin}</Link>
        ) : (
          <span className="flex min-w-0 items-center gap-3">{coin}</span>
        )}
      </td>
      <td className="px-2 py-2">
        <span className="flex flex-col items-start gap-1">
          <ActionBadge kind={event.kind} />
          <span className="sm:hidden">{time}</span>
        </span>
      </td>
      <td className="px-2 py-2 text-right">
        <MaskedAmount className={cn('block whitespace-nowrap text-sm font-bold tabular-nums', inflow ? 'text-success' : event.amountRaw < 0n ? 'text-danger' : 'text-text')}>
          {signedTokenAmount(event.amountRaw, identity.decimals, identity.symbol)}
        </MaskedAmount>
        <MaskedAmount className="block whitespace-nowrap text-xs text-text-faint tabular-nums">{activityValueText(event.value)}</MaskedAmount>
      </td>
      <td className="hidden px-2 py-2 text-right sm:table-cell">{time}</td>
      <td className="w-10 py-2 pl-1 pr-2 text-right">
        {explorer ? (
          <a
            href={explorer}
            target="_blank"
            rel="noopener noreferrer"
            aria-label="View transaction on Paxeer explorer, opens in new tab"
            className="inline-flex size-control-height-sm items-center justify-center rounded-sm text-text-muted transition-colors duration-fast hover:bg-surface-active hover:text-text focus-visible:outline-2 focus-visible:outline-focus"
          >
            <ArrowUpRight aria-hidden="true" className="size-icon-sm" />
          </a>
        ) : null}
      </td>
    </tr>
  );
}
