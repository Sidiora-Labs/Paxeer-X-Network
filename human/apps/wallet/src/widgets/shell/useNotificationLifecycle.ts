'use client';

import { useEffect } from 'react';
import { useWalletState } from '@/providers/WalletProvider';
import {
  startNotificationTriggers,
  stopNotificationTriggers,
  handleTxCount,
} from '@/lib/notification-triggers';
import { useTxCountQuery } from '@/lib/queries/txCount';

/**
 * Wires the notification trigger lifecycle to the active wallet session.
 *
 * - Starts triggers when a session becomes active, stops on lock/unmount.
 * - Subscribes to useTxCountQuery (60 s poll via TanStack Query) and
 *   forwards each new count to handleTxCount for comparison + notification.
 */
export function useNotificationLifecycle(): void {
  const { activeAccount, isLocked } = useWalletState();
  const address = !isLocked && activeAccount?.address ? activeAccount.address : undefined;

  const { data: txCount } = useTxCountQuery(address);

  useEffect(() => {
    if (!address) {
      stopNotificationTriggers();
      return;
    }
    startNotificationTriggers(address);
    return () => stopNotificationTriggers();
  }, [address]);

  useEffect(() => {
    if (txCount !== undefined && address) {
      handleTxCount(txCount);
    }
  }, [txCount, address]);
}
