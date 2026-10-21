/**
 * Paxscan API barrel.
 *
 * Re-exports the HTTP surface consumed by Paxscan hooks and external callers.
 * Type-only symbols are re-exported under `export type` to keep the emitted
 * JS clean.
 */

export { buildUrl } from './buildUrl';
export { paxscanFetch, PaxscanError } from './fetch';
export { RESOURCES } from './resources';
export type {
  ApiResource,
  PaginatedResourceName,
  PathParams,
  QueryParams,
  ResourceName,
  ResourcePayload,
} from './resources';
