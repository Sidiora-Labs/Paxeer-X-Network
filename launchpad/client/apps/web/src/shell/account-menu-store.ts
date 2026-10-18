'use client';

import { useSyncExternalStore } from 'react';
import type { AccountMenuTab } from './wallet-modal';

let current: AccountMenuTab | null = null;
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

function setMenu(next: AccountMenuTab | null) {
  if (current === next) return;
  current = next;
  for (const listener of listeners) listener();
}

/** Opens the header account menu on a tab from anywhere (for example the profile Deposit button opening Receive). */
export function openAccountMenu(tab: AccountMenuTab = 'account'): void {
  setMenu(tab);
}

export function closeAccountMenu(): void {
  setMenu(null);
}

/** The open account-menu tab, or null when the menu is closed. The menu itself is rendered by WalletButton. */
export function useAccountMenu(): AccountMenuTab | null {
  return useSyncExternalStore(subscribe, () => current, () => null);
}
