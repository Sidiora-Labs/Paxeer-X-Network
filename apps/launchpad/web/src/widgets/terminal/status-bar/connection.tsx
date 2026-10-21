'use client';
import { useSyncExternalStore } from 'react';
import { cn } from '@/lib/utils';
import { Tooltip } from '@/ui/primitives';
import { useOptionalTerminalLive } from '../live-context';
import { connectionStatus, type ConnectionStatus } from './model';

function subscribeOnline(listener: () => void) {
  window.addEventListener('online', listener);
  window.addEventListener('offline', listener);
  return () => { window.removeEventListener('online', listener); window.removeEventListener('offline', listener); };
}
const readOnline = () => navigator.onLine !== false;
const serverOnline = () => true;

export function useOnline(): boolean {
  return useSyncExternalStore(subscribeOnline, readOnline, serverOnline);
}

const dotTone = { success: 'bg-success', warning: 'bg-warning', danger: 'bg-danger' } as const;
const textTone = { success: 'text-success', warning: 'text-warning', danger: 'text-danger' } as const;

function Pill({ status }: { status: ConnectionStatus }) {
  return (
    <Tooltip content={status.detail} align="start">
      <span tabIndex={0} role="status" aria-label={`Connection ${status.label}. ${status.detail}`}
        className={cn('inline-flex h-5 shrink-0 items-center gap-1.5 rounded-pill bg-surface-3 px-2 text-xs font-bold', textTone[status.tone])}>
        <span aria-hidden="true" className={cn('size-1.5 rounded-pill', dotTone[status.tone])} />
        {status.label}
      </span>
    </Tooltip>
  );
}

/** Connection pill: the open pair's shared market stream (TerminalLiveProvider) when a pair is loaded, else the browser network flag. */
export function ConnectionPill({ poolAddress }: { poolAddress: string | null }) {
  const online = useOnline();
  const live = useOptionalTerminalLive(poolAddress);
  return <Pill status={connectionStatus({ online, stream: live ? live.state.stream : null, freshness: live ? live.freshness : null })} />;
}
