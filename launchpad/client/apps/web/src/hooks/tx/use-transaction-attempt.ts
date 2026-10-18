'use client';
import { useCallback, useSyncExternalStore } from 'react';
import type { TradeJourney, JourneyState } from '@/widgets/trading/transaction-controls/runtime';
const idle: JourneyState = Object.freeze({ signing: false, attempts: Object.freeze([]) });
export function useTransactionAttempt(journey: TradeJourney | null) {
  const subscribe = useCallback((notify: () => void) => journey?.subscribe(notify) ?? (() => {}), [journey]);
  const snapshot = useCallback(() => journey?.state ?? idle, [journey]);
  return useSyncExternalStore(subscribe, snapshot, () => idle);
}
