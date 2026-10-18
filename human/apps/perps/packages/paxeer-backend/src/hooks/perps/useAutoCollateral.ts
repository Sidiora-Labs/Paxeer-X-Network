"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { usePerpsWallet } from "./usePerpsWallet";
import { getVaultAddress, getBalance } from "./useVault";
import { COLLATERAL_TOKENS, COLLATERAL_OPTIONS } from "@paxeer-network/paxeer-backend/core/perps/config";

const ZERO_ADDR = "0x0000000000000000000000000000000000000000" as `0x${string}`;
const ALL_TOKENS = Object.values(COLLATERAL_TOKENS);

export interface AutoCollateralResult {
  selectedToken: string;
  selectedSymbol: string;
  selectedDecimals: number;
  balances: Record<string, number>;
  isLoading: boolean;
}

/**
 * Auto-selects the stablecoin with the highest vault balance.
 * Falls back to COLLATERAL_OPTIONS[0] if no vault or all balances are zero.
 */
export function useAutoCollateral(): AutoCollateralResult {
  const { isConnected, address } = usePerpsWallet();
  const [balances, setBalances] = useState<Record<string, number>>({});
  const [isLoading, setIsLoading] = useState(false);
  const mountedRef = useRef(true);

  const fetchBalances = useCallback(async () => {
    if (!isConnected || !address) {
      setBalances({});
      return;
    }
    setIsLoading(true);
    try {
      const vault = await getVaultAddress(address as `0x${string}`);
      if (vault === ZERO_ADDR || !mountedRef.current) {
        setIsLoading(false);
        return;
      }
      const results = await Promise.allSettled(
        ALL_TOKENS.map(async (tok) => {
          const bal = await getBalance(vault, tok.address);
          return {
            address: tok.address,
            balance: Number(bal) / 10 ** tok.decimals,
          };
        })
      );
      if (!mountedRef.current) return;
      const map: Record<string, number> = {};
      for (const r of results) {
        if (r.status === "fulfilled") {
          map[r.value.address] = r.value.balance;
        }
      }
      setBalances(map);
    } catch {
      // Silently fail, keep existing balances
    } finally {
      if (mountedRef.current) setIsLoading(false);
    }
  }, [isConnected, address]);

  useEffect(() => {
    mountedRef.current = true;
    fetchBalances();
    const id = setInterval(fetchBalances, 30_000);
    return () => {
      mountedRef.current = false;
      clearInterval(id);
    };
  }, [fetchBalances]);

  // Pick the token with the highest balance
  let bestAddr = COLLATERAL_OPTIONS[0]?.value ?? "";
  let bestBal = 0;
  for (const tok of ALL_TOKENS) {
    const bal = balances[tok.address] ?? 0;
    if (bal > bestBal) {
      bestBal = bal;
      bestAddr = tok.address;
    }
  }

  const tokenInfo = ALL_TOKENS.find((t) => t.address === bestAddr);

  return {
    selectedToken: bestAddr,
    selectedSymbol: tokenInfo?.symbol ?? "USID",
    selectedDecimals: tokenInfo?.decimals ?? 18,
    balances,
    isLoading,
  };
}
