'use client';

import {
  PaxeerWallet,
} from '@/lib/wallet';
import { PAXEER_CONFIG, getActiveRpcUrl } from './constants';

let _wallet: PaxeerWallet | null = null;

export function getWallet(): PaxeerWallet {
  if (_wallet) return _wallet;
  _wallet = new PaxeerWallet({
    rpcUrl: getActiveRpcUrl(),
    chainId: PAXEER_CONFIG.chainId,
    sessionTimeoutMs: PAXEER_CONFIG.sessionTimeoutMs,
  });
  return _wallet;
}
