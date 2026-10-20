import { useState, type ReactNode } from "react";
import { Box, ExternalLink } from "lucide-react";
import { cn } from "@/ui/utils/cn";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/ui/atoms/sheet";

import { useBlockscoutQuery } from "@/hooks/paxscan/useBlockscoutQuery";
import type { Block } from "@/types/paxscan/api/block";
import { EXPLORER_URL } from "@/core/perps/config";
import { fmtDate } from "@/ui/utils/format";
import { fmtGas, fmtGasPct, toRelativeTime, truncateHash } from "./explorer-utils";

// NOTE: BatchFillsTable / OrobWritesTable / KeeperExecutionsCard /
// PoFQScoresCard were deprecated alongside the spot exchange surface.
// The Batch Context tab below has been removed; only the Standard tab
// remains active.

interface BlockDetailPanelProps {
  /**
   * Block identifier — height (string numeric) or hash. Matches Paxscan's
   * `/blocks/{height_or_hash}` path param convention. `null` closes the panel.
   */
  identifier: string | null;
  /**
   * Optional seed block from the live stream — shown immediately while the
   * REST query refetches canonical data. Lets the panel feel instant when
   * opened from a live row.
   */
  seedBlock?: Block | null;
  /** Close callback (bound to Sheet's onOpenChange). */
  onClose: () => void;
}

type Tab = "standard" | "batch";

/**
 * Slide-over panel showing standard + Paxeer-specific data for one block.
 *
 * - Standard tab: canonical Blockscout fields via `useBlockscoutQuery('general:block')`.
 *   (The previous "Batch Context" tab was deprecated with the spot
 *   exchange surface — its sub-components have been deleted.)
 *
 * Uses `ui/sheet.tsx` (Radix dialog slide-over). The `sm:max-w-xl` override
 * widens the default `sm:max-w-sm` to fit data tables.
 */
export function BlockDetailPanel({ identifier, seedBlock, onClose }: BlockDetailPanelProps) {
  const [tab, setTab] = useState<Tab>("standard");

  const { data: fetched, isLoading, error } = useBlockscoutQuery("general:block", {
    pathParams: { height_or_hash: identifier ?? "" },
    queryOptions: { enabled: !!identifier },
  });

  // Prefer fresh REST data; fall back to the seed block (from live stream) so
  // the panel renders immediately without a spinner when opened from a row.
  const block: Block | null = fetched ?? seedBlock ?? null;
  const blockNumber = block?.height ?? null;

  return (
    <Sheet open={identifier !== null} onOpenChange={(open) => !open && onClose()}>
      <SheetContent
        side="right"
        className="sm:max-w-xl w-full overflow-y-auto scrollbar-custom p-0"
      >
        {/* Header */}
        <div className="sticky top-0 z-10 bg-surface-2 border-b border-surface-1 px-5 py-4">
          <SheetHeader>
            <SheetTitle className="flex items-center gap-2">
              <Box className="h-4 w-4 text-emerald-400" />
              Block {block ? `#${block.height}` : identifier}
            </SheetTitle>
            <SheetDescription className="font-mono text-[10px] flex items-center gap-1.5">
              {block ? (
                <>
                  <span title={block.hash}>{truncateHash(block.hash, 10, 6)}</span>
                  <span className="text-muted-foreground/60">·</span>
                  <span>{toRelativeTime(block.timestamp)}</span>
                </>
              ) : isLoading ? (
                <span>Loading…</span>
              ) : error ? (
                <span className="text-red-400">Error: {error.message}</span>
              ) : (
                <span>—</span>
              )}
            </SheetDescription>
          </SheetHeader>

          {/* Tabs */}
          <div className="mt-3 flex items-center gap-1 text-[11px]">
            <TabButton active={tab === "standard"} onClick={() => setTab("standard")}>
              Standard
            </TabButton>
          </div>
        </div>

        {/* Body */}
        <div className="px-5 py-4 space-y-4">
          {tab === "standard" && <StandardTab block={block} isLoading={isLoading} />}
        </div>
      </SheetContent>
    </Sheet>
  );
}

// ─────────────────────────────────────────────────────────────────────
//  Local helpers (not exported — keep the public surface tight)
// ─────────────────────────────────────────────────────────────────────

function TabButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "px-3 py-1.5 rounded-md uppercase tracking-wider font-semibold transition-colors",
        active
          ? "bg-surface-1 text-foreground"
          : "text-muted-foreground hover:text-foreground hover:bg-surface-1/60",
      )}
    >
      {children}
    </button>
  );
}

function StandardTab({ block, isLoading }: { block: Block | null; isLoading: boolean }) {
  if (!block && isLoading) {
    return (
      <div className="space-y-2">
        {Array.from({ length: 6 }).map((_, i) => (
          <div
            key={i}
            className="h-5 rounded bg-surface-1 animate-pulse"
            style={{ opacity: Math.max(0.3, 1 - i * 0.12) }}
          />
        ))}
      </div>
    );
  }
  if (!block) {
    return (
      <div className="text-xs text-muted-foreground italic text-center py-6">
        Block not found.
      </div>
    );
  }
  return (
    <div className="space-y-2">
      <Field label="Height" value={`#${block.height}`} mono />
      <Field label="Hash" value={block.hash} mono truncate />
      <Field label="Parent" value={block.parent_hash} mono truncate />
      <Field label="Timestamp" value={fmtDate(block.timestamp)} />
      <Field label="Transactions" value={String(block.transactions_count)} mono />
      <Field label="Internal Txs" value={String(block.internal_transactions_count)} mono />
      <Field
        label="Gas Used"
        value={`${fmtGas(block.gas_used)} / ${fmtGas(block.gas_limit)} (${fmtGasPct(block.gas_used, block.gas_limit)})`}
        mono
      />
      {block.base_fee_per_gas && (
        <Field label="Base Fee" value={fmtGas(block.base_fee_per_gas)} mono />
      )}
      {block.priority_fee && (
        <Field label="Priority Fee" value={fmtGas(block.priority_fee)} mono />
      )}
      {block.burnt_fees && (
        <Field label="Burnt Fees" value={fmtGas(block.burnt_fees)} mono />
      )}
      <Field
        label="Miner"
        value={block.miner.hash}
        mono
        truncate
        href={`${EXPLORER_URL}/address/${block.miner.hash}`}
      />
      <Field label="Type" value={block.type} />
    </div>
  );
}


function Field({
  label,
  value,
  mono = false,
  truncate = false,
  href,
}: {
  label: string;
  value: string;
  mono?: boolean;
  truncate?: boolean;
  href?: string;
}) {
  return (
    <div className="flex items-start justify-between gap-3 text-xs">
      <span className="text-muted-foreground uppercase tracking-wider text-[10px] shrink-0 w-24 pt-0.5">
        {label}
      </span>
      <div className="flex items-center gap-1.5 min-w-0 flex-1 justify-end">
        <span
          className={cn(
            mono && "font-mono",
            truncate && "truncate",
            "text-foreground text-right",
          )}
          title={truncate ? value : undefined}
        >
          {value}
        </span>
        {href && (
          <a
            href={href}
            target="_blank"
            rel="noopener noreferrer"
            className="text-muted-foreground hover:text-foreground shrink-0"
            // @stub awaiting: FROZEN:PaxscanLink from agent_1 (round 2)
          >
            <ExternalLink className="h-3 w-3" />
          </a>
        )}
      </div>
    </div>
  );
}
