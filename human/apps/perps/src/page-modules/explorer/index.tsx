import React from "react";
import { Radio, WifiOff, Box, Zap } from "lucide-react";
import { cn } from "@/ui/utils/cn";
import { useExplorerStream, type ExplorerBlock, type ExplorerTx } from "@/hooks/chain/useExplorerStream";
import { truncateHash, toRelativeTime } from "@/widgets/explorer/explorer-utils";

// ── Helpers ──────────────────────────────────────────────────────────

/** Action → accent colour for the badge in the TX table. */
const ACTION_COLOR: Record<string, string> = {
  "Place Order":          "bg-emerald-500/10 text-emerald-400",
  "Cancel Order":         "bg-red-500/10 text-red-400",
  "Modify Order":         "bg-amber-500/10 text-amber-400",
  "OROB Fill":            "bg-blue-500/10 text-blue-400",
  "Oracle Updated":       "bg-violet-500/10 text-violet-400",
  "Sealed Batch Executed":"bg-cyan-500/10 text-cyan-400",
};

function actionClass(action: string): string {
  return ACTION_COLOR[action] ?? "bg-surface-1 text-muted-foreground";
}

// ── Sub-components ───────────────────────────────────────────────────

function LiveIndicator({ connected }: { connected: boolean }) {
  return (
    <div className="flex items-center gap-1.5 text-[10px]">
      {connected ? (
        <>
          <Radio className="h-3 w-3 text-emerald-400 animate-pulse" />
          <span className="text-emerald-400 uppercase tracking-wider font-semibold font-mono">Live</span>
        </>
      ) : (
        <>
          <WifiOff className="h-3 w-3 text-amber-400" />
          <span className="text-amber-400 uppercase tracking-wider font-semibold font-mono">Connecting…</span>
        </>
      )}
    </div>
  );
}

function TableHeader({ children }: { children: React.ReactNode }) {
  return (
    <div className="px-4 py-2.5 border-b border-border bg-surface-1/60 shrink-0">
      <span className="text-xs font-semibold uppercase tracking-wider text-foreground">{children}</span>
    </div>
  );
}

function ColHeader({ children, align = "left" }: { children: React.ReactNode; align?: "left" | "right" }) {
  return (
    <span
      className={cn(
        "text-[10px] uppercase tracking-wider font-mono text-muted-foreground",
        align === "right" && "text-right",
      )}
    >
      {children}
    </span>
  );
}

// ── Skeleton row ─────────────────────────────────────────────────────

function SkeletonRows({ count, cols }: { count: number; cols: string }) {
  return (
    <>
      {Array.from({ length: count }).map((_, i) => (
        <div
          key={i}
          className={cn("grid gap-x-3 px-4 py-2 border-b border-border/40", cols)}
          style={{ opacity: Math.max(0.25, 1 - i * 0.07) }}
        >
          {Array.from({ length: cols.split("_").length - 1 }).map((__, j) => (
            <div key={j} className="h-3.5 rounded bg-surface-1 animate-pulse my-0.5" />
          ))}
        </div>
      ))}
    </>
  );
}

// ── Latest Blocks panel ──────────────────────────────────────────────

