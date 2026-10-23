"use client";

/**
 * Live bridge reads — the truth layer.
 *
 * Paxeer side (precompile 0x…1016): isPaused, getChain, getCap and
 * getAttestors describe whether a route is even open. Foreign side
 * (PaxeerXVault): paused, caps, outstanding and attestors/threshold. The
 * static registry only says what was configured; these reads say what is.
 * Balances: the foreign wallet balance via wagmi, and the Paxeer-side
 * bridged balance via the Bank precompile against the denom getCap
 * returns — never a client-computed denom.
 */
import { useQuery } from "@tanstack/react-query";
import { useAccount, useBalance, useReadContract, type UseReadContractReturnType } from "wagmi";
import type { Address } from "viem";

import { bankAbi, BANK_ADDRESS, layerxBridgeAbi, LAYERX_BRIDGE_ADDRESS, paxeerXVaultAbi } from "./contracts";
import { bridgeConfig } from "./config";
import { paxeer as paxeerChain, type BridgeChain } from "./chains";
import { fetchDepositAttestation, fetchReleaseAttestation } from "./attestation-api";
import type { BridgeTransfer } from "./transfers";

export const paxeerConfigured = bridgeConfig.paxeerRpcUrl !== undefined;
export const attestationApiConfigured = bridgeConfig.attestationApiUrl !== undefined;

const REFRESH_MS = 20_000;
const POLL_MS = 12_000;

export type PaxeerPairState = {
  paused: boolean | undefined;
  registered: boolean;
  enabled: boolean;
  vault: Address | undefined;
  finalityDepth: number | undefined;
  denom: string | undefined;
  maxInFlight: bigint;
  maxPerTx: bigint;
  inFlight: bigint;
  attestors: readonly Address[];
  threshold: number | undefined;
  loaded: boolean;
  error: boolean;
};

export function usePaxeerPairState(chain: BridgeChain, assetId: Address): PaxeerPairState {
  const query = { refetchInterval: REFRESH_MS, enabled: paxeerConfigured } as const;
  const paused = useReadContract({
    abi: layerxBridgeAbi,
    address: LAYERX_BRIDGE_ADDRESS,
    functionName: "isPaused",
    chainId: paxeerChain.id,
    query,
  }) as UseReadContractReturnType<typeof layerxBridgeAbi, "isPaused">;
  const chainInfo = useReadContract({
    abi: layerxBridgeAbi,
    address: LAYERX_BRIDGE_ADDRESS,
    functionName: "getChain",
    args: [chain.chainId],
    chainId: paxeerChain.id,
    query,
  }) as UseReadContractReturnType<typeof layerxBridgeAbi, "getChain">;
  const cap = useReadContract({
    abi: layerxBridgeAbi,
    address: LAYERX_BRIDGE_ADDRESS,
    functionName: "getCap",
    args: [chain.chainId, assetId],
    chainId: paxeerChain.id,
    query,
  }) as UseReadContractReturnType<typeof layerxBridgeAbi, "getCap">;
  const attestors = useReadContract({
    abi: layerxBridgeAbi,
    address: LAYERX_BRIDGE_ADDRESS,
    functionName: "getAttestors",
    chainId: paxeerChain.id,
    query,
  }) as UseReadContractReturnType<typeof layerxBridgeAbi, "getAttestors">;

  const chainData = chainInfo.data as
    | readonly [boolean, Address, bigint, boolean]
    | undefined;
  const capData = cap.data as
    | readonly [string, bigint, bigint, bigint]
    | undefined;
  const attestorData = attestors.data as
    | readonly [readonly Address[], readonly bigint[], number]
    | undefined;

  const loaded =
    paxeerConfigured && !paused.isLoading && !chainInfo.isLoading && !cap.isLoading && !attestors.isLoading;
  const error = paxeerConfigured && (paused.isError || chainInfo.isError || cap.isError || attestors.isError);

  return {
    paused: paused.data as boolean | undefined,
    registered: chainData?.[0] ?? false,
    enabled: chainData?.[3] ?? false,
    vault: chainData?.[1],
    finalityDepth: chainData ? Number(chainData[2]) : undefined,
    denom: capData?.[0] || undefined,
    maxInFlight: capData?.[1] ?? 0n,
    maxPerTx: capData?.[2] ?? 0n,
    inFlight: capData?.[3] ?? 0n,
    attestors: attestorData?.[0] ?? [],
    threshold: attestorData ? Number(attestorData[2]) : undefined,
    loaded,
    error,
  };
}

export type VaultState = {
  paused: boolean | undefined;
  perTx: bigint;
  total: bigint;
  outstanding: bigint;
  attestors: readonly Address[];
  threshold: number | undefined;
  loaded: boolean;
  error: boolean;
};

