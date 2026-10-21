"use client";

import { useMemo, useState } from "react";
import { ArrowDownLeft, ArrowUpRight, ExternalLink } from "lucide-react";

import {
  paxscanAddressUrl,
  paxscanTokenUrl,
  paxscanTxUrl,
  useBlockscoutInfiniteQuery,
} from "@/lib/paxscan";
import type {
  TokenTransfer,
  Transaction,
} from "@/lib/paxscan";
import { fmtDate, shortAddr } from "@/ui/utils/format";
import { cn } from "@/ui/utils/cn";

/**
 * Embeds a paginated on-chain history panel (token transfers + native txs)
 * against the Paxscan Blockscout backend. Consumed by:
 *   - SpotPortfolio.tsx          → variant="desktop" (table chrome, matches `features/spot/*`)
 *   - MobilePortfolioOverview.tsx → variant="mobile"  (card chrome, matches activity area)
 *
 * Backed by FROZEN Round 1 exports:
 *   - `useBlockscoutInfiniteQuery` (`src/lib/paxscan/hooks`)
 *   - `general:address_token_transfers` / `general:address_txs` resources
 *   - `paxscanTxUrl` / `paxscanAddressUrl` / `paxscanTokenUrl` deep-link helpers
 */
export type OnChainHistorySubTab = "transfers" | "transactions";
export type OnChainHistoryVariant = "desktop" | "mobile";

export interface OnChainHistoryPanelProps {
  /** 0x-prefixed address whose history is being viewed. */
  address: string;
  /** Presentation chrome. Defaults to `"desktop"`. */
  variant?: OnChainHistoryVariant;
  /** Optional extra classes on the root wrapper. */
  className?: string;
  /** Initial sub-tab. Defaults to `"transfers"`. */
  initialSubTab?: OnChainHistorySubTab;
}

const TABS: Array<{ key: OnChainHistorySubTab; label: string }> = [
  { key: "transfers", label: "Transfers" },
  { key: "transactions", label: "Transactions" },
];

