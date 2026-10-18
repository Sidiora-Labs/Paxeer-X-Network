'use client';

import { useCallback, useSyncExternalStore } from 'react';

interface AuthModalState {
  open: boolean;
  reason: string | null;
}

let state: AuthModalState = { open: false, reason: null };
const listeners = new Set<() => void>();

function emit() {
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

function getSnapshot(): AuthModalState {
  return state;
}

const serverSnapshot: AuthModalState = { open: false, reason: null };

function getServerSnapshot(): AuthModalState {
  return serverSnapshot;
}

/** Plain sentences for the short reason keys callers pass; any other key-like reason is not shown. */
const reasonCopy: Record<string, string> = {
  watchlist: 'Sign in to save your watchlist',
  rewards: 'Sign in to see your rewards',
  trade: 'Sign in to trade',
  'ticker-holdings': 'Sign in to see your holdings',
  'terminal-position': 'Sign in to see your position',
};

export function readableReason(reason: string | null | undefined): string | null {
  if (!reason) return null;
  if (Object.hasOwn(reasonCopy, reason)) return reasonCopy[reason];
  return /\s/.test(reason.trim()) ? reason.trim().replace(/\.$/, '') : null;
}

/** Open the sign-in modal from anywhere (event handlers, transports, non-React code). */
export function openAuthModal(reason?: string): void {
  state = { open: true, reason: readableReason(reason) };
  emit();
}

export function closeAuthModal(): void {
  if (!state.open && state.reason === null) return;
  state = { open: false, reason: null };
  emit();
}

export function useAuthModal(): {
  open: boolean;
  reason: string | null;
  openModal: (reason?: string) => void;
  close: () => void;
} {
  const snapshot = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
  const openModal = useCallback((reason?: string) => openAuthModal(reason), []);
  const close = useCallback(() => closeAuthModal(), []);
  return { open: snapshot.open, reason: snapshot.reason, openModal, close };
}
