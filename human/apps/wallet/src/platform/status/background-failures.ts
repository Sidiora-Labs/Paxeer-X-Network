import type { FailureDomain, FailureKind } from '@/domains/shared';

export interface BackgroundFailure {
  readonly id: string;
  readonly domain: FailureDomain;
  readonly kind: FailureKind;
  readonly code: string;
  readonly message: string;
  readonly retryable: boolean;
  readonly occurredAt: number;
}

type Listener = () => void;

let failures: readonly BackgroundFailure[] = [];
const listeners = new Set<Listener>();
const MAX_FAILURES = 20;

function notify(): void {
  for (const listener of listeners) listener();
}

export function reportBackgroundFailure(
  failure: Omit<BackgroundFailure, 'id' | 'occurredAt'>,
): BackgroundFailure {
  const value: BackgroundFailure = {
    ...failure,
    id: globalThis.crypto?.randomUUID?.() ?? `failure-${Date.now()}`,
    occurredAt: Date.now(),
  };
  failures = [value, ...failures].slice(0, MAX_FAILURES);
  notify();
  return value;
}

export function getBackgroundFailures(): readonly BackgroundFailure[] {
  return failures;
}

export function clearBackgroundFailure(id: string): void {
  const next = failures.filter((failure) => failure.id !== id);
  if (next.length !== failures.length) {
    failures = next;
    notify();
  }
}

export function subscribeBackgroundFailures(listener: Listener): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}
