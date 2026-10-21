"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import type { Address } from "viem";

import { pickCollateral } from "@/core/perps/accountView";
import { useAccount } from "./useAccount";
import { usePerpsWallet } from "./usePerpsWallet";

export interface AutoCollateralResult {
  selectedToken: Address;
  selectedSymbol: string;
  selectedDecimals: number;
  /** USDL base units (6 decimals) of getAccount(account).collateral. */
  collateral: bigint;
  isLoading: boolean;
}

/**
 * Selects USDL, the single collateral of the cross-margin account at 0x1015, with the
 * account collateral read through getAccount(account). Falls back to USDL at 6 decimals
 * with zero collateral when no wallet is connected.
 */
export function useAutoCollateral(): AutoCollateralResult {
  const { isConnected, address } = usePerpsWallet();
  const { getAccount } = useAccount();
  const [collateral, setCollateral] = useState(0n);
  const [isLoading, setIsLoading] = useState(false);
  const mountedRef = useRef(true);

  const fetchCollateral = useCallback(async () => {
    if (!isConnected || !address) {
      setCollateral(0n);
      return;
    }
    setIsLoading(true);
    try {
      const picked = pickCollateral(await getAccount(address as Address));
      if (mountedRef.current) setCollateral(picked.collateral);
    } catch {
      // Keep the last collateral read; the next poll retries.
    } finally {
      if (mountedRef.current) setIsLoading(false);
    }
  }, [isConnected, address, getAccount]);

  useEffect(() => {
    mountedRef.current = true;
    fetchCollateral();
    const id = setInterval(fetchCollateral, 30_000);
    return () => {
      mountedRef.current = false;
      clearInterval(id);
    };
  }, [fetchCollateral]);

  const picked = pickCollateral({ collateral });

  return {
    selectedToken: picked.token,
    selectedSymbol: picked.symbol,
    selectedDecimals: picked.decimals,
    collateral: picked.collateral,
    isLoading,
  };
}
