'use client';

/**
 * Bento layout for token holdings.
 *
 * Layout rules:
 * - Native PAX:        tall card spanning 2 rows (left column)
 * - 1st & 2nd holding: compact cards (right column)
 * - 3rd holding:       wide card (full width, breaks the grid up)
 * - 4+ holdings:       compact cards in pairs
 *
 * Filtering happens upstream — this widget just renders what it receives.
 */

import { formatBalance } from '@/lib/format';
import { PAX_ICON_URL } from '@/lib/constants';
import { BentoTokenTall, BentoTokenCompact, BentoTokenWide } from './BentoTokenCard';

export interface HoldingsGridHolding {
  contract_address?: string;
  symbol?: string | null;
  name?: string | null;
  balance_raw?: string | null;
  decimals?: number | null;
  value_usd?: string | number | null;
  icon_url?: string | null;
}

export interface HoldingsGridProps {
  hidden: boolean;
  loading: boolean;
  nativeBalance: string;
  nativeValueUsd: number;
  holdings: HoldingsGridHolding[];
  allHoldingsCount: number;
  onTokenDetail?: (tokenId: string, symbol?: string) => void;
}

const placeholder = '••••';

export function HoldingsGrid({
  hidden,
  loading,
  nativeBalance,
  nativeValueUsd,
  holdings,
  allHoldingsCount,
  onTokenDetail,
}: HoldingsGridProps) {
  const hiddenCount = allHoldingsCount - holdings.length;

  return (
    <>
      <BentoTokenTall
        symbol="PAX"
        name="Paxeer"
        balance={hidden ? placeholder : nativeBalance}
        valueUsd={hidden ? null : nativeValueUsd}
        iconUrl={PAX_ICON_URL}
        loading={loading}
        onClick={() => onTokenDetail?.('pax', 'PAX')}
      />

      {holdings.slice(0, 2).map((h) => (
        <BentoTokenCompact
          key={h.contract_address}
          symbol={h.symbol || '???'}
          name={h.name || 'Unknown'}
          balance={hidden ? placeholder : formatBalance(h.balance_raw || '0', h.decimals || 18, 4)}
          valueUsd={hidden ? null : h.value_usd != null ? Number(h.value_usd) : null}
          iconUrl={h.icon_url || null}
          onClick={() => onTokenDetail?.(h.contract_address || '', h.symbol || '???')}
        />
      ))}

      {holdings.length === 0 && (
        <>
          <div className="bg-pax-surface rounded-[20px] p-4 flex items-center justify-center min-h-[110px]">
            <p className="text-xs text-pax-muted">No tokens yet</p>
          </div>
          <div />
        </>
      )}
      {holdings.length === 1 && <div />}

      {holdings.length > 2 && (
        <BentoTokenWide
          symbol={holdings[2].symbol || '???'}
          name={holdings[2].name || 'Unknown'}
          balance={
            hidden
              ? placeholder
              : formatBalance(holdings[2].balance_raw || '0', holdings[2].decimals || 18, 4)
          }
          valueUsd={
            hidden
              ? null
              : holdings[2].value_usd != null
                ? Number(holdings[2].value_usd)
                : null
          }
          iconUrl={holdings[2].icon_url || null}
          onClick={() =>
            onTokenDetail?.(holdings[2].contract_address || '', holdings[2].symbol || '???')
          }
        />
      )}

      {holdings.slice(3).map((h) => (
        <BentoTokenCompact
          key={h.contract_address}
          symbol={h.symbol || '???'}
          name={h.name || 'Unknown'}
          balance={hidden ? placeholder : formatBalance(h.balance_raw || '0', h.decimals || 18, 4)}
          valueUsd={hidden ? null : h.value_usd != null ? Number(h.value_usd) : null}
          iconUrl={h.icon_url || null}
          onClick={() => onTokenDetail?.(h.contract_address || '', h.symbol || '???')}
        />
      ))}

      {hiddenCount > 0 && holdings.length > 0 && (
        <div className="col-span-2 text-center text-[11px] text-pax-muted py-1">
          {hiddenCount} token{hiddenCount > 1 ? 's' : ''} hidden by filter
        </div>
      )}
      {!loading && holdings.length === 0 && allHoldingsCount > 0 && (
        <div className="col-span-2 text-center text-xs text-pax-muted py-4">
          All {allHoldingsCount} tokens hidden by filters
        </div>
      )}
    </>
  );
}
