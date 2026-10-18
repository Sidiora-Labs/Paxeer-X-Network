'use client';

/**
 * React bindings for the public v2 market stream (core/api `GET /v2/snapshots`
 * + WS `/v2/stream`, see ./data-stream).
 *
 * `useMarketTopics` joins the shared, reference-counted connection for the
 * component's lifetime. Callbacks are held in a ref so inline closures do not
 * churn the subscription; it only re-registers when the topic set changes.
 * SSR-safe: the effect only runs on the client and `subscribeMarketTopics`
 * no-ops without a WebSocket.
 */

import { useEffect, useRef, useState } from 'react';
import type { ReconnectStatus, V2StreamEvent, V2TopicSnapshot } from '@kindle/sdk';
import { reportError } from '@/core/report-error';
import { useMarketClient } from '@/core/market/client';
import { subscribeMarketTopics, CanonicalDataStream, type CanonicalSubscription } from './data-stream';

export interface UseMarketTopicsOptions {
  onEvent(event: V2StreamEvent): void;
  onSnapshot?(snapshot: V2TopicSnapshot): void;
  onStatus?(status: ReconnectStatus): void;
  enabled?: boolean;
}

const idle: ReconnectStatus = Object.freeze({ state: 'idle', circuitOpen: false, authenticationRequired: false });

/** Subscribe to v2 public market topics; returns the shared connection status. */
export function useMarketTopics(topics: readonly string[], options: UseMarketTopicsOptions): ReconnectStatus {
  const client = useMarketClient();
  const [status, setStatus] = useState<ReconnectStatus>(idle);
  const callbacks = useRef(options);
  useEffect(() => { callbacks.current = options; });
  const enabled = options.enabled ?? true;
  const identity = JSON.stringify([...new Set(topics)].sort());
  useEffect(() => {
    const wanted = JSON.parse(identity) as string[];
    if (!enabled || wanted.length === 0) return;
    const controller = new AbortController();
    let release = () => {};
    try {
      release = subscribeMarketTopics(client, {
        topics: wanted,
        signal: controller.signal,
        onSnapshot: snapshot => { if (!controller.signal.aborted) callbacks.current.onSnapshot?.(snapshot); },
        onEvent: event => { if (!controller.signal.aborted) callbacks.current.onEvent(event); },
        onStatus: value => { if (!controller.signal.aborted) { setStatus(value); callbacks.current.onStatus?.(value); } },
        onError: error => { if (!controller.signal.aborted) reportError(error, { area: 'market-stream', action: 'consumer' }); },
      });
    } catch (error) {
      // Topic policy (more than 20 topics on the shared connection) or an invalid topic.
      reportError(error, { area: 'market-stream', action: 'subscribe' });
    }
    return () => { controller.abort(); release(); };
  }, [client, identity, enabled]);
  return status;
}

export function useCanonicalDataStream(stream: CanonicalDataStream, options: Omit<CanonicalSubscription, 'signal'>, enabled = true): ReconnectStatus {
  const [status, setStatus] = useState<ReconnectStatus>(stream.status);
  const callbacks = useRef(options);
  useEffect(() => { callbacks.current = options; });
  const identity = JSON.stringify([...options.topics].sort());
  useEffect(() => {
    if (!enabled) return;
    const controller = new AbortController();
    const release = stream.subscribe({
      topics: JSON.parse(identity) as string[], signal: controller.signal,
      installSnapshot: snapshot => { if (!controller.signal.aborted) return callbacks.current.installSnapshot(snapshot); },
      onEvent: event => { if (!controller.signal.aborted) callbacks.current.onEvent(event); },
      onError: error => { if (!controller.signal.aborted) callbacks.current.onError?.(error); },
      onStatus: value => { if (!controller.signal.aborted) { setStatus(value); callbacks.current.onStatus?.(value); } },
    });
    return () => { controller.abort(); release(); };
  }, [stream, identity, enabled]);
  return status;
}
