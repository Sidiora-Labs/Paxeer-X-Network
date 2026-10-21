/**
 * Paxscan URL builder.
 *
 * Ported from Blockscout `lib/api/buildUrl.ts`, simplified to:
 *   - single base URL (no per-chain, no rollup-L2, no external-chain branches)
 *   - no Next.js proxy (`isNeedProxy`) logic
 *   - no `path-to-regexp` dep — inline `:param` substitution
 *   - query-param serialization mirrors Blockscout's behavior (skip `undefined`
 *     and `''`; serialize `null`/`false`/`0` as-is; arrays append each element).
 */

import { PAXSCAN_API_URL } from '../config';
import { RESOURCES } from './resources';
import type { PathParams, QueryParams, ResourceName } from './resources';

const PATH_PARAM_REGEX = /:([a-zA-Z_][a-zA-Z0-9_]*)/g;

function substitutePathParams(path: string, params?: Record<string, string | undefined>): string {
  if (!params) return path;
  return path.replace(PATH_PARAM_REGEX, (_match, key: string) => {
    const value = params[key];
    // Leaving the `:name` placeholder surfaces the missing param as a 404; hook-level
    // `enabled` checks should gate the call before we get here in practice.
    if (value === undefined) return `:${ key }`;
    return encodeURIComponent(value);
  });
}

function appendQueryParam(
  params: URLSearchParams,
  key: string,
  value: string | Array<string> | number | boolean | null | undefined,
): void {
  // Skip `undefined` and empty-string; serialize `null`, `false`, `0` as strings —
  // matches Blockscout's `value !== undefined && value !== ''` convention.
  if (value === undefined || value === '') return;
  if (Array.isArray(value)) {
    for (const v of value) {
      if (v === undefined || v === null || v === '') continue;
      params.append(key, String(v));
    }
    return;
  }
  params.append(key, String(value));
}

export function buildUrl<R extends ResourceName>(
  resource: R,
  pathParams?: PathParams<R>,
  queryParams?: QueryParams<R>,
): string {
  const def = RESOURCES[resource];
  const substitutedPath = substitutePathParams(
    def.path,
    pathParams as Record<string, string | undefined> | undefined,
  );

  const params = new URLSearchParams();
  if (queryParams) {
    for (const [ key, value ] of Object.entries(queryParams)) {
      appendQueryParam(params, key, value);
    }
  }
  const qs = params.toString();
  const suffix = qs ? `?${ qs }` : '';

  if (!PAXSCAN_API_URL) {
    // Config warned about empty env var; return relative path so SSR / unit tests
    // don't crash on URL construction. `fetch` will fail meaningfully.
    return `${ substitutedPath }${ suffix }`;
  }

  // Using `new URL(absolutePath, base)` so a leading-slash path correctly
  // replaces the base's pathname (avoids `/api/v2/api/v2/...` double-prefix
  // when `PAXSCAN_API_URL` already ends in `/api/v2`).
  const url = new URL(substitutedPath, PAXSCAN_API_URL);
  if (qs) url.search = qs;
  return url.toString();
}
