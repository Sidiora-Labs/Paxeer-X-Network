import { useEffect, useRef, useState } from "react";

import { getPublicClient } from "@/core/viem/clients";
import type { Block } from "@/types/paxscan/api/block";
import type { AddressParam } from "@/types/paxscan/api/addressParams";

/**
 * Subscribes to new blocks over the standard EVM JSON-RPC using viem's
 * `watchBlocks` against the Paxeer RPC (api.hyperpax.xyz). The chain is a
 * reliable Sei-EVM fork, so we use plain `eth_getBlockByNumber` polling —
 * no Tendermint/CometBFT-specific WebSocket or hash workarounds. Block
 * hashes are standard 0x… EVM hashes, matching the Blockscout REST seed.
 *
 * Keeps the rest of the explorer (REST seed, detail panels) on Paxscan —
 * this hook only powers the live-update stream.
 */

// Poll cadence for new heads. The fallback HTTP transport is used, so this
// is an eth_blockNumber/eth_getBlockByNumber poll. Kept tight because the
// chain produces sub-second blocks and the RPC is reliable.
const DEFAULT_POLL_INTERVAL_MS = 1_000;

// ── EVM block → Block adapter ───────────────────────────────────────────

function makeMiner(address: string | undefined | null): AddressParam {
  const hash = address ?? "0x0000000000000000000000000000000000000000";
  return {
    hash,
    implementations: null,
    name: null,
    is_contract: false,
    is_verified: null,
    ens_domain_name: null,
    private_tags: null,
    watchlist_names: null,
    public_tags: null,
  };
}

type ViemBlock = Awaited<
  ReturnType<ReturnType<typeof getPublicClient>["getBlock"]>
>;

function evmToBlock(block: ViemBlock): Block | null {
  if (block.number === null || block.hash === null) return null;

  const gasUsed = block.gasUsed;
  const gasLimit = block.gasLimit;
  const gasUsedPct =
    gasLimit > 0n ? Number((gasUsed * 10000n) / gasLimit) / 100 : null;

  return {
    height: Number(block.number),
    timestamp: new Date(Number(block.timestamp) * 1000).toISOString(),
    hash: block.hash,
    parent_hash: block.parentHash,
    transactions_count: block.transactions.length,
    internal_transactions_count: 0,
    miner: makeMiner(block.miner),
    size: block.size !== undefined ? Number(block.size) : undefined,
    gas_used: gasUsed.toString(),
    gas_limit: gasLimit.toString(),
    nonce: block.nonce ?? "0x0",
    base_fee_per_gas:
      block.baseFeePerGas !== null && block.baseFeePerGas !== undefined
        ? block.baseFeePerGas.toString()
        : null,
    burnt_fees: null,
    priority_fee: null,
    extra_data: block.extraData ?? null,
    state_root: block.stateRoot ?? null,
    rewards: [],
    gas_target_percentage: null,
    gas_used_percentage: gasUsedPct,
    burnt_fees_percentage: null,
    type: "block",
    transaction_fees: null,
    uncles_hashes: [],
  };
}

// ── Hook ─────────────────────────────────────────────────────────────────

export interface UseEvmBlockStreamOptions {
  /** Override the poll interval in ms (default 1000). */
  pollingIntervalMs?: number;
  /** Called once per new block, after adaptation. */
  onBlock?: (block: Block) => void;
  /** Disable the subscription entirely. */
  isDisabled?: boolean;
}

export interface EvmBlockStreamState {
  isConnected: boolean;
}

export function useEvmBlockStream(
  options: UseEvmBlockStreamOptions = {},
): EvmBlockStreamState {
  const {
    pollingIntervalMs = DEFAULT_POLL_INTERVAL_MS,
    onBlock,
    isDisabled = false,
  } = options;

  // Keep `onBlock` in a ref so changes don't tear down the watcher.
  const onBlockRef = useRef(onBlock);
  useEffect(() => {
    onBlockRef.current = onBlock;
  }, [onBlock]);

  const [isConnected, setIsConnected] = useState(false);

  useEffect(() => {
    if (isDisabled) return;
    if (typeof window === "undefined") return;

    const client = getPublicClient();
    let cancelled = false;

    // viem's watchBlocks polls eth_getBlockByNumber and emits each new head.
    // emitMissed backfills any blocks skipped between polls so the feed stays
    // gap-free even if a poll lands late.
    const unwatch = client.watchBlocks({
      emitMissed: true,
      includeTransactions: false,
      pollingInterval: pollingIntervalMs,
      onBlock: (block) => {
        if (cancelled) return;
        setIsConnected(true);
        const adapted = evmToBlock(block as ViemBlock);
        if (adapted) onBlockRef.current?.(adapted);
      },
      onError: (err) => {
        if (cancelled) return;
        console.warn("[evm-block-stream] watch error", err);
        setIsConnected(false);
      },
    });

    return () => {
      cancelled = true;
      try {
        unwatch();
      } catch {}
      setIsConnected(false);
    };
  }, [pollingIntervalMs, isDisabled]);

  return { isConnected };
}
