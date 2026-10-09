// @vitest-environment jsdom

import { describe, expect, it } from 'vitest';
import { renderHook } from 'vitest/lib';

import useQueryClientConfig, { getResourceStaleTime, retry, STALE_TIME } from './useQueryClientConfig';

describe('useQueryClientConfig', () => {
  it('keeps the window-focus refetch off and hands the client the bounded retry', () => {
    const { result } = renderHook(() => useQueryClientConfig());

    const queries = result.current.getDefaultOptions().queries;

    expect(queries?.refetchOnWindowFocus).toBe(false);
    expect(queries?.retry).toBe(retry);
  });

  it('gives the same client back on a rerender', () => {
    const { result, rerender } = renderHook(() => useQueryClientConfig());

    const first = result.current;
    rerender();

    expect(result.current).toBe(first);
  });
});

describe('retry', () => {
  it('does not retry a client error', () => {
    expect(retry(0, { payload: { status: 404 } })).toBe(false);
    expect(retry(0, { status: 422 })).toBe(false);
  });

  it('does not retry a route the server does not implement', () => {
    expect(retry(0, { status: 501 })).toBe(false);
    expect(retry(0, { payload: { status: 501 } })).toBe(false);
  });

  it('retries a server error twice and then gives up', () => {
    expect(retry(0, { status: 500 })).toBe(true);
    expect(retry(1, { status: 500 })).toBe(true);
    expect(retry(2, { status: 500 })).toBe(false);
  });
});

describe('getResourceStaleTime', () => {
  it('holds what the chain has already written', () => {
    expect(getResourceStaleTime('general:tx')).toBe(STALE_TIME.record);
    expect(getResourceStaleTime('general:block')).toBe(STALE_TIME.record);
    expect(getResourceStaleTime('general:tx_raw_trace')).toBe(STALE_TIME.record);
    expect(getResourceStaleTime('general:paxeer_x_receipt')).toBe(STALE_TIME.record);
  });

  it('holds the configuration for longer than a record and the statistics for less', () => {
    expect(getResourceStaleTime('general:config_backend_version')).toBe(STALE_TIME.config);
    expect(getResourceStaleTime('general:stats')).toBe(STALE_TIME.stats);
    expect(getResourceStaleTime('stats:pages_main')).toBe(STALE_TIME.stats);
    expect(getResourceStaleTime('general:address_counters')).toBe(STALE_TIME.stats);

    expect(STALE_TIME.config).toBeGreaterThan(STALE_TIME.record);
    expect(STALE_TIME.record).toBeGreaterThan(STALE_TIME.stats);
    expect(STALE_TIME.stats).toBeGreaterThan(STALE_TIME.head);
  });

  it('keeps what follows the chain head on the shortest class', () => {
    expect(getResourceStaleTime('general:address')).toBe(STALE_TIME.head);
    expect(getResourceStaleTime('general:homepage_blocks')).toBe(STALE_TIME.head);
  });

  it('leaves a resource outside the classes to the library default', () => {
    expect(getResourceStaleTime('general:address_txs')).toBeUndefined();
    expect(getResourceStaleTime('general:search')).toBeUndefined();
  });
});
