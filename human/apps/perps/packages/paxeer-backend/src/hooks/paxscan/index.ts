/**
 * Paxscan hooks barrel.
 *
 * Exposes the two react-query hooks that wrap the Paxscan HTTP client.
 * Consumers typically import from the top-level `@paxeer-network/paxeer-backend/lib/paxscan` barrel; this
 * file exists so Agent 3's `src/lib/paxscan/index.ts` can re-export cleanly
 * via `export * from './hooks'`.
 */

export { useBlockscoutQuery } from './useBlockscoutQuery';
export type { UseBlockscoutQueryOptions } from './useBlockscoutQuery';

export { useBlockscoutInfiniteQuery } from './useBlockscoutInfiniteQuery';
export type {
  PaxscanPageParam,
  UseBlockscoutInfiniteQueryOptions,
} from './useBlockscoutInfiniteQuery';
