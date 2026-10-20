import React from 'react';

import { PAXSCAN_WS_URL } from '@/core/paxscan/config';
import { PaxscanSocketProvider } from '@/core/paxscan/socket';

export function PaxscanProvider({ children }: { children: React.ReactNode }) {
  return (
    <PaxscanSocketProvider url={ PAXSCAN_WS_URL }>
      { children }
    </PaxscanSocketProvider>
  );
}
