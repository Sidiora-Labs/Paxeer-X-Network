import { useEffect, useState } from "react";
import { ArrowRight, Box, Zap } from "lucide-react";
import { cn } from "@/ui/utils/cn";
import type { Block } from "@/types/paxscan/api/block";
import { fmtGas, fmtGasPct, toRelativeTime, truncateHash } from "./explorer-utils";

interface BlockRowProps {
  block: Block;
  /**
   * When true, the row plays a subtle flash animation on mount — used by
   * `LiveBlockStream` to highlight blocks that just streamed in via WSS.
   */
  isNew?: boolean;
  /** Click handler — opens the detail panel keyed to this block's height. */
  onSelect: (block: Block) => void;
  /** Optional visual "selected" state (e.g. when the detail panel is open on this row). */
  isSelected?: boolean;
}

/**
 * Single block rendered as a transaction-like row. In Paxeer's batch-auction
 * model each block is effectively one settlement TX — this row treats blocks
 * as first-class TX-like entities, mirroring how etherscan renders a txn row.
 *
 * Visual language copied from `features/spot/*`:
 *   - `bg-surface-1` / `bg-surface-2` hover swap
 *   - `uppercase tracking-wider text-muted-foreground` secondary labels
 *   - `font-mono` numeric / hex fields
 *   - `text-[10px]` inner labels, `text-xs` primary text
 * No new design language introduced (Hard Rule 4).
 */
export function BlockRow({ block, isNew = false, onSelect, isSelected = false }: BlockRowProps) {
  // NOTE: we intentionally do NOT run a per-row 1s ticker to refresh
  // "Xs ago". With ~100 rows visible, that would be 100 re-renders/sec. The
  // timestamp is accurate at insertion time and naturally updates whenever
  // the list reshuffles (new block every ~2-12s) — good enough for UX.
  //
  // Flash on mount if this row is freshly streamed. Self-clears after 1.5s.
  const [flash, setFlash] = useState(isNew);
  useEffect(() => {
    if (!isNew) return;
    const id = setTimeout(() => setFlash(false), 1_500);
    return () => clearTimeout(id);
  }, [isNew]);

  return (
    <button
      type="button"
      onClick={() => onSelect(block)}
      className={cn(
        "w-full text-left rounded-lg bg-surface-1 hover:bg-surface-2 transition-colors",
        "px-3 py-2.5 flex items-center gap-3 group",
        isSelected && "ring-1 ring-emerald-400/40 bg-surface-2",
        flash && "animate-pulse",
      )}
      aria-label={`Open block #${block.height}`}
    >
      {/* Left: block icon + height */}
      <div className="flex items-center gap-2 min-w-0 shrink-0">
        <div className="flex items-center justify-center h-7 w-7 rounded-md bg-surface-2 group-hover:bg-surface-1 transition-colors">
          <Box className="h-3.5 w-3.5 text-emerald-400" />
        </div>
        <div className="flex flex-col min-w-0">
          <span className="text-xs font-semibold font-mono text-foreground">
            #{block.height}
          </span>
          <span
            className="text-[10px] font-mono text-muted-foreground truncate max-w-[16ch]"
            title={block.hash}
          >
            {truncateHash(block.hash, 6, 4)}
          </span>
        </div>
      </div>

      {/* Middle: tx count + gas */}
      <div className="flex items-center gap-4 flex-1 min-w-0">
        <div className="flex flex-col">
          <span className="text-[10px] uppercase tracking-wider text-muted-foreground">
            Fills
          </span>
          <span className="text-xs font-mono text-foreground">
            {block.transactions_count}
          </span>
        </div>
        <div className="flex flex-col">
          <span className="text-[10px] uppercase tracking-wider text-muted-foreground">
            Gas
          </span>
          <span className="text-xs font-mono text-foreground" title={block.gas_used ?? "—"}>
            {fmtGas(block.gas_used)}{" "}
            <span className="text-muted-foreground">
              ({fmtGasPct(block.gas_used, block.gas_limit)})
            </span>
          </span>
        </div>
        {block.priority_fee && (
          <div className="hidden sm:flex sm:flex-col">
            <span className="text-[10px] uppercase tracking-wider text-muted-foreground flex items-center gap-1">
              <Zap className="h-2.5 w-2.5" />
              Priority
            </span>
            <span className="text-xs font-mono text-foreground">
              {fmtGas(block.priority_fee)}
            </span>
          </div>
        )}
      </div>

      {/* Right: relative time + chevron */}
      <div className="flex items-center gap-2 shrink-0">
        <span className="text-[10px] text-muted-foreground tabular-nums">
          {toRelativeTime(block.timestamp)}
        </span>
        <ArrowRight className="h-3.5 w-3.5 text-muted-foreground/60 group-hover:text-foreground transition-colors" />
      </div>
    </button>
  );
}
