import type { OperationInput, Operations, V2ContractTypes } from '@kindle/sdk';

export type PlatformData = V2ContractTypes['PlatformData'];
export type PlatformWindow = OperationInput<'IF_PLATFORM'>['query']['window'];

/** The window the platform footer shows; core/api serves only `24h` (any other window answers 501 FEATURE_UNAVAILABLE). */
export const PLATFORM_WINDOW = '24h' as const;

/**
 * Interface 2.2.0 platform aggregates on the IF_PLATFORM envelope. They are
 * optional in the contract: absent (`undefined`) from a producer older than
 * 2.2.0, so the footer can tell "not served" from a value. Raw unsigned
 * integer strings like the contract fields.
 */
export interface UpcomingPlatformFields {
  /** Sum of pool market caps, raw USDL (6 decimals). */
  totalMarketCapUsdlRaw?: string | null;
  /** Trades in the window. */
  trades?: string | null;
  /** Pools launched in the window. */
  newPools?: string | null;
}
const upcomingFields = ['totalMarketCapUsdlRaw', 'trades', 'newPools'] as const satisfies readonly (keyof UpcomingPlatformFields)[];

export interface PlatformMetricsV2 extends UpcomingPlatformFields {
  window: PlatformWindow;
  /** Traded volume in the window, raw USDL (6 decimals). */
  volumeUsdlRaw: string;
  /** Pools launched (all time, core/stats-workers totalTokensLaunched). */
  pools: string;
  /** Distinct trading wallets in the window. */
  activeWallets: string;
  /** Time the aggregate was computed (the envelope's generatedAt). */
  generatedAt: string;
}

const RAW = /^(0|[1-9][0-9]{0,77})$/;

/** The 2.2.0 fields the envelope carries, validated; absent ones stay absent so the footer can tell "not served" from "served". */
export function upcomingPlatformFields(data: PlatformData): UpcomingPlatformFields {
  const row = data as PlatformData & Record<string, unknown>;
  const out: UpcomingPlatformFields = {};
  for (const field of upcomingFields) {
    const value = row[field];
    if (value === undefined) continue;
    if (value !== null && (typeof value !== 'string' || !RAW.test(value))) throw new TypeError(`Invalid platform metric ${field}`);
    out[field] = value;
  }
  return out;
}

/**
 * IF_PLATFORM `GET /v2/platform/metrics?window=24h` on core/api
 * (internal/rest/pool_reads.go platformMetricsV2, registered at
 * compatibility.go:93). The envelope carries volume, pool count and active
 * wallets, plus (2.2.0) total market cap, trades and new pools; a quarantined
 * envelope is rejected.
 */
export async function fetchPlatformMetricsV2(operations: Operations, signal?: AbortSignal, window: PlatformWindow = PLATFORM_WINDOW): Promise<PlatformMetricsV2> {
  const response = await operations.call('IF_PLATFORM', { query: { window } }, { signal });
  if (response.quality === 'quarantined' || response.watermark?.integrity === 'quarantined') throw new Error('Platform metrics integrity quarantined');
  const { volumeUsdlRaw, pools, activeWallets } = response.data;
  return { window, volumeUsdlRaw, pools, activeWallets, ...upcomingPlatformFields(response.data), generatedAt: response.generatedAt };
}
