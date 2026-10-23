"use client";

/**
 * The two settlement writes, shared by the bridge card's follow-through and
 * the transfer panels: claim a deposit with bridgeIn on Paxeer, and release
 * a withdrawal with PaxeerXVault.release on the foreign chain. Everything
 * the backend says is re-derived and re-verified against chain state
 * before the wallet is asked for a signature.
 */
import { getAddress, parseEventLogs } from "viem";
import { getBlockNumber, readContract, getTransactionReceipt } from "wagmi/actions";

import type { DepositAttestation, ReleaseAttestation } from "@/lib/bridge/attestation-api";
import { bridgeChainByKey, paxeer } from "@/lib/bridge/chains";
import { layerxBridgeAbi, LAYERX_BRIDGE_ADDRESS, paxeerXVaultAbi } from "@/lib/bridge/contracts";
import { inboundDigest, outboundDigest, verifyThresholdSignatures } from "@/lib/bridge/digests";
import type { BridgeTransfer } from "@/lib/bridge/transfers";
import { wagmiConfig } from "@/lib/bridge/wagmi";

export type TransferPhase =
  | "confirming"
  | "attesting"
  | "ready"
  | "settling"
  | "settled"
  | "failed";

export type TransferLive = {
  confirmations: number;
  requiredConfirmations: number;
  credited: boolean;
  attestationReady: boolean;
  sourceFailed: boolean;
};

/** Source-chain confirmation count and receipt status for a transfer. */
export async function readTransferLive(transfer: BridgeTransfer): Promise<TransferLive> {
  const chain = bridgeChainByKey(transfer.foreignChainKey);
  const sourceChainId = transfer.direction === "in" ? chain?.viemChain?.id : paxeer.id;
  if (sourceChainId === undefined) {
    return {
      confirmations: 0,
      requiredConfirmations: transfer.requiredConfirmations,
      credited: false,
      attestationReady: false,
      sourceFailed: false,
    };
  }
  const [receipt, head] = await Promise.all([
    getTransactionReceipt(wagmiConfig, { hash: transfer.sourceTxHash, chainId: sourceChainId }).catch(() => null),
    getBlockNumber(wagmiConfig, { chainId: sourceChainId }).catch(() => null),
  ]);
  let credited = false;
  if (transfer.direction === "in" && transfer.logIndex !== undefined) {
    credited = await readContract(wagmiConfig, {
      abi: layerxBridgeAbi,
      address: LAYERX_BRIDGE_ADDRESS,
      functionName: "isNullified",
      args: [BigInt(transfer.foreignChainId), transfer.sourceTxHash, BigInt(transfer.logIndex)],
      chainId: paxeer.id,
    }).catch(() => false) as boolean;
  }
  const confirmations =
    receipt && head !== null ? Math.max(0, Number(head - receipt.blockNumber) + 1) : 0;
  return {
    confirmations,
    requiredConfirmations: transfer.requiredConfirmations,
    credited,
    attestationReady: false,
    sourceFailed: receipt !== null && receipt.status === "reverted",
  };
}

export type VerifiedClaim = {
  request: {
    abi: typeof layerxBridgeAbi;
    address: typeof LAYERX_BRIDGE_ADDRESS;
    functionName: "bridgeIn";
    args: [bigint, `0x${string}`, `0x${string}`, bigint, `0x${string}`, `0x${string}`, bigint, `0x${string}`[]];
    chainId: number;
  };
};

export async function buildVerifiedClaim(
  transfer: BridgeTransfer,
  attestation: DepositAttestation,
): Promise<VerifiedClaim> {
  const chainId = BigInt(transfer.foreignChainId);
  const chainInfo = (await readContract(wagmiConfig, {
    abi: layerxBridgeAbi,
    address: LAYERX_BRIDGE_ADDRESS,
    functionName: "getChain",
    args: [chainId],
    chainId: paxeer.id,
  })) as readonly [boolean, `0x${string}`, bigint, boolean];
  if (!chainInfo[0] || !chainInfo[3]) {
    throw new Error("This chain is not registered and enabled on Paxeer.");
  }
  const registeredVault = getAddress(chainInfo[1]);
  if (transfer.vault && registeredVault.toLowerCase() !== transfer.vault.toLowerCase()) {
    throw new Error("The vault registered on Paxeer does not match the deposit vault.");
  }
  if (getAddress(attestation.vault).toLowerCase() !== registeredVault.toLowerCase()) {
    throw new Error("The backend attested a different vault than the one registered on Paxeer.");
  }
  if (
    attestation.txHash.toLowerCase() !== transfer.sourceTxHash.toLowerCase() ||
    BigInt(attestation.logIndex) !== BigInt(transfer.logIndex ?? "-1") ||
    BigInt(attestation.amount) !== BigInt(transfer.amount)
  ) {
    throw new Error("The backend attestation does not match this deposit.");
  }
  const digest = inboundDigest({
    chainId,
    vault: registeredVault,
    txHash: transfer.sourceTxHash,
    logIndex: BigInt(attestation.logIndex),
    recipient32: attestation.recipient32,
    asset: attestation.asset,
    amount: BigInt(attestation.amount),
  });
  if (digest.toLowerCase() !== attestation.digest.toLowerCase()) {
    throw new Error("The backend digest does not match the deposit.");
  }
  const attestors = (await readContract(wagmiConfig, {
    abi: layerxBridgeAbi,
    address: LAYERX_BRIDGE_ADDRESS,
    functionName: "getAttestors",
    chainId: paxeer.id,
  })) as readonly [readonly `0x${string}`[], readonly bigint[], number];
  const check = await verifyThresholdSignatures({
    digest,
    signatures: attestation.signatures,
    attestors: attestors[0],
    threshold: Number(attestors[2]),
  });
  if (!check.ok) throw new Error(check.reason);
  return {
    request: {
      abi: layerxBridgeAbi,
      address: LAYERX_BRIDGE_ADDRESS,
      functionName: "bridgeIn",
      args: [
        chainId,
        registeredVault,
        transfer.sourceTxHash,
        BigInt(attestation.logIndex),
        attestation.recipient32,
        getAddress(attestation.asset),
        BigInt(attestation.amount),
        [...attestation.signatures],
      ],
      chainId: paxeer.id,
    },
  };
}

