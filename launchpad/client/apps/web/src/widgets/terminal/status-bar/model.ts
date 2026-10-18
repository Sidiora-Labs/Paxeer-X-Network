import type { StreamState, V2ContractTypes } from '@kindle/sdk';
import type { freshness } from '@/page-modules/token/market-presentation';

/** Window event other terminal packages dispatch to open a status-bar panel: `new CustomEvent(terminalOpenPanelEvent, { detail: 'alerts' })`. */
export const terminalOpenPanelEvent = 'sidiora:terminal-open-panel';
/** Window event the status-bar gear and the token-strip settings dispatch; the terminal frame resets its column widths. */
export const terminalResetColumnsEvent = 'sidiora:terminal-reset-columns';

export const terminalPanels = ['watchlist', 'wallets', 'pnl', 'feed', 'alpha', 'alerts', 'trending', 'callouts'] as const;
export type TerminalPanel = typeof terminalPanels[number];

export function isTerminalPanel(value: unknown): value is TerminalPanel {
  return typeof value === 'string' && (terminalPanels as readonly string[]).includes(value);
}

export type Freshness = ReturnType<typeof freshness>;
export type ConnectionLabel = 'Stable' | 'Reconnecting' | 'Offline';
export interface ConnectionStatus { label: ConnectionLabel; tone: 'success' | 'warning' | 'danger'; detail: string }

/**
 * Status pill mapping. Without an open pair only the browser's network flag is
 * known; with a pair the market stream state and its freshness decide.
 */
export function connectionStatus(input: { online: boolean; stream: StreamState | null; freshness: Freshness | null }): ConnectionStatus {
  if (!input.online) return { label: 'Offline', tone: 'danger', detail: "You're offline. Check your internet connection." };
  if (input.stream === null) return { label: 'Stable', tone: 'success', detail: "You're online." };
  if (input.stream === 'closed') return { label: 'Offline', tone: 'danger', detail: 'Live updates for this coin are off.' };
  if (input.stream === 'live' && (input.freshness === 'current' || input.freshness === 'delayed')) {
    return { label: 'Stable', tone: 'success', detail: input.freshness === 'delayed' ? 'Live, prices may be a little behind.' : 'Live and up to date.' };
  }
  return { label: 'Reconnecting', tone: 'warning', detail: 'Reconnecting to live updates...' };
}

export type AlertDirection = 'crosses_above' | 'crosses_below';

/**
 * Exact decimal USDL price (per whole token) as an integer ratio for the
 * contract's InputPrice: "0.00012" -> 12 / 100000. Null when the text is not
 * a positive decimal with at most 18 fractional digits.
 */
export function priceRatio(text: string): { numerator: string; denominator: string } | null {
  const match = /^(\d{1,30})(?:\.(\d{1,18}))?$/.exec(text.trim());
  if (!match) return null;
  const fraction = match[2] ?? '';
  const digits = (match[1] + fraction).replace(/^0+/, '');
  if (digits === '') return null;
  return { numerator: digits, denominator: (10n ** BigInt(fraction.length)).toString() };
}

export const alertCooldowns = [60, 300, 900, 3600] as const;

export type AlertCreateBody = V2ContractTypes['AlertCreateBody'];

/**
 * IF_ALERT_CREATE body for a user-entered USDL threshold. The price is a
 * display target typed by the user, so its source is `external_display` /
 * `provider_display` with no chain position.
 */
export function alertCreateBody(input: { poolAddress: string; tokenAddress: string; tokenDecimals: number; usdlAddress: string; direction: AlertDirection; price: string; cooldownSeconds: number; observedAt: Date }): AlertCreateBody | null {
  const ratio = priceRatio(input.price);
  if (!ratio) return null;
  return {
    poolAddress: input.poolAddress.toLowerCase(),
    direction: input.direction,
    cooldownSeconds: input.cooldownSeconds,
    threshold: {
      numerator: ratio.numerator,
      denominator: ratio.denominator,
      baseAsset: { kind: 'erc20', chainId: '125', address: input.tokenAddress.toLowerCase(), currencyCode: null, decimals: input.tokenDecimals },
      quoteAsset: { kind: 'erc20', chainId: '125', address: input.usdlAddress.toLowerCase(), currencyCode: 'USDL', decimals: 6 },
      source: { kind: 'external_display', amountBasis: 'provider_display', observedAt: input.observedAt.toISOString(), position: null, providerId: null, sourceEventId: null, sourceReference: null },
    },
  };
}
