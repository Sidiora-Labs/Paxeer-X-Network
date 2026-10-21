'use client';

import { QueryClient, QueryClientProvider, useQueryClient } from '@tanstack/react-query';
import { useCallback, useEffect, useRef, useState, useSyncExternalStore, type ReactNode } from 'react';
import { httpClient as indexerHttp } from 'indexer-sdk';
import { httpClient as metadataHttp } from 'metadata-sdk';
import { httpClient as rankingHttp } from 'ranking-algo-sdk';
import { httpClient as statsHttp } from 'stats-sdk';
import { dataApiUrl, metadataApiUrl } from '@/core/sdk-config';
import { cachePolicy } from '@/core/cache-policy';
import { transientRetry } from '@/core/transient-retry';
import { changeFoundationPrincipal, invalidateFoundationDeployment, isPrivateFoundationQuery } from '@/core/app-foundation/private-cache';
import { useWalletRuntime } from '@/core/wallet-sdk/runtime';
import { useRuntimeAuthority } from '@/core/runtime-authority';
import { isWalletPrivateQueryV2 } from '@/core/wallet-sdk/backend';
import type { Address } from 'viem';

let sdksConfigured = false;
function configureSDKs() {
  if (sdksConfigured) return;
  // Each SDK appends its own resource path (e.g. `/metadata/{addr}`), so
  // baseUrl is the HOST ROOT. Chart candles are not read through an SDK here:
  // the chart datafeed uses core/api `/v2/pools/{pool}/candles` + `/v2/stream`
  // and core/trading-charts `/v2/udf/*` (core/charts-udf.ts).
  indexerHttp.configure({ baseUrl: dataApiUrl('') });
  metadataHttp.configure({ baseUrl: metadataApiUrl('') });
  rankingHttp.configure({ baseUrl: dataApiUrl('') });
  statsHttp.configure({ baseUrl: dataApiUrl('') });
  sdksConfigured = true;
}

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        // Default tier for queries that don't opt into a specific policy.
        // FAST = 10s fresh, matching the prior default; see cache-policy.ts.
        staleTime: cachePolicy.FAST.staleTime,
        gcTime: cachePolicy.FAST.gcTime,
        refetchOnWindowFocus: true,
        refetchOnReconnect: true,
        refetchIntervalInBackground: false,
        // Transient failures (retryable 503 / 429 with retryAfterMs, dropped
        // connections) retry on the server's delay; anything else retries once.
        ...transientRetry(1),
      },
    },
  });
}

let browserQueryClient: QueryClient | undefined;
function getQueryClient() {
  if (typeof window === 'undefined') return makeQueryClient();
  if (!browserQueryClient) browserQueryClient = makeQueryClient();
  return browserQueryClient;
}

export default function QueryProvider({ children }: { children: ReactNode }) {
  const queryClient = getQueryClient();

  useEffect(() => {
    configureSDKs();
  }, []);

  return (
    <QueryClientProvider client={queryClient}>
      {children}
    </QueryClientProvider>
  );
}

/**
 * The principal a private cache belongs to. `anonymous` means no wallet session
 * and no identity principal: no private query can have been fetched for it.
 */
export type CachePrincipal = { key: string; anonymous: boolean };

/**
 * Whether the barrier must hide its children while the private cache is
 * cleared. Only leaving a published, non-anonymous principal for a different
 * one can show the previous principal's private data, so only that hides the
 * app. The first publish (SSR and hydration), anonymous -> signed-in, and a
 * deployment-authority change (whose foundation queries are keyed by the
 * deployment) clear the cache without making the whole app inert. A barrier
 * that mounts over a cache already holding private queries (a remount under a
 * shared client) has an unknown previous principal and stays hidden until the
 * first clear completes.
 */
export function principalBarrierBlocks(published: CachePrincipal | undefined, observed: CachePrincipal, failure: boolean, inheritedPrivateCache = false): boolean {
  if (failure) return true;
  if (!published) return inheritedPrivateCache;
  if (published.anonymous) return false;
  return published.key !== observed.key;
}

export function PrincipalCacheBarrier({ children }: { children: ReactNode }) {
  const client = useQueryClient();
  const runtime = useWalletRuntime();
  const { authority } = useRuntimeAuthority();
  const identity = runtime.identity;
  const subscribe = useCallback((listener: () => void) => identity?.subscribe(listener) ?? (() => {}), [identity]);
  const snapshot = useCallback(() => identity?.current ?? null, [identity]);
  const principal = useSyncExternalStore(subscribe, snapshot, () => null);
  const account = runtime.session?.account.toLowerCase() ?? null;
  const principalKey = JSON.stringify([account, runtime.session?.chainId ?? null, runtime.session?.sessionEpoch ?? null, principal?.sessionId ?? null, principal?.wallet.toLowerCase() ?? null, principal?.revokedAt ?? null, principal ? [...principal.scopes].sort() : []]);
  const anonymous = account === null && principal === null;
  const observed = JSON.stringify([principalKey, authority?.revision.revisionSha256 ?? null]);
  const desired = useRef(observed); desired.current = observed;
  const previousDeployment = useRef<string | undefined>(undefined);
  const previousWallet = useRef<Address | undefined>(undefined);
  const [published, setPublished] = useState<CachePrincipal>();
  const [failure, setFailure] = useState<string>();
  const [inheritedPrivateCache] = useState(() => client.getQueryCache().findAll({ predicate: query => isPrivateFoundationQuery(query) || isWalletPrivateQueryV2(query) }).length > 0);
  useEffect(() => {
    let active = true;
    const deployment = authority?.revision.revisionSha256;
    const transition = async () => {
      if (previousDeployment.current && previousDeployment.current !== deployment) await invalidateFoundationDeployment(client, previousDeployment.current);
      const filters = { predicate: (query: { queryKey: readonly unknown[]; meta?: Record<string, unknown> }) => isWalletPrivateQueryV2(query, previousWallet.current) || isWalletPrivateQueryV2(query, runtime.session?.account) };
      const canceled = client.cancelQueries(filters); client.removeQueries(filters); await canceled;
      await changeFoundationPrincipal(client, () => {
        if (!active || desired.current !== observed) return;
        previousDeployment.current = deployment;
        previousWallet.current = runtime.session?.account;
        setFailure(undefined); setPublished({ key: principalKey, anonymous });
      });
    };
    void transition().catch(() => { if (active) setFailure('Something went wrong, reload the page'); });
    return () => { active = false; };
  }, [client, observed, principalKey, anonymous, authority?.revision.revisionSha256, runtime.session?.account]);
  const blocked = principalBarrierBlocks(published, { key: principalKey, anonymous }, !!failure, inheritedPrivateCache);
  return (
    <>
      {blocked && <p role="status" className="p-4 text-sm text-text-muted">{failure ?? 'Loading...'}</p>}
      <div inert={blocked} aria-hidden={blocked || undefined} style={blocked ? { visibility: 'hidden' } : undefined}>
        {children}
      </div>
    </>
  );
}