function BlocksPanel({ blocks, seedLoading }: { blocks: ExplorerBlock[]; seedLoading: boolean }) {
  const showSkeleton = seedLoading && blocks.length === 0;
  const showEmpty    = !seedLoading && blocks.length === 0;

  return (
    <div className="flex flex-col min-h-0 flex-1 border-r border-border overflow-hidden">
      <TableHeader>Latest Blocks</TableHeader>

      {/* Column headers */}
      <div className="grid grid-cols-[72px_1fr_52px_1fr_1fr] gap-x-3 px-4 py-2 border-b border-border/60 shrink-0">
        <ColHeader>Block</ColHeader>
        <ColHeader>Time</ColHeader>
        <ColHeader align="right">Txns</ColHeader>
        <ColHeader>Proposer</ColHeader>
        <ColHeader>Hash</ColHeader>
      </div>

      {/* Rows */}
      <div className="flex-1 min-h-0 overflow-y-auto scrollbar-custom">
        {showSkeleton && (
          <SkeletonRows count={14} cols="grid-cols-[72px_1fr_52px_1fr_1fr]" />
        )}

        {showEmpty && (
          <div className="flex flex-col items-center justify-center h-full gap-2 text-muted-foreground py-12">
            <Box className="h-5 w-5 opacity-40" />
            <span className="text-xs font-mono">Waiting for blocks…</span>
          </div>
        )}

        {blocks.map((b) => (
          <div
            key={b.hash || b.block_number}
            className="grid grid-cols-[72px_1fr_52px_1fr_1fr] gap-x-3 px-4 py-2 border-b border-border/40 hover:bg-surface-1/40 transition-colors"
          >
            <span className="text-xs font-mono font-semibold text-foreground tabular-nums">
              #{b.block_number}
            </span>
            <span className="text-xs font-mono text-muted-foreground truncate tabular-nums">
              {toRelativeTime(b.time)}
            </span>
            <span className="text-xs font-mono text-foreground text-right tabular-nums">
              {b.transactions}
            </span>
            <span className="text-xs font-mono text-muted-foreground truncate" title={b.proposer}>
              {b.proposer ? truncateHash(b.proposer, 4, 4) : "—"}
            </span>
            <span className="text-xs font-mono text-blue-400 truncate" title={b.hash}>
              {b.hash ? truncateHash(b.hash, 6, 4) : "—"}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

// ── Latest Transactions panel ─────────────────────────────────────────

function TxsPanel({ txs, seedLoading }: { txs: ExplorerTx[]; seedLoading: boolean }) {
  const showSkeleton = seedLoading && txs.length === 0;
  const showEmpty    = !seedLoading && txs.length === 0;

  return (
    <div className="flex flex-col min-h-0 flex-1 overflow-hidden">
      <TableHeader>Latest Transactions</TableHeader>

      {/* Column headers */}
      <div className="grid grid-cols-[1fr_140px_64px_80px_1fr] gap-x-3 px-4 py-2 border-b border-border/60 shrink-0">
        <ColHeader>Hash</ColHeader>
        <ColHeader>Action</ColHeader>
        <ColHeader>Block</ColHeader>
        <ColHeader>Time</ColHeader>
        <ColHeader>User</ColHeader>
      </div>

      {/* Rows */}
      <div className="flex-1 min-h-0 overflow-y-auto scrollbar-custom">
        {showSkeleton && (
          <SkeletonRows count={18} cols="grid-cols-[1fr_140px_64px_80px_1fr]" />
        )}

        {showEmpty && (
          <div className="flex flex-col items-center justify-center h-full gap-2 text-muted-foreground py-12">
            <Zap className="h-5 w-5 opacity-40" />
            <span className="text-xs font-mono">Waiting for transactions…</span>
          </div>
        )}

        {txs.map((tx) => (
          <div
            key={tx.hash}
            className="grid grid-cols-[1fr_140px_64px_80px_1fr] gap-x-3 px-4 py-2 border-b border-border/40 hover:bg-surface-1/40 transition-colors items-center"
          >
            <span className="text-xs font-mono text-blue-400 truncate" title={tx.hash}>
              {truncateHash(tx.hash, 6, 4)}
            </span>
            <span
              className={cn(
                "text-[10px] font-mono font-medium px-2 py-0.5 rounded-full w-fit truncate max-w-full",
                actionClass(tx.action),
              )}
              title={tx.action}
            >
              {tx.action}
            </span>
            <span className="text-xs font-mono text-muted-foreground tabular-nums">
              #{tx.block}
            </span>
            <span className="text-xs font-mono text-muted-foreground truncate tabular-nums">
              {toRelativeTime(tx.time)}
            </span>
            <span className="text-xs font-mono text-muted-foreground truncate" title={tx.user}>
              {tx.user ? truncateHash(tx.user, 4, 4) : "—"}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

// ── Page ─────────────────────────────────────────────────────────────

/**
 * `/explorer` — Hyperpax-os live chain explorer.
 *
 * Desktop: two side-by-side scrollable panels (Latest Blocks | Latest Txns).
 * Mobile:  stacked panels, each with a capped scrollable height.
 *
 * Data source: wss://data.feed.chainflowtrading.com/explorer
 *   Topics: chain@blocks, chain@transactions
 */
export default function ExplorerPage() {
  const { blocks, txs, connected, seedLoading } = useExplorerStream();

  return (
    <div className="flex flex-col h-full min-h-0 bg-background">
      {/* Page header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-border shrink-0">
        <div>
          <h1 className="text-base font-semibold text-foreground">Hyperpax-os Explorer</h1>
          <p className="text-[11px] text-muted-foreground font-mono">
            Live chain activity · {blocks.length} blocks · {txs.length} txns buffered
          </p>
        </div>
        <LiveIndicator connected={connected} />
      </div>

      {/* Two-column layout — side-by-side on lg+, stacked on mobile */}
      <div className="flex flex-1 min-h-0 flex-col lg:flex-row overflow-hidden">
        {/* Latest Blocks */}
        <div className="flex-1 min-h-0 lg:max-h-full max-h-[50%] overflow-hidden">
          <BlocksPanel blocks={blocks} seedLoading={seedLoading} />
        </div>

        {/* Divider (horizontal on mobile, vertical on desktop) */}
        <div className="lg:hidden h-px w-full bg-border shrink-0" />
        <div className="hidden lg:block w-px bg-border shrink-0" />

        {/* Latest Transactions */}
        <div className="flex-1 min-h-0 lg:max-h-full max-h-[50%] overflow-hidden">
          <TxsPanel txs={txs} seedLoading={seedLoading} />
        </div>
      </div>
    </div>
  );
}
