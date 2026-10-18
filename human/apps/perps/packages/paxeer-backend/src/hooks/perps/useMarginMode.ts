"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { TradingAccountABI } from "@paxeer-network/paxeer-backend/core/network/abi/TradingAccount";
import { readContract, writeContract, waitForTx } from "./useContract";
import { getVaultAddress } from "./useVault";
import type {
  MarginMode,
  PositionMargin,
  AccountSummary,
  TopUpPositionArgs,
  TransferMarginArgs,
} from "@paxeer-network/paxeer-backend/types/perps";

export function useMarginMode(userAddress: `0x${string}` | undefined): {
  data: { marginMode: number; accountSummary: AccountSummary | null } | undefined;
  isLoading: boolean;
  error: Error | null;
  refetch: () => void;
} {
  const [data, setData] = useState<{ marginMode: number; accountSummary: AccountSummary | null } | undefined>();
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const mountedRef = useRef(true);

  const refetch = useCallback(async () => {
    if (!userAddress) return;
    setIsLoading(true);
    setError(null);
    try {
      const vaultAddr = await getVaultAddress(userAddress);
      const zeroAddr = "0x0000000000000000000000000000000000000000" as `0x${string}`;
      if (vaultAddr === zeroAddr) {
        if (mountedRef.current) setData({ marginMode: 0, accountSummary: null });
        return;
      }

      const mode = await readContract({
        address: vaultAddr,
        abi: TradingAccountABI,
        functionName: "getMarginMode",
      }) as number;

      if (mountedRef.current) {
        setData({ marginMode: mode, accountSummary: null });
      }
    } catch (e) {
      if (mountedRef.current) setError(e instanceof Error ? e : new Error(String(e)));
    } finally {
      if (mountedRef.current) setIsLoading(false);
    }
  }, [userAddress]);

  useEffect(() => {
    mountedRef.current = true;
    refetch();
    return () => { mountedRef.current = false; };
  }, [refetch]);

  return { data, isLoading, error, refetch };
}

export function useSetMarginMode(userAddress: `0x${string}` | undefined): {
  execute: (mode: MarginMode) => Promise<void>;
  isPending: boolean;
  error: Error | null;
} {
  const [isPending, setIsPending] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const execute = useCallback(async (mode: MarginMode) => {
    if (!userAddress) throw new Error("Wallet not connected");
    setIsPending(true);
    setError(null);
    try {
      const vaultAddr = await getVaultAddress(userAddress);
      const hash = await writeContract({
        address: vaultAddr,
        abi: TradingAccountABI,
        functionName: "setMarginMode",
        args: [mode],
      });
      await waitForTx(hash);
    } catch (e) {
      const err = e instanceof Error ? e : new Error(String(e));
      setError(err);
      throw err;
    } finally {
      setIsPending(false);
    }
  }, [userAddress]);

  return { execute, isPending, error };
}

export function usePositionMargin(
  userAddress: `0x${string}` | undefined,
  positionId: bigint | undefined
): {
  data: PositionMargin | undefined;
  isLoading: boolean;
  error: Error | null;
  refetch: () => void;
} {
  const [data, setData] = useState<PositionMargin | undefined>();
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const mountedRef = useRef(true);

  const refetch = useCallback(async () => {
    if (!userAddress || positionId === undefined) return;
    setIsLoading(true);
    setError(null);
    try {
      const vaultAddr = await getVaultAddress(userAddress);
      const result = await readContract({
        address: vaultAddr,
        abi: TradingAccountABI,
        functionName: "getPositionMargin",
        args: [positionId],
      }) as { token: `0x${string}`; lockedAmount: bigint; marketId: bigint; isLong: boolean };

      if (mountedRef.current) {
        setData({
          token: result.token,
          lockedAmount: result.lockedAmount,
          marketId: result.marketId,
          isLong: result.isLong,
        });
      }
    } catch (e) {
      if (mountedRef.current) setError(e instanceof Error ? e : new Error(String(e)));
    } finally {
      if (mountedRef.current) setIsLoading(false);
    }
  }, [userAddress, positionId]);

  useEffect(() => {
    mountedRef.current = true;
    refetch();
    return () => { mountedRef.current = false; };
  }, [refetch]);

  return { data, isLoading, error, refetch };
}

