import React from 'react';
import { renderToString } from 'react-dom/server';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { describe, expect, it } from 'vitest';

// No stats host: holders are read from core/stats-workers only, so nothing may be requested anywhere.
delete process.env.NEXT_PUBLIC_STATS_API_URL;
const { fetchLegacyShare, holdersHostUnset, loadHoldersPage, useHolders } = await import('./use-holders');
const { toUnsupported } = await import('./unsupported');

const pool = '0xce20da50f92d21d8bee497910c782e9aa25b6263';

function recordingFetch() {
  const calls: string[] = [];
  const fetcher: typeof fetch = async input => { calls.push(String(input)); throw new Error('no request expected'); };
  return { calls, fetcher };
}

describe('holders without a stats host', () => {
  it('requests no holder share from any other origin', async () => {
    const { calls, fetcher } = recordingFetch();
    expect(await fetchLegacyShare(pool, undefined, fetcher)).toBeNull();
    expect(calls).toEqual([]);
  });

  it('fails the page read with an unsupported-class error, not a fabricated empty list', async () => {
    const failure = await loadHoldersPage(pool, null, undefined, null).catch((error: unknown) => error);
    expect(toUnsupported('IF_HOLDERS', failure)).toMatchObject({ state: 'unsupported', operation: 'IF_HOLDERS' });
  });

  it('reports the stats-host-unset marker from the hook', () => {
    let seen: unknown = null;
    function Probe() { seen = useHolders(pool); return null; }
    renderToString(<QueryClientProvider client={new QueryClient()}><Probe /></QueryClientProvider>);
    expect(seen).toBe(holdersHostUnset);
    expect(holdersHostUnset).toMatchObject({ state: 'unsupported', operation: 'IF_HOLDERS', status: null });
  });
});
