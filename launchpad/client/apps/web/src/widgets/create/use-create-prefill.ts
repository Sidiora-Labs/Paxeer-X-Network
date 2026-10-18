'use client';

import { useSearchParams } from 'next/navigation';
import type { CreationDraftV2 } from './creation-storage';

export type FeeStrategy = CreationDraftV2['feeStrategy'];

const strategyAliases: Record<string, FeeStrategy> = {
  claim: 0,
  creator: 0,
  burn: 1,
  airdrop: 2,
  holders: 2,
  lp: 3,
  liquidity: 3,
  'lp-rewards': 3,
};

export function strategyFromQuery(value: string | null | undefined): FeeStrategy | null {
  if (typeof value !== 'string') return null;
  const key = value.trim().toLowerCase();
  return Object.prototype.hasOwnProperty.call(strategyAliases, key) ? strategyAliases[key] : null;
}

export function useCreatePrefill(): { feeStrategy: FeeStrategy | null } {
  const params = useSearchParams();
  return { feeStrategy: strategyFromQuery(params.get('strategy')) };
}
