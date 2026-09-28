/**
 * useRpcBalances — fetches token balances directly from the Paxeer RPC node.
 *
 * - Native PAX via eth_getBalance
 * - ERC-20s via balanceOf multicall
 * - Exposes refreshBalances() for post-transaction updates
 * - Returns a map of { address => balanceRaw (string) } where address '' = native
 */

import { useState, useCallback, useRef } from 'react';
import { ethers } from 'ethers';
import { getActiveRpcUrl } from '@/lib/constants';

const ERC20_BALANCE_ABI = ['function balanceOf(address owner) view returns (uint256)'];
const NATIVE_KEY = 'native';

export interface RpcBalances {
  /** 'native' => raw wei string, token address (lowercase) => raw balance string */
  [addressOrNative: string]: string;
}

interface TokenInfo {
  address: string;
  decimals: number;
}

function getProvider() {
  return new ethers.JsonRpcProvider(getActiveRpcUrl());
}

export function useRpcBalances() {
  const [balances, setBalances] = useState<RpcBalances>({});
  const [loading, setLoading] = useState(false);
  const lastTokensRef = useRef<TokenInfo[]>([]);

  const fetchBalances = useCallback(async (
    walletAddress: string,
    tokens: TokenInfo[],
  ) => {
    if (!walletAddress) return;
    setLoading(true);
    lastTokensRef.current = tokens;

    try {
      const provider = getProvider();

      // Fetch native + all ERC-20 balances in parallel
      const nativePromise = provider.getBalance(walletAddress);
      const tokenPromises = tokens.map(async (t) => {
        try {
          const contract = new ethers.Contract(t.address, ERC20_BALANCE_ABI, provider);
          const bal: bigint = await contract.balanceOf(walletAddress);
          return { address: t.address.toLowerCase(), balance: bal.toString() };
        } catch {
          return { address: t.address.toLowerCase(), balance: '0' };
        }
      });

      const [nativeBal, ...tokenResults] = await Promise.all([
        nativePromise,
        ...tokenPromises,
      ]);

      const result: RpcBalances = {
        [NATIVE_KEY]: nativeBal.toString(),
      };
      for (const r of tokenResults) {
        result[r.address] = r.balance;
      }

      setBalances(result);
    } catch (err) {
      console.error('[useRpcBalances] fetch failed:', err);
    } finally {
      setLoading(false);
    }
  }, []);

  /** Re-fetch balances using the last known token list. Call after any transaction. */
  const refreshBalances = useCallback(async (walletAddress: string) => {
    if (!walletAddress) return;
    await fetchBalances(walletAddress, lastTokensRef.current);
  }, [fetchBalances]);

  return {
    balances,
    loading,
    fetchBalances,
    refreshBalances,
    NATIVE_KEY,
  };
}
