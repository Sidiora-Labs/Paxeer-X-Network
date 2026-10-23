"use client";

import { useQuery } from "@tanstack/react-query";
import { parseEventLogs } from "viem";
import { useEffect, useMemo, useState } from "react";
import { useAccount, useChainId, useSwitchChain, useWriteContract } from "wagmi";
import { getTransactionReceipt } from "wagmi/actions";

import { DepositStatus, type TransferStatusItem } from "@/components/trading/deposit-status";
import { TransferHistory, type TransferRecord } from "@/components/trading/transfer-history";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { toast } from "@/components/ui/sonner";
import { formatAmount } from "@/lib/bridge/amount";
import { bridgeChainByKey, finalityEstimate, paxeer } from "@/lib/bridge/chains";
import { paxeerXVaultAbi } from "@/lib/bridge/contracts";
import { describeWriteError } from "@/lib/bridge/errors";
import { foreignTxUrl, paxeerTxUrl } from "@/lib/bridge/explorer";
import {
  listTransfers,
  updateTransfer,
  type BridgeTransfer,
} from "@/lib/bridge/transfers";
import {
  attestationApiConfigured,
  useDepositAttestation,
  useDepositNullified,
  useReleaseAttestation,
} from "@/lib/bridge/use-bridge-state";
import { wagmiConfig } from "@/lib/bridge/wagmi";
import {
  bridgeOutNonceFromReceipt,
  buildVerifiedClaim,
  buildVerifiedRelease,
  readTransferLive,
  type TransferPhase,
} from "./transfer-actions";

function sourceChainIdOf(transfer: BridgeTransfer): number | undefined {
  const chain = bridgeChainByKey(transfer.foreignChainKey);
  return transfer.direction === "in" ? chain?.viemChain?.id : paxeer.id;
}

/** Enrich local records with the ids only the receipt knows (log index, nonce). */
async function enrichTransfers(list: BridgeTransfer[]): Promise<BridgeTransfer[]> {
  let changed = false;
  const next: BridgeTransfer[] = [];
  for (const transfer of list) {
    const needsLogIndex = transfer.direction === "in" && transfer.logIndex === undefined;
    const needsNonce = transfer.direction === "out" && transfer.bridgeOutNonce === undefined;
    const chainId = sourceChainIdOf(transfer);
    if ((!needsLogIndex && !needsNonce) || chainId === undefined) {
      next.push(transfer);
      continue;
    }
    const receipt = await getTransactionReceipt(wagmiConfig, {
      hash: transfer.sourceTxHash,
      chainId,
    }).catch(() => null);
    if (!receipt) {
      next.push(transfer);
      continue;
    }
    let enriched = transfer;
    if (needsLogIndex && transfer.vault) {
      try {
        const events = parseEventLogs({
          abi: paxeerXVaultAbi,
          logs: receipt.logs.filter((l) => l.address.toLowerCase() === transfer.vault!.toLowerCase()),
          eventName: "BridgeDeposit",
        });
        const hit = events.find(
          (e) =>
            (e.args.amount as bigint) === BigInt(transfer.amount) &&
            (e.args.sender as string).toLowerCase() === transfer.account.toLowerCase(),
        );
        if (hit && hit.logIndex !== undefined) {
          enriched = { ...enriched, logIndex: String(hit.logIndex), nonce: String(hit.args.nonce as bigint) };
        }
      } catch {
        // Not a vault deposit log — leave unenriched; tracking still works.
      }
    }
    if (needsNonce) {
      const nonce = bridgeOutNonceFromReceipt(receipt.logs);
      if (nonce !== undefined) enriched = { ...enriched, bridgeOutNonce: String(nonce) };
    }
    if (enriched !== transfer) {
      updateTransfer(enriched.id, {
        logIndex: enriched.logIndex,
        nonce: enriched.nonce,
        bridgeOutNonce: enriched.bridgeOutNonce,
      });
      changed = true;
    }
    next.push(enriched);
  }
  return changed ? next : list;
}

function phaseOf(
  transfer: BridgeTransfer,
  live: Awaited<ReturnType<typeof readTransferLive>> | undefined,
  attestationReady: boolean,
): TransferPhase {
  if (transfer.direction === "in" && (transfer.claimTxHash || live?.credited)) return "settled";
  if (transfer.direction === "out" && transfer.releaseTxHash) return "settled";
  if (live?.sourceFailed) return "failed";
  if (attestationReady) return "ready";
  if (live && live.confirmations > 0 && live.confirmations >= live.requiredConfirmations) return "attesting";
  return "confirming";
}