export function useVaultState(chain: BridgeChain, assetAddress: Address): VaultState {
  const vault = chain.vault;
  const chainId = chain.viemChain?.id;
  const enabled = vault !== undefined && chainId !== undefined;
  const query = { refetchInterval: REFRESH_MS, enabled } as const;

  const paused = useReadContract({
    abi: paxeerXVaultAbi,
    address: vault,
    functionName: "paused",
    chainId,
    query,
  });
  const caps = useReadContract({
    abi: paxeerXVaultAbi,
    address: vault,
    functionName: "caps",
    args: [assetAddress],
    chainId,
    query,
  });
  const outstanding = useReadContract({
    abi: paxeerXVaultAbi,
    address: vault,
    functionName: "outstanding",
    args: [assetAddress],
    chainId,
    query,
  });
  const attestors = useReadContract({
    abi: paxeerXVaultAbi,
    address: vault,
    functionName: "attestors",
    chainId,
    query,
  });
  const threshold = useReadContract({
    abi: paxeerXVaultAbi,
    address: vault,
    functionName: "threshold",
    chainId,
    query,
  });

  const capsData = caps.data as readonly [bigint, bigint] | undefined;

  return {
    paused: paused.data,
    perTx: capsData?.[0] ?? 0n,
    total: capsData?.[1] ?? 0n,
    outstanding: outstanding.data ?? 0n,
    attestors: attestors.data ?? [],
    threshold: threshold.data !== undefined ? Number(threshold.data) : undefined,
    loaded:
      enabled &&
      !paused.isLoading &&
      !caps.isLoading &&
      !outstanding.isLoading &&
      !attestors.isLoading &&
      !threshold.isLoading,
    error:
      enabled &&
      (paused.isError || caps.isError || outstanding.isError || attestors.isError || threshold.isError),
  };
}

/** Foreign-chain wallet balance of an asset (native coin or ERC-20). */
export function useForeignBalance(chain: BridgeChain, assetAddress: Address, isNative: boolean) {
  const { address } = useAccount();
  return useBalance({
    address,
    chainId: chain.viemChain?.id,
    token: isNative ? undefined : assetAddress,
    query: { refetchInterval: REFRESH_MS, enabled: address !== undefined && chain.viemChain !== undefined },
  });
}

/** Paxeer-side bridged balance of a denom, via the Bank precompile. */
export function useBridgedBalance(denom: string | undefined) {
  const { address } = useAccount();
  return useReadContract({
    abi: bankAbi,
    address: BANK_ADDRESS,
    functionName: "balance",
    args: address && denom ? [address, denom] : undefined,
    chainId: paxeerChain.id,
    query: {
      refetchInterval: REFRESH_MS,
      enabled: paxeerConfigured && address !== undefined && denom !== undefined,
    },
  });
}

/** Poll the backend for a deposit's attestation until it is ready. */
export function useDepositAttestation(transfer: BridgeTransfer | undefined, settled: boolean) {
  return useQuery({
    queryKey: ["deposit-attestation", transfer?.id],
    enabled:
      attestationApiConfigured &&
      !settled &&
      transfer !== undefined &&
      transfer.direction === "in" &&
      transfer.logIndex !== undefined,
    queryFn: () =>
      fetchDepositAttestation({
        chainId: BigInt(transfer!.foreignChainId),
        txHash: transfer!.sourceTxHash,
        logIndex: BigInt(transfer!.logIndex!),
      }),
    refetchInterval: (query) => (query.state.data?.status === "ready" ? false : POLL_MS),
  });
}

/** Poll the backend for a withdrawal's release attestation until ready. */
export function useReleaseAttestation(transfer: BridgeTransfer | undefined, settled: boolean) {
  return useQuery({
    queryKey: ["release-attestation", transfer?.id],
    enabled:
      attestationApiConfigured &&
      !settled &&
      transfer !== undefined &&
      transfer.direction === "out" &&
      transfer.bridgeOutNonce !== undefined,
    queryFn: () =>
      fetchReleaseAttestation({
        chainId: BigInt(transfer!.foreignChainId),
        paxeerTxHash: transfer!.sourceTxHash,
        paxeerNonce: BigInt(transfer!.bridgeOutNonce!),
      }),
    refetchInterval: (query) => (query.state.data?.status === "ready" ? false : POLL_MS),
  });
}

/** isNullified for a deposit — the precompile's own record of a completed claim. */
export function useDepositNullified(transfer: BridgeTransfer | undefined) {
  return useReadContract({
    abi: layerxBridgeAbi,
    address: LAYERX_BRIDGE_ADDRESS,
    functionName: "isNullified",
    args:
      transfer && transfer.direction === "in" && transfer.logIndex !== undefined
        ? [BigInt(transfer.foreignChainId), transfer.sourceTxHash, BigInt(transfer.logIndex)]
        : undefined,
    chainId: paxeerChain.id,
    query: {
      refetchInterval: REFRESH_MS,
      enabled:
        paxeerConfigured &&
        transfer !== undefined &&
        transfer.direction === "in" &&
        transfer.logIndex !== undefined,
    },
  }) as UseReadContractReturnType<typeof layerxBridgeAbi, "isNullified">;
}
