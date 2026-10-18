"use client";

// ─────────────────────────────────────────────────────────────────────
//  Thin React-hook layer over `graphqlClient.ts`.
//
//  `useSharedPolledQuery` replaces the ~30 hand-rolled `useState +
//  useCallback + useEffect(setInterval(refetch))` blocks that used to
//  live in every `useXxx` hook across `lib/perps`. All of them funnel
//  through the same `subscribeQuery` singleton so:
//    • duplicate components → 1 shared timer
//    • duplicate concurrent fetches → 1 in-flight Promise (dedup)
//    • hidden tab → every poller freezes until focus comes back
//
//  The public shape (data / isLoading / error / refetch) intentionally
//  matches the old bespoke hooks so migrating call sites is purely a
//  1-line swap.
// ─────────────────────────────────────────────────────────────────────

import { useCallback, useEffect, useRef, useState } from "react";
import {
  subscribeQuery,
  gqlQuery,
  type GraphQLEndpoint,
} from "@paxeer-network/paxeer-backend/core/graphql/client";

export interface SharedPolledQueryOptions<TRaw, TPicked> {
  endpoint: GraphQLEndpoint;
  query: string;
  /**
   * Variables object. Object identity does NOT need to be stable —
   * the hook serializes `variables` into the shared-poller key and
   * re-subscribes only when the serialized value changes.
   */
  variables?: Record<string, unknown>;
  /** Polling cadence in ms. Fastest requested cadence wins globally. */
  intervalMs: number;
  /** Gate the subscription (e.g. wait for a userAddress). Default `true`. */
  enabled?: boolean;
  /**
   * Extract the slice of the raw response that the component actually
   * wants. Lets callers name-shift (`res.oiSnapshots` → `OiSnapshot[]`).
   * The function is stored behind a ref so it doesn't need to be
   * memoized by the caller.
   */
  pick: (raw: TRaw) => TPicked;
}

export interface SharedPolledQueryResult<T> {
  data: T | undefined;
  isLoading: boolean;
  error: Error | null;
  /** Force an immediate fetch through the in-flight dedup layer. */
  refetch: () => void;
}

/**
 * Subscribe to a shared polled GraphQL query with React-ergonomic
 * state. Components asking for the same `(endpoint, query, variables)`
 * share the same upstream fetch + timer, and the hook returns a cached
 * snapshot synchronously on remount so there's no loading flash on
 * route changes.
 */
export function useSharedPolledQuery<TRaw = unknown, TPicked = TRaw>(
  opts: SharedPolledQueryOptions<TRaw, TPicked>,
): SharedPolledQueryResult<TPicked> {
  const { endpoint, query, variables, intervalMs, enabled = true, pick } = opts;

  const [data, setData] = useState<TPicked | undefined>();
  const [isLoading, setIsLoading] = useState<boolean>(enabled);
  const [error, setError] = useState<Error | null>(null);

  // Keep pick / variables in refs so consumers don't have to memoize.
  const pickRef = useRef(pick);
  pickRef.current = pick;
  const variablesRef = useRef(variables);
  variablesRef.current = variables;

  // Serialize variables so the effect's dep array is string-stable.
  const variablesKey = JSON.stringify(variables ?? null);

  const refetch = useCallback(() => {
    if (!enabled) return;
    setIsLoading(true);
    gqlQuery<TRaw>(endpoint, query, variablesRef.current)
      .then((res) => {
        setData(pickRef.current(res));
        setError(null);
      })
      .catch((e) => setError(e instanceof Error ? e : new Error(String(e))))
      .finally(() => setIsLoading(false));
  }, [endpoint, query, enabled]);

  useEffect(() => {
    if (!enabled) {
      setIsLoading(false);
      return;
    }
    setIsLoading(true);
    const unsubscribe = subscribeQuery<TRaw>({
      endpoint,
      query,
      variables: variablesRef.current,
      intervalMs,
      onData: (res) => {
        setData(pickRef.current(res));
        setError(null);
        setIsLoading(false);
      },
      onError: (err) => {
        setError(err);
        setIsLoading(false);
      },
    });
    return unsubscribe;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [endpoint, query, variablesKey, intervalMs, enabled]);

  return { data, isLoading, error, refetch };
}
