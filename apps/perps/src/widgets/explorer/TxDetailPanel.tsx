import { useState, type ReactNode } from "react";
import { Check, ExternalLink, X, Zap } from "lucide-react";
import { cn } from "@/ui/utils/cn";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/ui/atoms/sheet";

import { useBlockscoutQuery } from "@/hooks/paxscan/useBlockscoutQuery";
import type { Transaction } from "@/types/paxscan/api/transaction";
import { EXPLORER_URL } from "@/core/perps/config";
import { fmtDate } from "@/ui/utils/format";
import { fmtGas, fmtGasPct, toRelativeTime, truncateHash } from "./explorer-utils";

// NOTE: BatchFillsTable / OrobWritesTable / KeeperExecutionsCard /
// PoFQScoresCard were deprecated alongside the spot exchange surface.
// The Batch Context tab below has been removed; only the Standard tab
// remains active.

interface TxDetailPanelProps {
  /** Tx hash — Paxscan `/transactions/{hash}` path param. `null` closes. */
  txHash: string | null;
  /** Close callback (Sheet onOpenChange). */
  onClose: () => void;
}

type Tab = "standard" | "batch";

/**
 * Slide-over panel showing standard + Paxeer-specific data for one tx.
 *
 * - Standard tab: canonical fields via `useBlockscoutQuery('general:transaction')`.
 * - Batch context tab: same four batch-context components as the block panel,
 *   filtered by the tx's `block_number`. Because every Paxeer block is
 *   effectively one batch-settlement tx, a user arriving via tx hash sees
 *   the SAME uniform clearing price / other fills / keeper / PoFQ context
 *   they'd see via the block — which is the Paxeer-unique payoff.
 *
 * If tx `block_number` is null (pending tx) the batch tab degrades to a
 * friendly empty state.
 */
export function TxDetailPanel({ txHash, onClose }: TxDetailPanelProps) {
  const [tab, setTab] = useState<Tab>("standard");

  const { data: tx, isLoading, error } = useBlockscoutQuery("general:transaction", {
    pathParams: { hash: txHash ?? "" },
    queryOptions: { enabled: !!txHash },
  });

  const blockNumber = tx?.block_number ?? null;

  return (
    <Sheet open={txHash !== null} onOpenChange={(open) => !open && onClose()}>
      <SheetContent
        side="right"
        className="sm:max-w-xl w-full overflow-y-auto scrollbar-custom p-0"
      >
        {/* Header */}
        <div className="sticky top-0 z-10 bg-surface-2 border-b border-surface-1 px-5 py-4">
          <SheetHeader>
            <SheetTitle className="flex items-center gap-2">
              <Zap className="h-4 w-4 text-emerald-400" />
              Layer X Transaction
            </SheetTitle>
            <SheetDescription className="font-mono text-[10px] flex items-center gap-1.5">
              {tx ? (
                <>
                  <span title={tx.hash}>{truncateHash(tx.hash, 10, 6)}</span>
                  {tx.timestamp && (
                    <>
                      <span className="text-muted-foreground/60">·</span>
                      <span>{toRelativeTime(tx.timestamp)}</span>
                    </>
                  )}
                  {tx.status && (
                    <>
                      <span className="text-muted-foreground/60">·</span>
                      <StatusBadge status={tx.status} />
                    </>
                  )}
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
          {tab === "standard" && <StandardTab tx={tx ?? null} isLoading={isLoading} />}
        </div>
      </SheetContent>
    </Sheet>
  );
}

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

function StatusBadge({ status }: { status: NonNullable<Transaction["status"]> }) {
  if (status === "ok") {
    return (
      <span className="inline-flex items-center gap-0.5 text-emerald-400">
        <Check className="h-2.5 w-2.5" />
        <span className="uppercase tracking-wider font-semibold">Ok</span>
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-0.5 text-red-400">
      <X className="h-2.5 w-2.5" />
      <span className="uppercase tracking-wider font-semibold">Err</span>
    </span>
  );
}

function StandardTab({ tx, isLoading }: { tx: Transaction | null; isLoading: boolean }) {
  if (!tx && isLoading) {
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
  if (!tx) {
    return (
      <div className="text-xs text-muted-foreground italic text-center py-6">
        Transaction not found.
      </div>
    );
  }
  return (
    <div className="space-y-2">
      <Field label="Hash" value={tx.hash} mono truncate />
      <Field
        label="Block"
        value={tx.block_number !== null ? `#${tx.block_number}` : "pending"}
        mono
      />
      <Field label="Timestamp" value={fmtDate(tx.timestamp)} />
      <Field
        label="From"
        value={tx.from.hash}
        mono
        truncate
        href={`${EXPLORER_URL}/address/${tx.from.hash}`}
      />
      <Field
        label="To"
        value={tx.to?.hash ?? (tx.created_contract?.hash ?? "—")}
        mono
        truncate
        href={
          tx.to
            ? `${EXPLORER_URL}/address/${tx.to.hash}`
            : tx.created_contract
              ? `${EXPLORER_URL}/address/${tx.created_contract.hash}`
              : undefined
        }
      />
      {tx.method && <Field label="Method" value={tx.method} mono />}
      <Field label="Value" value={tx.value} mono />
      <Field
        label="Fee"
        value={tx.fee.value ? fmtGas(tx.fee.value) : "—"}
        mono
      />
      <Field
        label="Gas"
        value={`${fmtGas(tx.gas_used)} / ${fmtGas(tx.gas_limit)} (${fmtGasPct(tx.gas_used, tx.gas_limit)})`}
        mono
      />
      {tx.gas_price && <Field label="Gas Price" value={fmtGas(tx.gas_price)} mono />}
      <Field label="Nonce" value={String(tx.nonce)} mono />
      {tx.position !== null && <Field label="Position" value={String(tx.position)} mono />}
      <Field label="Result" value={tx.result} />
      <Field label="Confirmations" value={String(tx.confirmations)} mono />
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
