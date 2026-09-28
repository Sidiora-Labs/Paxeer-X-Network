'use client';

/**
 * One activity row — used for both native txns and token transfers.
 *
 * Direction (sent vs received) is derived by comparing `fromAddress` to the
 * active wallet address passed in. The row is rendered as a button so the
 * parent can link out to a tx-detail view.
 */

import { shortenAddress, formatBalance } from '@/lib/format';
import { SvgIcon } from '@/components/ui/SvgIcon';
import type { TxHistoryRow } from '@/lib/queries';

const SENT_FILTER =
  'invert(48%) sepia(79%) saturate(2476%) hue-rotate(338deg) brightness(118%) contrast(119%)';
const RECEIVED_FILTER =
  'invert(69%) sepia(61%) saturate(588%) hue-rotate(88deg) brightness(93%) contrast(93%)';

export interface TransactionRowProps {
  row: TxHistoryRow;
  walletAddress: string;
  onClick?: (hash: string) => void;
  /** Native txns label as "Sent"/"Received"; transfers prefix the symbol. */
  showSymbolInLabel?: boolean;
}

export function TransactionRow({
  row,
  walletAddress,
  onClick,
  showSymbolInLabel = false,
}: TransactionRowProps) {
  const addr = walletAddress.toLowerCase();
  const isSent = row.fromAddress.toLowerCase() === addr;
  const formattedAmount = formatBalance(row.amountRaw, row.decimals, 4);
  const dateLabel = row.timestamp ? new Date(row.timestamp).toLocaleDateString() : '';

  const directionLabel = isSent ? 'Sent' : 'Received';
  const fullLabel = showSymbolInLabel ? `${directionLabel} ${row.symbol}` : directionLabel;
  const counterparty = isSent ? row.toAddress : row.fromAddress;

  return (
    <button
      onClick={() => onClick?.(row.hash)}
      className="w-full flex items-center gap-3 px-4 py-3.5 press-scale text-left hover:bg-white/[0.02]"
    >
      <div
        className={`w-9 h-9 rounded-full flex items-center justify-center shrink-0 ${
          isSent ? 'bg-red-500/10' : 'bg-green-500/10'
        }`}
      >
        <SvgIcon
          name={isSent ? 'arrow-up-right' : 'arrow-down-left'}
          className="w-4 h-4"
          style={{ filter: isSent ? SENT_FILTER : RECEIVED_FILTER }}
        />
      </div>
      <div className="flex-1 min-w-0">
        <p className="text-sm font-medium">{fullLabel}</p>
        <p className="text-xs text-pax-muted truncate">
          {isSent ? `To ${shortenAddress(counterparty)}` : `From ${shortenAddress(counterparty)}`}
        </p>
      </div>
      <div className="text-right shrink-0">
        <p className={`text-sm font-medium ${isSent ? 'text-red-400' : 'text-green-400'}`}>
          {isSent ? '-' : '+'}
          {formattedAmount} {row.symbol}
        </p>
        {dateLabel && <p className="text-[10px] text-pax-muted">{dateLabel}</p>}
      </div>
    </button>
  );
}