function TransferRow({
  transfer,
  onChanged,
}: {
  transfer: BridgeTransfer;
  onChanged: () => void;
}) {
  const chain = bridgeChainByKey(transfer.foreignChainKey);
  const walletChainId = useChainId();
  const { switchChain } = useSwitchChain();
  const settle = useWriteContract();
  const [settleHash, setSettleHash] = useState(transfer.claimTxHash ?? transfer.releaseTxHash);

  const settledLocally =
    (transfer.direction === "in" && transfer.claimTxHash !== undefined) ||
    (transfer.direction === "out" && transfer.releaseTxHash !== undefined);

  const liveQuery = useQuery({
    queryKey: ["transfer-live", transfer.id, settledLocally],
    queryFn: () => readTransferLive(transfer),
    enabled: !settledLocally,
    refetchInterval: 15_000,
  });
  const live = liveQuery.data;
  const nullified = useDepositNullified(settledLocally ? undefined : transfer);
  const credited = settledLocally || nullified.data === true || live?.credited === true;

  const depositAttestation = useDepositAttestation(transfer, credited);
  const releaseAttestation = useReleaseAttestation(transfer, settledLocally);
  const attestation = transfer.direction === "in" ? depositAttestation.data : releaseAttestation.data;
  const attestationReady = attestation?.status === "ready" && attestation.signatures.length > 0;

  const phase = phaseOf({ ...transfer, claimTxHash: credited ? (transfer.claimTxHash ?? settleHash) : transfer.claimTxHash }, live, attestationReady);

  // Persist a claim the chain already knows about (claimed from elsewhere).
  useEffect(() => {
    if (transfer.direction === "in" && nullified.data === true && !transfer.claimTxHash && !settleHash) {
      onChanged();
    }
  }, [nullified.data, onChanged, settleHash, transfer.claimTxHash, transfer.direction]);

  const settleChainId = transfer.direction === "in" ? paxeer.id : chain?.viemChain?.id;
  const onSettleChain = settleChainId !== undefined && walletChainId === settleChainId;

  const runSettle = async () => {
    try {
      if (transfer.direction === "in") {
        if (!depositAttestation.data) throw new Error("The attestation is not available yet.");
        const { request } = await buildVerifiedClaim(transfer, depositAttestation.data);
        settle.writeContract(request, {
          onSuccess: (hash) => {
            setSettleHash(hash);
            updateTransfer(transfer.id, { claimTxHash: hash, claimedAt: Date.now() });
            toast.success("Claim submitted", { description: "The bridged balance lands once the Paxeer transaction confirms." });
            onChanged();
          },
          onError: (error) => toast.error("Claim failed", { description: describeWriteError(error) }),
        });
      } else {
        if (!releaseAttestation.data) throw new Error("The attestation is not available yet.");
        const { request } = await buildVerifiedRelease(transfer, releaseAttestation.data);
        settle.writeContract(request, {
          onSuccess: (hash) => {
            setSettleHash(hash);
            updateTransfer(transfer.id, { releaseTxHash: hash, releasedAt: Date.now() });
            toast.success("Release submitted", { description: `The vault pays out on ${chain?.label ?? "the foreign chain"} once it confirms.` });
            onChanged();
          },
          onError: (error) => toast.error("Release failed", { description: describeWriteError(error) }),
        });
      }
    } catch (error) {
      toast.error("Could not verify the attestation", {
        description: error instanceof Error ? error.message : "Verification failed.",
      });
    }
  };

  const amountNumber = Number(formatAmount(BigInt(transfer.amount), transfer.decimals));
  const sourceUrl =
    transfer.direction === "in"
      ? chain
        ? foreignTxUrl(chain, transfer.sourceTxHash)
        : undefined
      : paxeerTxUrl(transfer.sourceTxHash);

  const statusItem: TransferStatusItem = {
    id: transfer.id,
    type: transfer.direction === "in" ? "deposit" : "withdraw",
    token: transfer.symbol,
    amount: amountNumber,
    network: chain?.label,
    step:
      phase === "settled"
        ? "credited"
        : phase === "failed"
          ? "failed"
          : phase === "confirming"
            ? "confirming"
            : "submitted",
    confirmations: live?.confirmations,
    requiredConfirmations: live?.requiredConfirmations ?? transfer.requiredConfirmations,
    time: new Date(transfer.submittedAt).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }),
    eta: phase === "confirming" && chain ? finalityEstimate(chain) : undefined,
    txHash: transfer.sourceTxHash,
  };

  return (
    <div className="flex flex-col gap-2">
      <DepositStatus items={[statusItem]} explorerUrl={() => sourceUrl} defaultOpen={phase === "ready"} />
      {phase === "attesting" && !attestationApiConfigured ? (
        <p className="px-1 text-[11px] text-muted-foreground">
          Finality reached. The signature bundle comes from the bridge backend, which is not configured in this
          deployment — the {transfer.direction === "in" ? "claim" : "release"} button appears once it is.
        </p>
      ) : null}
      {phase === "attesting" && attestationApiConfigured ? (
        <p className="flex items-center gap-1.5 px-1 text-[11px] text-muted-foreground">
          <Spinner className="size-3" /> Waiting for the attestor threshold…
        </p>
      ) : null}
      {phase === "ready" ? (
        onSettleChain ? (
          <Button size="sm" className="self-start" disabled={settle.isPending} onClick={() => void runSettle()}>
            {settle.isPending ? <Spinner /> : null}
            {transfer.direction === "in" ? "Claim on Paxeer" : `Release on ${chain?.label ?? "destination"}`}
          </Button>
        ) : (
          <Button
            size="sm"
            variant="tonal"
            className="self-start"
            onClick={() => settleChainId && switchChain({ chainId: settleChainId })}
          >
            Switch to {transfer.direction === "in" ? "Paxeer" : (chain?.label ?? "destination")} to{" "}
            {transfer.direction === "in" ? "claim" : "release"}
          </Button>
        )
      ) : null}
      {phase === "failed" ? (
        <p className="px-1 text-[11px] text-destructive">The source transaction reverted. No funds moved.</p>
      ) : null}
    </div>
  );
}

