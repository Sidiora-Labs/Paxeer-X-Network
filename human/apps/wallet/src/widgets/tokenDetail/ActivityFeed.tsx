'use client';

/**
 * Token-scoped activity feed.
 *
 * Filters the wallet's full tx history down to rows that touch this token —
 * native transactions for PAX, token-transfer rows matching the contract
 * address otherwise. Renders skeleton placeholders while loading.
 */

import { formatBalance, shortenAddress } from '@/lib/format';
import { SvgIcon } from '@/components/ui/SvgIcon';
import { SectionLabel } from './Atoms';
import type { TxHistoryRow } from '@/lib/queries';

const SENT_FILTER =
  'invert(48%) sepia(79%) saturate(2476%) hue-rotate(338deg) brightness(118%) contrast(119%)';
const RECEIVED_FILTER =
  'invert(69%) sepia(61%) saturate(588%) hue-rotate(88deg) brightness(93%) contrast(93%)';

export interface ActivityFeedProps {
  rows: TxHistoryRow[];
  walletAddress: string;
  loading: boolean;
  onTxDetail?: (hash: string) => void;
}

export function ActivityFeed({ rows, walletAddress, loading, onTxDetail }: ActivityFeedProps) {
  const addr = walletAddress.toLowerCase();

  return (
    <>
      <div className="col-span-2 px-1 pt-1">
        <SectionLabel text="Activity" />
      </div>
      {loading ? (
        <div className="col-span-2 space-y-2">
          {[1, 2, 3].map((i) => (
            <div key={i} className="h-14 shimmer rounded-xl" />
          ))}
        </div>
      ) : rows.length === 0 ? (
        <div className="col-span-2 bg-pax-surface rounded-[20px] py-10 text-center">
          <p className="text-xs text-pax-muted">No activity yet</p>
        </div>
      ) : (
        <div className="col-span-2 bg-pax-surface rounded-[20px]  divide-white/[0.04] overflow-hidden">
          {rows.map((row, i) => {
            const isSent = row.fromAddress.toLowerCase() === addr;
            return (
              <button
                key={row.hash + i}
                onClick={() => onTxDetail?.(row.hash)}
                className="w-full flex items-center gap-3 px-4 py-3.5 press-scale text-left transition-colors"
              >
                <div
                  className={`w-9 h-9 rounded-full flex items-center justify-center shrink-0 ${
                    isSent ? 'bg-pax-error/10' : 'bg-pax-success/10'
                  }`}
                >
                  <SvgIcon
                    name={isSent ? 'arrow-up-right' : 'arrow-down-left'}
                    className="w-4 h-4"
                    style={{ filter: isSent ? SENT_FILTER : RECEIVED_FILTER }}
                  />
                </div>
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-semibold">{isSent ? 'Sent' : 'Received'}</p>
                  <p className="text-[11px] text-pax-muted truncate">
                    {isSent
                      ? `To ${shortenAddress(row.toAddress)}`
                      : `From ${shortenAddress(row.fromAddress)}`}
                  </p>
                </div>
                <p
                  className={`text-xs font-semibold shrink-0 ${
                    isSent ? 'text-pax-error' : 'text-pax-success'
                  }`}
                >
                  {isSent ? '-' : '+'}
                  {formatBalance(row.amountRaw, row.decimals, 4)} {row.symbol}
                </p>
              </button>
            );
          })}
        </div>
      )}
    </>
  );
}