export function useTopUpPosition(userAddress: `0x${string}` | undefined): {
  execute: (args: TopUpPositionArgs) => Promise<void>;
  isPending: boolean;
  error: Error | null;
} {
  const [isPending, setIsPending] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const execute = useCallback(async (args: TopUpPositionArgs) => {
    if (!userAddress) throw new Error("Wallet not connected");
    setIsPending(true);
    setError(null);
    try {
      const vaultAddr = await getVaultAddress(userAddress);
      const hash = await writeContract({
        address: vaultAddr,
        abi: TradingAccountABI,
        functionName: "topUpPosition",
        args: [args.positionId, args.amount],
      });
      await waitForTx(hash);
    } catch (e) {
      const err = e instanceof Error ? e : new Error(String(e));
      setError(err);
      throw err;
    } finally {
      setIsPending(false);
    }
  }, [userAddress]);

  return { execute, isPending, error };
}

export function useTransferMargin(userAddress: `0x${string}` | undefined): {
  execute: (args: TransferMarginArgs) => Promise<void>;
  isPending: boolean;
  error: Error | null;
} {
  const [isPending, setIsPending] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const execute = useCallback(async (args: TransferMarginArgs) => {
    if (!userAddress) throw new Error("Wallet not connected");
    setIsPending(true);
    setError(null);
    try {
      const vaultAddr = await getVaultAddress(userAddress);
      const hash = await writeContract({
        address: vaultAddr,
        abi: TradingAccountABI,
        functionName: "transferMargin",
        args: [args.fromPositionId, args.toPositionId, args.amount],
      });
      await waitForTx(hash);
    } catch (e) {
      const err = e instanceof Error ? e : new Error(String(e));
      setError(err);
      throw err;
    } finally {
      setIsPending(false);
    }
  }, [userAddress]);

  return { execute, isPending, error };
}

export function useAccountSummary(
  userAddress: `0x${string}` | undefined,
  tokenAddress: `0x${string}` | undefined
): {
  data: AccountSummary | undefined;
  isLoading: boolean;
  error: Error | null;
  refetch: () => void;
} {
  const [data, setData] = useState<AccountSummary | undefined>();
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const mountedRef = useRef(true);

  const refetch = useCallback(async () => {
    if (!userAddress || !tokenAddress) return;
    setIsLoading(true);
    setError(null);
    try {
      const vaultAddr = await getVaultAddress(userAddress);
      const zeroAddr = "0x0000000000000000000000000000000000000000" as `0x${string}`;
      if (vaultAddr === zeroAddr) return;

      const result = await readContract({
        address: vaultAddr,
        abi: TradingAccountABI,
        functionName: "getAccountSummary",
        args: [tokenAddress],
      }) as { idle: bigint; totalLocked: bigint; positionCount: bigint };

      if (mountedRef.current) {
        setData({
          idle: result.idle,
          totalLocked: result.totalLocked,
          positionCount: result.positionCount,
        });
      }
    } catch (e) {
      if (mountedRef.current) setError(e instanceof Error ? e : new Error(String(e)));
    } finally {
      if (mountedRef.current) setIsLoading(false);
    }
  }, [userAddress, tokenAddress]);

  useEffect(() => {
    mountedRef.current = true;
    refetch();
    return () => { mountedRef.current = false; };
  }, [refetch]);

  return { data, isLoading, error, refetch };
}

export function useEmergencyWithdraw(userAddress: `0x${string}` | undefined): {
  execute: (token: `0x${string}`) => Promise<void>;
  isPending: boolean;
  error: Error | null;
} {
  const [isPending, setIsPending] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const execute = useCallback(async (token: `0x${string}`) => {
    if (!userAddress) throw new Error("Wallet not connected");
    setIsPending(true);
    setError(null);
    try {
      const vaultAddr = await getVaultAddress(userAddress);
      const hash = await writeContract({
        address: vaultAddr,
        abi: TradingAccountABI,
        functionName: "emergencyWithdraw",
        args: [token],
      });
      await waitForTx(hash);
    } catch (e) {
      const err = e instanceof Error ? e : new Error(String(e));
      setError(err);
      throw err;
    } finally {
      setIsPending(false);
    }
  }, [userAddress]);

  return { execute, isPending, error };
}