export function OnChainHistoryPanel({
  address,
  variant = "desktop",
  className,
  initialSubTab = "transfers",
}: OnChainHistoryPanelProps) {
  const [subTab, setSubTab] = useState<OnChainHistorySubTab>(initialSubTab);
  const hash = (address ?? "").toLowerCase();

  if (!hash) return null;

  if (variant === "mobile") {
    return (
      <div
        className={cn(
          "rounded-2xl border border-[var(--brand-border)] bg-[var(--brand-surface)] overflow-hidden",
          className,
        )}
      >
        <div className="border-b border-[var(--brand-border)] px-4 py-3 flex items-center justify-between gap-3">
          <div className="text-xs font-semibold text-[#DDDDDD] uppercase tracking-wider">
            On-chain
          </div>
          <div className="flex items-center gap-1">
            {TABS.map((t) => (
              <button
                key={t.key}
                type="button"
                onClick={() => setSubTab(t.key)}
                className={cn(
                  "px-2.5 py-1 text-[10px] font-semibold rounded-full transition-colors uppercase tracking-wider",
                  subTab === t.key
                    ? "bg-[var(--brand-border)] text-white"
                    : "text-[#828282]",
                )}
              >
                {t.label}
              </button>
            ))}
          </div>
        </div>
        {subTab === "transfers" ? (
          <MobileTransfers address={hash} />
        ) : (
          <MobileTransactions address={hash} />
        )}
      </div>
    );
  }

  return (
    <div className={cn("space-y-3", className)}>
      <div className="flex items-center gap-1 border-b border-white/[0.06] pb-2">
        {TABS.map((t) => (
          <button
            key={t.key}
            type="button"
            onClick={() => setSubTab(t.key)}
            className={cn(
              "px-3 py-1 text-xs font-medium rounded-[var(--radius-md)] transition-colors whitespace-nowrap",
              subTab === t.key
                ? "bg-surface-2 text-foreground"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            {t.label}
          </button>
        ))}
      </div>
      {subTab === "transfers" ? (
        <DesktopTransfers address={hash} />
      ) : (
        <DesktopTransactions address={hash} />
      )}
    </div>
  );
}

// ─────────────────────────────────────────────────────────────────────────────
// Desktop: Transfers
// ─────────────────────────────────────────────────────────────────────────────

function DesktopTransfers({ address }: { address: string }) {
  const q = useBlockscoutInfiniteQuery("general:address_token_transfers", {
    pathParams: { hash: address },
    queryOptions: { staleTime: 15_000 },
  });

  const items = useMemo<Array<TokenTransfer>>(
    () => q.data?.pages.flatMap((p) => p.items) ?? [],
    [q.data],
  );

  if (q.isLoading) return <DesktopSkeletonRows />;
  if (q.isError) return <DesktopErrorBanner message={q.error?.message} />;
  if (items.length === 0) {
    return <DesktopEmpty label="No on-chain transfers yet" />;
  }

  return (
    <div className="rounded-[var(--radius-lg)] bg-surface-1 overflow-hidden">
      <div className="overflow-x-auto">
        <table className="w-full text-xs">
          <thead>
            <tr className="text-muted-foreground border-b border-white/[0.06]">
              <th className="text-left px-4 py-2.5 font-medium">Direction</th>
              <th className="text-left px-4 py-2.5 font-medium">Token</th>
              <th className="text-right px-4 py-2.5 font-medium">Amount</th>
              <th className="text-left px-4 py-2.5 font-medium hidden lg:table-cell">
                Counterparty
              </th>
              <th className="text-right px-4 py-2.5 font-medium hidden md:table-cell">
                Time
              </th>
              <th className="text-right px-4 py-2.5 font-medium">Tx</th>
            </tr>
          </thead>
          <tbody>
            {items.map((t, i) => {
              const fromHash = t.from?.hash?.toLowerCase() ?? "";
              const toHash = t.to?.hash?.toLowerCase() ?? "";
              const isOut = fromHash === address;
              const counterparty = isOut ? t.to?.hash : t.from?.hash;
              const sign = isOut ? "-" : "+";
              const signColor = isOut ? "text-red-400" : "text-emerald-400";
              const key = `${t.transaction_hash ?? "none"}-${t.log_index}-${i}`;
              return (
                <tr
                  key={key}
                  className="border-b border-white/[0.06]/50 hover:bg-muted/20"
                >
                  <td className="px-4 py-2">
                    <span
                      className={cn(
                        "inline-flex items-center gap-1 text-[10px] px-1.5 py-0.5 rounded font-semibold",
                        isOut
                          ? "bg-red-500/10 text-red-400"
                          : "bg-emerald-500/10 text-emerald-400",
                      )}
                    >
                      {isOut ? (
                        <ArrowUpRight className="h-3 w-3" />
                      ) : (
                        <ArrowDownLeft className="h-3 w-3" />
                      )}
                      {isOut ? "Out" : "In"}
                    </span>
                  </td>
                  <td className="px-4 py-2 font-medium">
                    {t.token ? (
                      <a
                        href={paxscanTokenUrl(t.token.address_hash ?? "")}
                        target="_blank"
                        rel="noopener"
                        className="hover:text-foreground transition-colors"
                      >
                        {t.token.symbol ?? shortAddrSafe(t.token.address_hash)}
                      </a>
                    ) : (
                      <span className="text-muted-foreground">—</span>
                    )}
                  </td>
                  <td className="px-4 py-2 text-right font-mono">
                    <span className={cn("font-semibold mr-0.5", signColor)}>
                      {sign}
                    </span>
                    {formatTokenAmount(t)}
                  </td>
                  <td className="px-4 py-2 hidden lg:table-cell">
                    {counterparty ? (
                      <a
                        href={paxscanAddressUrl(counterparty)}
                        target="_blank"
                        rel="noopener"
                        className="font-mono text-muted-foreground hover:text-foreground"
                      >
                        {shortAddrSafe(counterparty)}
                      </a>
                    ) : (
                      <span className="text-muted-foreground">—</span>
                    )}
                  </td>
                  <td className="px-4 py-2 text-right text-muted-foreground hidden md:table-cell">
                    {fmtDate(t.timestamp)}
                  </td>
                  <td className="px-4 py-2 text-right">
                    {t.transaction_hash ? (
                      <a
                        href={paxscanTxUrl(t.transaction_hash)}
                        target="_blank"
                        rel="noopener"
                        className="text-muted-foreground hover:text-foreground inline-block"
                      >
                        <ExternalLink className="h-3 w-3" />
                      </a>
                    ) : (
                      <span className="text-muted-foreground">—</span>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <DesktopLoadMore
        onClick={() => q.fetchNextPage()}
        isLoading={q.isFetchingNextPage}
        hasMore={!!q.hasNextPage}
      />
    </div>
  );
}

// ─────────────────────────────────────────────────────────────────────────────
// Desktop: Transactions
// ─────────────────────────────────────────────────────────────────────────────

function DesktopTransactions({ address }: { address: string }) {
  const q = useBlockscoutInfiniteQuery("general:address_txs", {
    pathParams: { hash: address },
    queryOptions: { staleTime: 15_000 },
  });

  const items = useMemo<Array<Transaction>>(
    () => q.data?.pages.flatMap((p) => p.items) ?? [],
    [q.data],
  );

  if (q.isLoading) return <DesktopSkeletonRows />;
  if (q.isError) return <DesktopErrorBanner message={q.error?.message} />;
  if (items.length === 0) return <DesktopEmpty label="No transactions yet" />;

  return (
    <div className="rounded-[var(--radius-lg)] bg-surface-1 overflow-hidden">
      <div className="overflow-x-auto">
        <table className="w-full text-xs">
          <thead>
            <tr className="text-muted-foreground border-b border-white/[0.06]">
              <th className="text-left px-4 py-2.5 font-medium">Status</th>
              <th className="text-left px-4 py-2.5 font-medium">Method</th>
              <th className="text-right px-4 py-2.5 font-medium hidden md:table-cell">
                Block
              </th>
              <th className="text-left px-4 py-2.5 font-medium hidden lg:table-cell">
                Counterparty
              </th>
              <th className="text-right px-4 py-2.5 font-medium">Value (PAX)</th>
              <th className="text-right px-4 py-2.5 font-medium hidden md:table-cell">
                Time
              </th>
              <th className="text-right px-4 py-2.5 font-medium">Tx</th>
            </tr>
          </thead>
          <tbody>
            {items.map((tx, i) => {
              const fromHash = tx.from?.hash?.toLowerCase() ?? "";
              const isOut = fromHash === address;
              const counterparty = isOut ? tx.to?.hash : tx.from?.hash;
              const key = `${tx.hash}-${i}`;
              return (
                <tr
                  key={key}
                  className="border-b border-white/[0.06]/50 hover:bg-muted/20"
                >
                  <td className="px-4 py-2">
                    <span
                      className={cn(
                        "inline-block text-[10px] px-1.5 py-0.5 rounded font-medium",
                        txStatusColor(tx.status),
                      )}
                    >
                      {tx.status ?? "pending"}
                    </span>
                  </td>
                  <td className="px-4 py-2 text-muted-foreground truncate max-w-[10rem]">
                    {tx.method ?? "—"}
                  </td>
                  <td className="px-4 py-2 text-right font-mono text-muted-foreground hidden md:table-cell">
                    {tx.block_number != null ? `#${tx.block_number}` : "—"}
                  </td>
                  <td className="px-4 py-2 hidden lg:table-cell">
                    {counterparty ? (
                      <a
                        href={paxscanAddressUrl(counterparty)}
                        target="_blank"
                        rel="noopener"
                        className="font-mono text-muted-foreground hover:text-foreground"
                      >
                        <span
                          className={cn(
                            "inline-block text-[9px] px-1 py-0 mr-1 rounded font-semibold uppercase",
                            isOut
                              ? "bg-red-500/10 text-red-400"
                              : "bg-emerald-500/10 text-emerald-400",
                          )}
                        >
                          {isOut ? "→" : "←"}
                        </span>
                        {shortAddrSafe(counterparty)}
                      </a>
                    ) : (
                      <span className="text-muted-foreground">—</span>
                    )}
                  </td>
                  <td className="px-4 py-2 text-right font-mono">
                    {formatPaxValue(tx.value)}
                  </td>
                  <td className="px-4 py-2 text-right text-muted-foreground hidden md:table-cell">
                    {fmtDate(tx.timestamp)}
                  </td>
                  <td className="px-4 py-2 text-right">
                    <a
                      href={paxscanTxUrl(tx.hash)}
                      target="_blank"
                      rel="noopener"
                      className="text-muted-foreground hover:text-foreground inline-block"
                    >
                      <ExternalLink className="h-3 w-3" />
                    </a>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <DesktopLoadMore
        onClick={() => q.fetchNextPage()}
        isLoading={q.isFetchingNextPage}
        hasMore={!!q.hasNextPage}
      />
    </div>
  );
}

// ─────────────────────────────────────────────────────────────────────────────
// Mobile: Transfers
// ─────────────────────────────────────────────────────────────────────────────

function MobileTransfers({ address }: { address: string }) {
  const q = useBlockscoutInfiniteQuery("general:address_token_transfers", {
    pathParams: { hash: address },
    queryOptions: { staleTime: 15_000 },
  });

  const items = useMemo<Array<TokenTransfer>>(
    () => q.data?.pages.flatMap((p) => p.items) ?? [],
    [q.data],
  );

  if (q.isLoading) return <MobileSkeletonRows />;
  if (q.isError) return <MobileErrorBanner message={q.error?.message} />;
  if (items.length === 0) {
    return (
      <div className="px-4 py-8 text-center text-sm text-[#828282]">
        No on-chain transfers yet
      </div>
    );
  }

  return (
    <>
      <div className="divide-y divide-[var(--brand-border)]">
        {items.map((t, i) => {
          const fromHash = t.from?.hash?.toLowerCase() ?? "";
          const isOut = fromHash === address;
          const counterparty = isOut ? t.to?.hash : t.from?.hash;
          const sign = isOut ? "-" : "+";
          const color = isOut ? "text-[var(--brand-red)]" : "text-[var(--brand-green)]";
          const symbol = t.token?.symbol ?? "Token";
          const key = `${t.transaction_hash ?? "none"}-${t.log_index}-${i}`;
          return (
            <div
              key={key}
              className="flex items-center justify-between gap-3 px-4 py-3 text-xs"
            >
              <div className="min-w-0 flex-1">
                <div className={cn("font-semibold", color)}>
                  {isOut ? "Out" : "In"} · {symbol}
                </div>
                <div className="text-[#828282] font-mono truncate">
                  {counterparty ? shortAddrSafe(counterparty) : "—"}
                </div>
              </div>
              <div className="text-right shrink-0">
                <div className={cn("font-mono font-semibold", color)}>
                  {sign}
                  {formatTokenAmount(t)}
                </div>
                <div className="text-[#828282]">{fmtDate(t.timestamp)}</div>
              </div>
              {t.transaction_hash && (
                <a
                  href={paxscanTxUrl(t.transaction_hash)}
                  target="_blank"
                  rel="noopener"
                  className="text-[#828282] shrink-0"
                  aria-label="View on Paxscan"
                >
                  <ExternalLink className="h-3.5 w-3.5" />
                </a>
              )}
            </div>
          );
        })}
      </div>
      <MobileLoadMore
        onClick={() => q.fetchNextPage()}
        isLoading={q.isFetchingNextPage}
        hasMore={!!q.hasNextPage}
      />
    </>
  );
}

// ─────────────────────────────────────────────────────────────────────────────
// Mobile: Transactions
// ─────────────────────────────────────────────────────────────────────────────

function MobileTransactions({ address }: { address: string }) {
  const q = useBlockscoutInfiniteQuery("general:address_txs", {
    pathParams: { hash: address },
    queryOptions: { staleTime: 15_000 },
  });

  const items = useMemo<Array<Transaction>>(
    () => q.data?.pages.flatMap((p) => p.items) ?? [],
    [q.data],
  );

  if (q.isLoading) return <MobileSkeletonRows />;
  if (q.isError) return <MobileErrorBanner message={q.error?.message} />;
  if (items.length === 0) {
    return (
      <div className="px-4 py-8 text-center text-sm text-[#828282]">
        No transactions yet
      </div>
    );
  }

  return (
    <>
      <div className="divide-y divide-[var(--brand-border)]">
        {items.map((tx, i) => {
          const fromHash = tx.from?.hash?.toLowerCase() ?? "";
          const isOut = fromHash === address;
          const counterparty = isOut ? tx.to?.hash : tx.from?.hash;
          const statusOk = tx.status === "ok";
          const statusColor =
            tx.status === "error"
              ? "text-[var(--brand-red)]"
              : statusOk
                ? "text-white"
                : "text-[#AFAFAF]";
          const key = `${tx.hash}-${i}`;
          return (
            <div
              key={key}
              className="flex items-center justify-between gap-3 px-4 py-3 text-xs"
            >
              <div className="min-w-0 flex-1">
                <div className={cn("font-semibold", statusColor)}>
                  {tx.method ?? (isOut ? "Sent" : "Received")}
                </div>
                <div className="text-[#828282] font-mono truncate">
                  {counterparty ? shortAddrSafe(counterparty) : "—"}
                </div>
              </div>
              <div className="text-right shrink-0">
                <div className="font-mono text-white">
                  {formatPaxValue(tx.value)}
                </div>
                <div className="text-[#828282]">{fmtDate(tx.timestamp)}</div>
              </div>
              <a
                href={paxscanTxUrl(tx.hash)}
                target="_blank"
                rel="noopener"
                className="text-[#828282] shrink-0"
                aria-label="View on Paxscan"
              >
                <ExternalLink className="h-3.5 w-3.5" />
              </a>
            </div>
          );
        })}
      </div>
      <MobileLoadMore
        onClick={() => q.fetchNextPage()}
        isLoading={q.isFetchingNextPage}
        hasMore={!!q.hasNextPage}
      />
    </>
  );
}

// ─────────────────────────────────────────────────────────────────────────────
// Shared chrome pieces
// ─────────────────────────────────────────────────────────────────────────────

function DesktopSkeletonRows() {
  return (
    <div className="space-y-2">
      {Array.from({ length: 5 }).map((_, i) => (
        <div
          key={i}
          className="h-10 rounded-md bg-muted/30 animate-pulse"
        />
      ))}
    </div>
  );
}

function DesktopEmpty({ label }: { label: string }) {
  return (
    <div className="py-12 text-center text-sm text-muted-foreground">
      {label}
    </div>
  );
}

function DesktopErrorBanner({ message }: { message?: string }) {
  return (
    <div className="rounded-[var(--radius-lg)] border border-red-500/30 bg-red-500/10 px-4 py-3 text-xs text-red-400">
      {message ?? "Failed to load on-chain history"}
    </div>
  );
}

function DesktopLoadMore({
  onClick,
  isLoading,
  hasMore,
}: {
  onClick: () => void;
  isLoading: boolean;
  hasMore: boolean;
}) {
  if (!hasMore) return null;
  return (
    <div className="flex justify-center py-2 border-t border-white/[0.06]">
      <button
        type="button"
        onClick={onClick}
        disabled={isLoading}
        className="h-7 px-3 text-xs font-medium rounded-[var(--radius-md)] bg-surface-2 text-foreground hover:bg-muted/30 disabled:opacity-50 transition-colors"
      >
        {isLoading ? "Loading…" : "Load more"}
      </button>
    </div>
  );
}

function MobileSkeletonRows() {
  return (
    <div className="divide-y divide-[var(--brand-border)]">
      {Array.from({ length: 4 }).map((_, i) => (
        <div
          key={i}
          className="h-[54px] bg-[var(--brand-surface)] animate-pulse"
          style={{ opacity: 0.6 }}
        />
      ))}
    </div>
  );
}

function MobileErrorBanner({ message }: { message?: string }) {
  return (
    <div className="px-4 py-3 text-xs text-[var(--brand-red)]">
      {message ?? "Failed to load on-chain history"}
    </div>
  );
}

function MobileLoadMore({
  onClick,
  isLoading,
  hasMore,
}: {
  onClick: () => void;
  isLoading: boolean;
  hasMore: boolean;
}) {
  if (!hasMore) return null;
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={isLoading}
      className="w-full border-t border-[var(--brand-border)] bg-[var(--brand-black)] py-3 text-xs font-medium text-[#AFAFAF] disabled:opacity-50 hover:text-white transition-colors"
    >
      {isLoading ? "Loading…" : "Load more"}
    </button>
  );
}

// ─────────────────────────────────────────────────────────────────────────────
// Formatting helpers
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Convert a Blockscout `TokenTransfer.total` payload to a human-readable
 * amount. Handles ERC-20 (decimals+value), ERC-721 (token_id only),
 * ERC-1155 (value+decimals+token_id), and ERC-404 (either shape).
 * Returns "—" when no renderable quantity is present.
 */
function formatTokenAmount(t: TokenTransfer): string {
  const total = t.total;
  if (!total) return "—";

  // ERC-721 branch: token_id only, no value.
  if ("token_id" in total && !("value" in total)) {
    return total.token_id != null ? `#${total.token_id}` : "—";
  }

  // ERC-404 alt branch: token_id present but value is empty.
  if (
    "token_id" in total &&
    total.token_id != null &&
    "value" in total &&
    !total.value
  ) {
    return `#${total.token_id}`;
  }

  // ERC-20 / ERC-1155 / ERC-404 with value: decimals-aware formatting.
  if ("value" in total && total.value) {
    const decStr = "decimals" in total ? total.decimals : null;
    const decimals = decStr != null && decStr !== "" ? parseInt(decStr, 10) : 0;
    return formatDecimalString(total.value, Number.isFinite(decimals) ? decimals : 0);
  }

  return "—";
}

/**
 * Format a raw uint string with N decimals into a trimmed display string
 * (max 4 fractional digits, locale thousand-separators on the whole part).
 * Uses BigInt to avoid precision loss for 18-decimal tokens.
 */
function formatDecimalString(raw: string, decimals: number): string {
  try {
    const big = BigInt(raw);
    if (decimals <= 0) return big.toLocaleString("en-US");
    const base = BigInt(10) ** BigInt(decimals);
    const whole = big / base;
    const frac = big % base;
    const wholeStr = whole.toLocaleString("en-US");
    if (frac === 0n) return wholeStr;
    const fracStr = frac.toString().padStart(decimals, "0").slice(0, 4);
    const trimmed = fracStr.replace(/0+$/, "");
    return trimmed ? `${wholeStr}.${trimmed}` : wholeStr;
  } catch {
    return raw;
  }
}

/** Format a native PAX transfer value (always 18 decimals) for display. */
function formatPaxValue(value: string | null | undefined): string {
  if (!value || value === "0") return "0";
  return formatDecimalString(value, 18);
}

function txStatusColor(status: Transaction["status"]): string {
  if (status === "ok") return "bg-emerald-500/10 text-emerald-400";
  if (status === "error") return "bg-red-500/10 text-red-400";
  return "bg-amber-500/10 text-amber-400";
}

function shortAddrSafe(s: string | null | undefined): string {
  if (!s) return "—";
  return shortAddr(s);
}
