import type { Token, Pool, PoolStats, RankingsResponse, HoldersResponse, Trade, Watermark } from '../../../../../../sdk/launchpad/packages/production-contracts/src/types.gen';
export type { Token, Pool, PoolStats, RankingsResponse, HoldersResponse, Trade, Watermark };

// Read-only presentation: never use rounded output for wallet submission.
export function exactUnits(raw: string | null, decimals: number | null, places = 6): string {
  if(raw === null || decimals === null) return '—';
  if(!/^(0|[1-9][0-9]{0,511})$/.test(raw) || !Number.isInteger(decimals) || decimals < 0 || decimals > 255 || !Number.isInteger(places) || places < 0 || places > 18) throw new RangeError('Invalid bounded amount');
  const padded = raw.padStart(decimals + 1, '0');
  const whole = decimals ? padded.slice(0, -decimals) : padded;
  const fraction = decimals ? padded.slice(-decimals) : '';
  const kept = fraction.slice(0, places).replace(/0+$/, '');
  const approximate = /[1-9]/.test(fraction.slice(places));
  return `${approximate ? '≈' : ''}${whole}${kept ? '.' + kept : ''}`;
}
export function tokenAmount(raw: string, token: Token): string {
  return exactUnits(raw, token.decimalsQuality === 'verified' ? token.decimals : null);
}
/** Liveness window: the SDK pings every 4 s (STREAM_PING_INTERVAL_MS) and each reply is a heartbeat, so 10 s tolerates one lost reply plus jitter. */
export const HEARTBEAT_LIVENESS_MS = 10_000;
/** A stream with no heartbeat (or snapshot) for this long is stale rather than reconnecting. */
export const LIVENESS_STALE_MS = 30_000;
/**
 * Stream freshness. Liveness comes from the heartbeat (the reply to the SDK's
 * {"op":"ping"}, or a checkpoint) on a contiguous live subscription; the
 * per-topic watermark and snapshot time only bound data age (see dataAgeMs),
 * so an idle pool on a healthy stream stays 'current'.
 *  - unavailable  no snapshot/watermark yet
 *  - stale        quarantined integrity, or no liveness signal for 30 s
 *  - delayed      a server timestamp is more than 5 s ahead of the local clock
 *  - reconnecting not contiguous or no heartbeat within the liveness window
 */
export function freshness(input: { now: number; generatedAt: string | null; heartbeatAt: number | null; contiguous: boolean; watermark: Watermark | null; }): 'current' | 'delayed' | 'reconnecting' | 'stale' | 'unavailable' {
  const generated = input.generatedAt === null ? NaN : Date.parse(input.generatedAt);
  if(!Number.isFinite(input.now) || !Number.isFinite(generated) || !input.watermark) return 'unavailable';
  const observed = Date.parse(input.watermark.observedAt);
  if(!Number.isFinite(observed)) return 'unavailable';
  if(input.watermark.integrity === 'quarantined') return 'stale';
  const heartbeat = input.heartbeatAt !== null && Number.isFinite(input.heartbeatAt) ? input.heartbeatAt : null;
  if(Math.max(generated, observed) - input.now > 5_000 || (heartbeat !== null && heartbeat - input.now > 5_000)) return 'delayed';
  if(input.contiguous && heartbeat !== null && input.now - heartbeat <= HEARTBEAT_LIVENESS_MS) return 'current';
  const lastAlive = heartbeat === null ? generated : Math.max(heartbeat, generated);
  return input.now - lastAlive > LIVENESS_STALE_MS ? 'stale' : 'reconnecting';
}
/** Age of the newest projected data (watermark observedAt) in ms; null when unknown. Data age never decides liveness. */
export function dataAgeMs(now: number, watermark: Watermark | null): number | null {
  if(!watermark || !Number.isFinite(now)) return null;
  const observed = Date.parse(watermark.observedAt);
  return Number.isFinite(observed) ? Math.max(0, now - observed) : null;
}
export function holderRows(response: HoldersResponse) {
  if(response.data.items.length > 100) throw new RangeError('Holder page exceeds 100');
  const identities = new Set<string>();
  for (const row of response.data.items) {
    exactUnits(row.balanceRaw, 0);
    if (!/^0x[0-9a-fA-F]{40}$/.test(row.wallet) || identities.has(row.wallet.toLowerCase())) throw new Error('Duplicate or invalid holder identity');
    identities.add(row.wallet.toLowerCase());
  }
  return [...response.data.items].sort((a, b) => {
    exactUnits(a.balanceRaw, 0); exactUnits(b.balanceRaw, 0);
    const x = BigInt(a.balanceRaw), y = BigInt(b.balanceRaw);
    return x === y ? a.wallet.toLowerCase().localeCompare(b.wallet.toLowerCase()) : x > y ? -1 : 1;
  });
}
export function rankingCards(response: RankingsResponse) {
  const snapshot = response.data.snapshot;
  if(snapshot.items.length > 100 || snapshot.items.some(row => row.category !== snapshot.category || row.modelVersion !== snapshot.modelVersion)) throw new Error('Incompatible ranking snapshot');
  return snapshot.items.slice(0, 50); // A replacement snapshot, never concatenate across cursors.
}
export function orderedTrades(rows: readonly Trade[]) {
  if(rows.length > 1000) throw new RangeError('Trade cache exceeds 1000');
  const unique = new Map<string, Trade>();
  for(const row of rows) {
    if (row.position.chainId !== '125' || !/^0x[0-9a-fA-F]{64}$/.test(row.position.transactionHash)) throw new Error('Invalid immutable trade identity');
    for (const field of ['height', 'transactionIndex', 'logIndex'] as const) exactUnits(row.position[field], 0);
    exactUnits(row.tokenAmountRaw, 0); exactUnits(row.usdlAmountRaw, 0);
    unique.set(`${row.position.chainId}:${row.position.transactionHash.toLowerCase()}:${row.position.logIndex}`, row);
  }
  return [...unique.values()].sort((a, b) => {
    for(const field of ['height', 'transactionIndex', 'logIndex'] as const) {
      exactUnits(a.position[field], 0); exactUnits(b.position[field], 0);
      const x = BigInt(a.position[field]), y = BigInt(b.position[field]);
      if(x !== y) return x > y ? -1 : 1;
    }
    return 0;
  });
}

