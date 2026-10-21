import { useCallback, useEffect, useRef, useState } from "react";
import { Radio, WifiOff } from "lucide-react";
import { cn } from "@/ui/utils/cn";

import { useBlockscoutInfiniteQuery } from "@/hooks/paxscan/useBlockscoutInfiniteQuery";
import { useEvmBlockStream } from "@/hooks/chain/useEvmBlockStream";
import type { Block } from "@/types/paxscan/api/block";

import { BlockRow } from "./BlockRow";

interface LiveBlockStreamProps {
  /** Called when a row is clicked. Parent (explorer page) opens the detail panel. */
  onSelectBlock: (block: Block) => void;
  /** Selected block's hash — we pass it through to BlockRow for a subtle ring. */
  selectedBlockHash?: string;
  /**
   * Hard cap on retained blocks in memory. Default 100. Older blocks are
   * evicted. Lower this if profiling shows render-cost issues.
   */
  maxBlocks?: number;
}

/**
 * Live-streaming block feed for `/explorer`.
 *
 * - Seeds the list with `useBlockscoutInfiniteQuery('general:blocks')` first
 *   page so historical context is populated before WSS produces its first
 *   tick. Paxscan stays in charge of REST / detail-panel data.
 * - Subscribes to new blocks over the standard EVM JSON-RPC (viem
 *   `watchBlocks` against api.hyperpax.xyz) — skips the indexer hop.
 * - Caps retained blocks at `maxBlocks` (default 100) — older blocks evict.
 * - Marks freshly-streamed blocks as `isNew` so `BlockRow` flashes briefly.
 * - Shows a connectivity indicator driven by the EVM watch state.
 */
export function LiveBlockStream({
  onSelectBlock,
  selectedBlockHash,
  maxBlocks = 100,
}: LiveBlockStreamProps) {
  // ── Seed from Paxscan REST (fires once on mount; subsequent updates come via WSS) ──
  const {
    data: seedData,
    isLoading: seedLoading,
    error: seedError,
  } = useBlockscoutInfiniteQuery("general:blocks");

  // ── In-memory block list (capped) ────────────────────────────────────
  const [blocks, setBlocks] = useState<Array<Block>>([]);
  const seededRef = useRef(false);
  const newHashesRef = useRef<Set<string>>(new Set());

  // Seed once. We intentionally avoid re-seeding if the REST query refetches
  // later — WSS is the source of truth for incremental updates.
  useEffect(() => {
    if (seededRef.current) return;
    const firstPage = seedData?.pages?.[0];
    if (!firstPage) return;
    const items = firstPage.items;
    if (!items || items.length === 0) return;
    setBlocks(items.slice(0, maxBlocks));
    seededRef.current = true;
  }, [seedData, maxBlocks]);

  // ── EVM new-block subscription (viem watchBlocks) ───────────────────
  //
  // Dedup by `height` in addition to `hash`: both the REST seed and the EVM
  // watcher emit standard 0x… hashes, but keeping the height guard is cheap
  // insurance against any casing/format drift across the two sources.
  const handleIncomingBlock = useCallback(
    (incoming: Block) => {
      if (!incoming || typeof incoming.hash !== "string") return;
      newHashesRef.current.add(incoming.hash);
      setBlocks((prev) => {
        if (prev.some((b) => b.hash === incoming.hash || b.height === incoming.height)) {
          return prev;
        }
        const next = [incoming, ...prev];
        if (next.length > maxBlocks) next.length = maxBlocks;
        return next;
      });
    },
    [maxBlocks],
  );

  const { isConnected: wsConnected } = useEvmBlockStream({
    onBlock: handleIncomingBlock,
  });

  // After `isNew` consumes a flash, BlockRow auto-clears its own state in
  // 1.5s. We also prune the ref after ~2s to keep its cardinality bounded.
  useEffect(() => {
    if (newHashesRef.current.size === 0) return;
    const id = setTimeout(() => {
      newHashesRef.current.clear();
    }, 2_000);
    return () => clearTimeout(id);
  }, [blocks]);

  const isConnected = wsConnected;
  const showSkeleton = blocks.length === 0 && seedLoading;
  const showEmpty = blocks.length === 0 && !seedLoading;

  return (
    <div className="flex flex-col min-h-0 h-full">
      {/* Header / live indicator */}
      <div className="flex items-center justify-between px-3 py-2 border-b border-surface-1">
        <div className="flex items-center gap-2">
          <span className="text-xs font-semibold uppercase tracking-wider text-foreground">
            Live Blocks
          </span>
          <span className="text-[10px] text-muted-foreground">
            {blocks.length}/{maxBlocks}
          </span>
        </div>
        <div className="flex items-center gap-1.5 text-[10px]">
          {isConnected ? (
            <>
              <Radio className="h-3 w-3 text-emerald-400 animate-pulse" />
              <span className="text-emerald-400 uppercase tracking-wider font-semibold">
                Live
              </span>
            </>
          ) : (
            <>
              <WifiOff className="h-3 w-3 text-amber-400" />
              <span className="text-amber-400 uppercase tracking-wider font-semibold">
                Connecting…
              </span>
            </>
          )}
        </div>
      </div>

      {/* Stream body */}
      <div className="flex-1 min-h-0 overflow-y-auto scrollbar-custom p-2 space-y-1.5">
        {showSkeleton && (
          <div className="space-y-1.5">
            {Array.from({ length: 8 }).map((_, i) => (
              <div
                key={i}
                className="h-[52px] rounded-lg bg-surface-1 animate-pulse"
                // Inline opacity avoids dynamic Tailwind classes (Tailwind only
                // bundles literal class strings). Fades older skeletons.
                style={{ opacity: Math.max(0.3, 1 - i * 0.1) }}
              />
            ))}
          </div>
        )}

        {showEmpty && (
          <div className="flex flex-col items-center justify-center py-12 gap-2 text-muted-foreground">
            <WifiOff className="h-5 w-5" />
            <span className="text-xs">No blocks yet — waiting for stream…</span>
            {seedError && (
              <span className="text-[10px] text-red-400">
                REST seed failed: {seedError.message}
              </span>
            )}
          </div>
        )}

        {!showSkeleton &&
          !showEmpty &&
          blocks.map((block) => (
            <BlockRow
              key={block.hash}
              block={block}
              isNew={newHashesRef.current.has(block.hash)}
              isSelected={selectedBlockHash === block.hash}
              onSelect={onSelectBlock}
            />
          ))}
      </div>
    </div>
  );
}
