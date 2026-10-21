'use client';

import { WagmiProvider, type State } from 'wagmi';
import { useState, type ReactNode } from 'react';
import { getWagmiConfig } from '@/core/wagmi-config';
import { PaxeerProvider } from '@/core/wallet-sdk/embedded_sdk/embedded';

/**
 * wagmi plus the Paxeer Embedded Wallet context. The embedded wallet's wagmi
 * connector and this provider share one SDK singleton, so a Supabase sign-in
 * started from the dialog is visible to both without prop drilling.
 */
export default function Web3Provider({
  children,
  initialState,
}: {
  children: ReactNode;
  initialState?: State;
}) {
  // One config per server render (never shared across requests) and one per
  // page in the browser; see getWagmiConfig.
  const [config] = useState(getWagmiConfig);
  return (
    <WagmiProvider config={config} initialState={initialState}>
      <PaxeerProvider>{children}</PaxeerProvider>
    </WagmiProvider>
  );
}
