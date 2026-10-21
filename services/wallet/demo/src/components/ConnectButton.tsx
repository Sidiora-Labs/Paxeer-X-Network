'use client';

import { useEffect, useState } from 'react';
import type { PaxeerWallet } from '@paxeer/wallet';
import { paxeerWallet } from '@/lib/paxeer';
import { WalletModal } from './WalletModal';
import { unifiedOrigin } from './FundedAccountPanel';

export function ConnectButton() {
  const [client, setClient] = useState<PaxeerWallet | null>(null);
  const [origin, setOrigin] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    try {
      setOrigin(unifiedOrigin());
      setClient(paxeerWallet());
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Wallet configuration unavailable');
    }
  }, []);

  return <>
    <button type="button" data-wallet-connect onClick={() => setOpen(true)} disabled={!client}
      className="rounded-xl bg-[#004ced] px-5 py-3 text-white disabled:opacity-50">
      {error ? 'Wallet unavailable' : client ? 'Connect wallet' : 'Loading…'}
    </button>
    {error && <p role="status" className="max-w-sm text-sm text-neutral-400">{error}</p>}
    {client && <WalletModal open={open} onOpenChange={setOpen} paxeer={client} origin={origin} />}
  </>;
}
