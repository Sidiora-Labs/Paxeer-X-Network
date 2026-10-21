'use client';

import { useSyncExternalStore } from 'react';
import { AlertTriangle, RotateCw, X } from 'lucide-react';
import {
  clearBackgroundFailure,
  getBackgroundFailures,
  subscribeBackgroundFailures,
} from '@/platform/status/background-failures';

const EMPTY: ReturnType<typeof getBackgroundFailures> = [];

export function BackgroundStatus() {
  const failures = useSyncExternalStore(
    subscribeBackgroundFailures,
    getBackgroundFailures,
    () => EMPTY,
  );
  const failure = failures[0];
  if (!failure) return null;

  return (
    <div
      role={failure.retryable ? 'status' : 'alert'}
      aria-live="polite"
      className="fixed left-3 right-3 top-[calc(env(safe-area-inset-top,0px)+4rem)] z-[70] mx-auto flex max-w-lg items-start gap-3 rounded-2xl bg-amber-950 px-4 py-3 text-amber-50 shadow-xl"
    >
      {failure.retryable ? (
        <RotateCw aria-hidden="true" className="mt-0.5 h-4 w-4 shrink-0" />
      ) : (
        <AlertTriangle aria-hidden="true" className="mt-0.5 h-4 w-4 shrink-0" />
      )}
      <div className="min-w-0 flex-1">
        <p className="text-sm font-semibold">Saved state needs attention</p>
        <p className="mt-0.5 text-xs text-amber-100/80">{failure.message}</p>
      </div>
      <button
        type="button"
        aria-label="Dismiss status"
        onClick={() => clearBackgroundFailure(failure.id)}
        className="rounded-lg bg-amber-900 p-1.5"
      >
        <X aria-hidden="true" className="h-4 w-4" />
      </button>
    </div>
  );
}
