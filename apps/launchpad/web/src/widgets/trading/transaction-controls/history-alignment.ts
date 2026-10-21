import type { V2ContractTypes } from '@kindle/sdk';

/**
 * True when an IF_TRADES page proves a confirmed swap reached authoritative
 * history: a healthy watermark at or past the receipt height and a row for the
 * transaction. core/api serves every IF_TRADES page with quality 'partial'
 * (core/api/internal/rest/accounting_v2.go EncodeCachePage), so only stale or
 * quarantined pages are refused.
 */
export function tradeHistoryAligned(history: V2ContractTypes['TradesResponse'], receiptHeight: bigint, txHash: string): boolean {
  if (history.quality === 'stale' || history.quality === 'quarantined' || history.watermark?.integrity !== 'healthy') return false;
  if (BigInt(history.watermark.height) < receiptHeight) return false;
  const hash = txHash.toLowerCase();
  return history.data.items.some(trade => trade.position.transactionHash.toLowerCase() === hash);
}