export type VerifiedRelease = {
  request: {
    abi: typeof paxeerXVaultAbi;
    address: `0x${string}`;
    functionName: "release";
    args: [`0x${string}`, bigint, `0x${string}`, `0x${string}`, bigint, `0x${string}`[]];
    chainId: number;
  };
};

export async function buildVerifiedRelease(
  transfer: BridgeTransfer,
  attestation: ReleaseAttestation,
): Promise<VerifiedRelease> {
  const chain = bridgeChainByKey(transfer.foreignChainKey);
  if (!chain?.viemChain || !transfer.vault) {
    throw new Error("This withdrawal cannot be released from an EVM wallet.");
  }
  const nonce = BigInt(transfer.bridgeOutNonce ?? "-1");
  if (
    attestation.paxeerTxHash.toLowerCase() !== transfer.sourceTxHash.toLowerCase() ||
    BigInt(attestation.paxeerNonce) !== nonce ||
    BigInt(attestation.amount) !== BigInt(transfer.amount) ||
    getAddress(attestation.recipient).toLowerCase() !== transfer.recipient.toLowerCase()
  ) {
    throw new Error("The backend attestation does not match this withdrawal.");
  }
  if (getAddress(attestation.vault).toLowerCase() !== transfer.vault.toLowerCase()) {
    throw new Error("The backend attested a different vault.");
  }
  const assetAddress = getAddress(attestation.asset);
  const onChainDigest = await readContract(wagmiConfig, {
    abi: paxeerXVaultAbi,
    address: transfer.vault,
    functionName: "releaseDigest",
    args: [transfer.sourceTxHash, nonce, getAddress(attestation.recipient), assetAddress, BigInt(attestation.amount)],
    chainId: chain.viemChain.id,
  });
  const localDigest = outboundDigest({
    chainId: BigInt(chain.viemChain.id),
    vault: transfer.vault,
    paxeerTxHash: transfer.sourceTxHash,
    paxeerNonce: nonce,
    recipient: getAddress(attestation.recipient),
    asset: assetAddress,
    amount: BigInt(attestation.amount),
  });
  if (
    localDigest.toLowerCase() !== attestation.digest.toLowerCase() ||
    (onChainDigest as string).toLowerCase() !== attestation.digest.toLowerCase()
  ) {
    throw new Error("The backend digest does not match the withdrawal.");
  }
  const [attestors, threshold] = await Promise.all([
    readContract(wagmiConfig, {
      abi: paxeerXVaultAbi,
      address: transfer.vault,
      functionName: "attestors",
      chainId: chain.viemChain.id,
    }),
    readContract(wagmiConfig, {
      abi: paxeerXVaultAbi,
      address: transfer.vault,
      functionName: "threshold",
      chainId: chain.viemChain.id,
    }),
  ]);
  const check = await verifyThresholdSignatures({
    digest: attestation.digest,
    signatures: attestation.signatures,
    attestors: attestors as readonly `0x${string}`[],
    threshold: Number(threshold),
  });
  if (!check.ok) throw new Error(check.reason);
  return {
    request: {
      abi: paxeerXVaultAbi,
      address: transfer.vault,
      functionName: "release",
      args: [
        assetAddress,
        BigInt(attestation.amount),
        getAddress(attestation.recipient),
        transfer.sourceTxHash,
        nonce,
        [...attestation.signatures],
      ],
      chainId: chain.viemChain.id,
    },
  };
}

/** Parse the BridgeOut nonce out of a Paxeer burn receipt. */
export function bridgeOutNonceFromReceipt(logs: readonly { address: string; topics: readonly string[]; data: string }[]): bigint | undefined {
  try {
    const events = parseEventLogs({ abi: layerxBridgeAbi, logs: logs as never, eventName: "BridgeOut" });
    const first = events[0] as { args?: { nonce?: bigint } } | undefined;
    return first?.args?.nonce;
  } catch {
    return undefined;
  }
}
