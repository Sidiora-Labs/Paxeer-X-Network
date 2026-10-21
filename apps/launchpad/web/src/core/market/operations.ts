'use client';
import { useContext, useSyncExternalStore } from 'react';
import { Operations, type SdkClient } from '@kindle/sdk';
import { accountSession, subscribeAccount } from '@/widgets/profile/account-transport';
import { GatewayClientContext, browserGatewayClient, browserGatewaySessionClient, browserMarketClient, browserSessionClient, browserUserClient, useMarketClient } from './client';

const bound = new WeakMap<SdkClient, Operations>();

/** One Operations instance per SdkClient so public snapshot deduplication is shared. */
export function operationsFor(client: SdkClient): Operations {
  let operations = bound.get(client);
  if (!operations) { operations = new Operations(client); bound.set(client, operations); }
  return operations;
}

export function publicOperations(): Operations { return operationsFor(browserMarketClient()); }
export function userOperations(): Operations { return operationsFor(browserUserClient()); }

/** Public Operations bound to the browser market client (or the provided context client). */
export function useOperations(): Operations {
  return operationsFor(useMarketClient());
}

const noSession = () => null;

/** Session-bound Operations while an account session is installed, else null. */
export function useSessionOperations(): Operations | null {
  const session = useSyncExternalStore(subscribeAccount, accountSession, noSession);
  return session ? operationsFor(browserSessionClient()) : null;
}

/** Public Operations addressed to media/gateway (bounties live there, not on core/api). */
export function useGatewayOperations(): Operations {
  return operationsFor(useContext(GatewayClientContext) ?? browserGatewayClient());
}

/** Session-bound gateway Operations while an account session is installed, else null. */
export function useGatewaySessionOperations(): Operations | null {
  const session = useSyncExternalStore(subscribeAccount, accountSession, noSession);
  const override = useContext(GatewayClientContext);
  return session ? operationsFor(override ?? browserGatewaySessionClient()) : null;
}
