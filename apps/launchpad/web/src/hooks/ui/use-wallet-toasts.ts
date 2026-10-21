'use client';

import { useEffect, useRef } from 'react';
import { useAccount } from 'wagmi';
import { toast } from 'sonner';
import { formatAddress } from '@/utils/format';

export function useWalletToasts() {
  const { address, isConnected } = useAccount();
  const prevConnected = useRef(isConnected);
  const prevAddress = useRef(address);

  useEffect(() => {
    if (isConnected && !prevConnected.current && address) {
      toast.success(`Wallet connected: ${formatAddress(address, 4)}`);
    }
    if (!isConnected && prevConnected.current && prevAddress.current) {
      toast('Wallet disconnected', { description: 'Connect again to trade' });
    }
    prevConnected.current = isConnected;
    prevAddress.current = address;
  }, [isConnected, address]);
}
