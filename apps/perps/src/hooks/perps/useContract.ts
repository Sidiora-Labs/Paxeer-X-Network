import type { Address } from "viem";
import { getPublicClient, getWalletClient } from "@/core/viem/clients";
import { paxeerChain } from "@/core/chain";
import { createLogger } from "@/core/log/logger";
import { gasWithMargin } from "@/core/network/gas";

const log = createLogger("perps:contract");

export async function readContract(params: {
  address: Address;
  abi: readonly unknown[];
  functionName: string;
  args?: readonly unknown[];
}) {
  const client = getPublicClient();
  return client.readContract(params as any);
}

// The chain enforces a minimum gas price of 10 wei. The RPC's fee
// suggestions (base fee ~0 on this chain) can come back below that,
// and a wallet that trusts them signs a tx the chain never includes.
// Clamp both EIP-1559 fee fields to the floor before sending.
const MIN_FEE_PER_GAS_WEI = 10n;

async function estimateClampedFees(): Promise<{
  maxFeePerGas: bigint;
  maxPriorityFeePerGas: bigint;
}> {
  let maxFeePerGas = MIN_FEE_PER_GAS_WEI;
  let maxPriorityFeePerGas = MIN_FEE_PER_GAS_WEI;
  try {
    const est = await getPublicClient().estimateFeesPerGas();
    if (est.maxFeePerGas && est.maxFeePerGas > maxFeePerGas) {
      maxFeePerGas = est.maxFeePerGas;
    }
    if (est.maxPriorityFeePerGas && est.maxPriorityFeePerGas > maxPriorityFeePerGas) {
      maxPriorityFeePerGas = est.maxPriorityFeePerGas;
    }
  } catch (err) {
    // Estimation failing is not fatal — the floor alone is a valid fee
    // on this chain. Log so a broken fee endpoint is still visible.
    const msg = err instanceof Error ? err.message : String(err);
    log.warn(`estimateFeesPerGas failed, using ${MIN_FEE_PER_GAS_WEI} wei floor: ${msg}`);
  }
  // maxFee must always cover the priority tip.
  if (maxPriorityFeePerGas > maxFeePerGas) maxFeePerGas = maxPriorityFeePerGas;
  return { maxFeePerGas, maxPriorityFeePerGas };
}

export async function writeContract(params: {
  address: Address;
  abi: readonly unknown[];
  functionName: string;
  args?: readonly unknown[];
  value?: bigint;
}): Promise<`0x${string}`> {
  const walletClient = await getWalletClient();
  if (!walletClient) {
    log.error("No wallet client; wallet probably disconnected");
    throw new Error("Wallet not connected — please connect your wallet");
  }

  const account = walletClient.account;
  if (!account) {
    log.error("Wallet client has no account");
    throw new Error("No active account");
  }

  // Preflight: simulate the call against the public RPC so a genuine
  // revert surfaces its decoded reason HERE. Without this, the embedded
  // wallet backend simulates server-side and a revert comes back as an
  // opaque `request failed: 500` that the UI mislabels; injected
  // wallets would burn gas on a doomed tx instead.
  const call = {
    account: account.address,
    address: params.address,
    abi: params.abi,
    functionName: params.functionName,
    args: params.args ?? [],
    ...(params.value ? { value: params.value } : {}),
  } as any;
  const publicClient = getPublicClient();
  await publicClient.simulateContract(call);

  // Gas limit: eth_estimateGas on the public client plus a 20% margin,
  // which covers log gas of a fill the simulation did not see. Fees are
  // estimated here and clamped to the chain's 10 wei minimum gas price —
  // wallet-side estimation was producing sub-floor fees the chain
  // silently never included.
  const gas = gasWithMargin(await publicClient.estimateContractGas(call));
  const { maxFeePerGas, maxPriorityFeePerGas } = await estimateClampedFees();

  return walletClient.writeContract({
    chain: paxeerChain,
    account,
    address: params.address,
    abi: params.abi,
    functionName: params.functionName,
    args: params.args ?? [],
    gas,
    maxFeePerGas,
    maxPriorityFeePerGas,
    ...(params.value ? { value: params.value } : {}),
  } as any);
}

// Transaction confirmation.
//
// The chain is a Sei-EVM fork with instant CometBFT finality (no
// reorgs), so confirmation only needs ONE RPC method:
// `eth_getTransactionReceipt`, polled until the receipt lands.
//
// We deliberately do NOT use viem's `waitForTransactionReceipt`. Its
// replacement-detection path gates every receipt fetch behind
// `eth_getTransactionByHash` inside a 6-attempt exponential retry
// ladder (~12.6 s per cycle). Sei-lineage RPCs only surface txs by
// hash once the async EVM indexer flushes them, and report not-found
// as a JSON-RPC *error* rather than geth's `null` — so viem either
// burns the whole timeout without ever asking for the receipt, or
// rejects with an unrecognized error after ~13 s of retries. Both
// showed up as "tx failed" toasts for transactions that confirmed
// on-chain in under a second. Replacement detection buys nothing on
// an instant-finality chain with no fee-bump UX, so we drop it.
//
// A reverted receipt throws; a missing receipt past the deadline
// throws a timeout the error classifier maps to `RPC_TX_TIMEOUT`.
// We never synthesize success.

const RECEIPT_TIMEOUT_MS = 30_000;
const RECEIPT_POLL_INTERVAL_MS = 400;

export interface WaitForTxResult {
  status: "success";
  transactionHash: `0x${string}`;
  blockNumber: bigint;
}

/**
 * True when an error means "receipt not indexed yet — keep polling".
 * Covers viem's TransactionReceiptNotFoundError (RPC returned null)
 * and Sei-style error-shaped not-found responses.
 */
function isReceiptPendingError(err: unknown): boolean {
  const name = err instanceof Error ? err.name : "";
  if (name === "TransactionReceiptNotFoundError") return true;
  const msg = err instanceof Error ? err.message : String(err);
  return /not\s*(be\s*)?found|does not exist|indexing|not indexed/i.test(msg);
}

export async function waitForTx(hash: `0x${string}`): Promise<WaitForTxResult> {
  const client = getPublicClient();
  const deadline = Date.now() + RECEIPT_TIMEOUT_MS;

  while (Date.now() < deadline) {
    let receipt: Awaited<ReturnType<typeof client.getTransactionReceipt>> | null = null;
    try {
      receipt = await client.getTransactionReceipt({ hash });
    } catch (err: unknown) {
      if (!isReceiptPendingError(err)) throw err;
    }

    if (receipt) {
      if (receipt.status === "reverted") {
        throw new Error(`Transaction reverted (hash: ${hash})`);
      }
      return {
        status: "success",
        transactionHash: hash,
        blockNumber: receipt.blockNumber,
      };
    }

    await new Promise((resolve) => setTimeout(resolve, RECEIPT_POLL_INTERVAL_MS));
  }

  throw new Error(
    `Transaction timed out — not confirmed within ${RECEIPT_TIMEOUT_MS}ms (hash: ${hash})`,
  );
}