export function TransfersPanel({ refreshKey }: { refreshKey: number }) {
  const { address, isConnected } = useAccount();
  const [version, setVersion] = useState(0);

  const transfersQuery = useQuery({
    queryKey: ["bridge-transfers", address, refreshKey, version],
    queryFn: async () => enrichTransfers(listTransfers(address)),
    enabled: isConnected && address !== undefined,
    refetchInterval: 30_000,
  });
  const transfers = useMemo(() => transfersQuery.data ?? [], [transfersQuery.data]);

  const records: TransferRecord[] = transfers.map((t) => {
    const chain = bridgeChainByKey(t.foreignChainKey);
    const settled =
      (t.direction === "in" && t.claimTxHash !== undefined) ||
      (t.direction === "out" && t.releaseTxHash !== undefined);
    return {
      id: t.id,
      type: t.direction === "in" ? "deposit" : "withdraw",
      token: t.symbol,
      amount: Number(formatAmount(BigInt(t.amount), t.decimals)),
      decimals: Math.min(t.decimals, 6),
      fee: null,
      network: chain?.label,
      status: settled ? "completed" : "processing",
      time: t.submittedAt,
      txHash: t.sourceTxHash,
    };
  });

  const pending = transfers.filter(
    (t) =>
      !((t.direction === "in" && t.claimTxHash) || (t.direction === "out" && t.releaseTxHash)),
  );

  return (
    <section id="transfers" aria-label="Your bridge transfers" className="flex w-full flex-col gap-4">
      <div className="flex items-baseline justify-between gap-3">
        <h2 className="text-lg font-semibold tracking-tight">Your transfers</h2>
        <span className="text-xs text-muted-foreground">This browser's record — chain state is re-read live.</span>
      </div>

      {!isConnected ? (
        <Alert variant="info">
          <AlertTitle>Connect a wallet to see your transfers</AlertTitle>
          <AlertDescription>
            Deposits and withdrawals you start here are tracked per wallet. A transfer started elsewhere is still
            claimable — its source transaction is the record.
          </AlertDescription>
        </Alert>
      ) : (
        <>
          {pending.length > 0 ? (
            <div className="flex flex-col gap-3">
              {pending.map((transfer) => (
                <TransferRow key={transfer.id} transfer={transfer} onChanged={() => setVersion((v) => v + 1)} />
              ))}
            </div>
          ) : null}
          <TransferHistory
            records={records}
            filters={["all", "deposit", "withdraw"]}
            loading={transfersQuery.isLoading}
            emptyText="No bridge transfers from this wallet yet. Your first deposit or withdrawal will appear here."
            explorerUrl={(record) => {
              const transfer = transfers.find((t) => t.id === record.id);
              if (!transfer) return undefined;
              const chain = bridgeChainByKey(transfer.foreignChainKey);
              return transfer.direction === "in"
                ? chain
                  ? foreignTxUrl(chain, transfer.sourceTxHash)
                  : undefined
                : paxeerTxUrl(transfer.sourceTxHash);
            }}
            onCopy={() => toast.success("Copied")}
          />
        </>
      )}
    </section>
  );
}