export function exactRatio(numerator: string, denominator: string, places = 6): string {
  exactUnits(numerator, 0); exactUnits(denominator, 0);
  if(!Number.isInteger(places) || places < 0 || places > 18) throw new RangeError('Invalid ratio precision');
  const divisor = BigInt(denominator);
  if(divisor === 0n) return '—';
  const scaled = BigInt(numerator) * 10n ** BigInt(places);
  return `${scaled % divisor ? '≈' : ''}${exactUnits((scaled / divisor).toString(), places, places)}`;
}

export function exactPriceRatio(numerator: string, denominator: string): string {
  const component = /^(0|[1-9][0-9]{0,1023})$/;
  if (!component.test(numerator) || !component.test(denominator) || BigInt(denominator) === 0n) throw new RangeError('Invalid bounded price ratio');
  const divisor = BigInt(denominator), scaled = BigInt(numerator) * 1000000n;
  const raw = (scaled / divisor).toString().padStart(7, '0');
  const fraction = raw.slice(-6).replace(/0+$/, '');
  return `${scaled % divisor ? '≈' : ''}${raw.slice(0, -6)}${fraction ? '.' + fraction : ''}`;
}

// Compact display helpers for the typed market data layer (core/market/*).
export type { Price } from '../../../../../../sdk/launchpad/packages/production-contracts/src/types.gen';
type PriceLike = { numerator: string; denominator: string };
/** Price ratio as a JS number for charts and sparklines only; null when not finite. Never use for wallet submission. */
export function priceNumber(price: PriceLike | null | undefined): number | null {
  if (!price) return null;
  const component = /^(0|[1-9][0-9]{0,1023})$/;
  if (!component.test(price.numerator) || !component.test(price.denominator) || BigInt(price.denominator) === 0n) return null;
  // Divide at 18 significant digits, not 6 decimal places, so a small positive
  // price keeps its digits; display rounding belongs to the final formatter.
  const numerator = BigInt(price.numerator), denominator = BigInt(price.denominator);
  if (numerator === 0n) return 0;
  const shift = 18 - (price.numerator.length - price.denominator.length);
  const quotient = shift >= 0 ? numerator * 10n ** BigInt(shift) / denominator : numerator / (denominator * 10n ** BigInt(-shift));
  const value = Number(`${quotient}e${-shift}`);
  return Number.isFinite(value) ? value : null;
}
/** Raw USDL integer (6 decimals) to a JS number for compact display; null when invalid. */
export function usdlNumber(raw: string | null | undefined, decimals = 6): number | null {
  if (raw === null || raw === undefined || !/^-?(0|[1-9][0-9]{0,77})$/.test(raw)) return null;
  const negative = raw.startsWith('-');
  const value = Number(exactUnits(negative ? raw.slice(1) : raw, decimals, 6).replace('≈', ''));
  return Number.isFinite(value) ? (negative ? -value : value) : null;
}
/** Signed basis points to a percent number; null when not an integer string. */
export function bpsPercent(bps: string | null | undefined): number | null {
  if (bps === null || bps === undefined || !/^-?(0|[1-9][0-9]{0,77})$/.test(bps)) return null;
  return Number(bps) / 100;
}
/** 0x1234…abcd form for wallets and pools. */
export function shortWallet(address: string, head = 4, tail = 4): string {
  if (!/^0x[0-9a-fA-F]{40}$/.test(address)) return address;
  return `${address.slice(0, 2 + head)}…${address.slice(-tail)}`;
}
/** Compact human count: 1.2K, 3.4M, 5.6B. */
export function compactCount(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '—';
  const abs = Math.abs(value);
  const units: Array<[number, string]> = [[1e9, 'B'], [1e6, 'M'], [1e3, 'K']];
  for (const [size, suffix] of units) if (abs >= size) return `${(value / size).toFixed(abs / size >= 100 ? 0 : 1).replace(/\.0$/, '')}${suffix}`;
  return Number.isInteger(value) ? String(value) : value.toFixed(2);
}
