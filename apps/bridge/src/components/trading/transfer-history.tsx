import { ArrowDownToLineIcon, ArrowLeftRightIcon, ArrowUpFromLineIcon, CheckIcon, CopyIcon, ExternalLinkIcon, InboxIcon } from "lucide-react";
import { useState, type ReactNode } from "react";

import { Numeral } from "@/components/trading/numeral";
import { ChainIcon, TokenIcon } from "@/components/trading/token-icon";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { shortenAddress } from "@/lib/trading";
import { cn } from "@/lib/utils";

function useControllable<T>(value: T | undefined, defaultValue: T, onChange?: (next: T) => void) {
  const [inner, setInner] = useState(defaultValue);
  const current = value !== undefined ? value : inner;
  const set = (next: T) => {
    if (value === undefined) setInner(next);
    onChange?.(next);
  };
  return [current, set] as const;
}

export type TransferRecordType = "deposit" | "withdraw" | "transfer";
export type TransferRecordStatus = "pending" | "processing" | "completed" | "failed" | "cancelled";
export type TransferHistoryFilter = "all" | TransferRecordType;

export type TransferRecord = {
  id: string;
  type: TransferRecordType;
  token: string;
  iconSrc?: string;
  /** Gross amount, always positive — the sign comes from the type. */
  amount: number;
  /** Decimal places for amount and fee. */
  decimals?: number;
  fee?: number | null;
  /** Network name for on-chain transfers. */
  network?: string;
  networkIconSrc?: string;
  status: TransferRecordStatus;
  /** Timestamp, Date or preformatted string. */
  time: number | Date | string;
  txHash?: string;
  /** Vault name for vault deposits and withdrawals — shown instead of the network. */
  vault?: string;
  /** Source and destination account ids for internal transfers. */
  from?: string;
  to?: string;
};

export interface TransferHistoryProps {
  records: TransferRecord[];
  /** Filter tabs, in order. */
  filters?: TransferHistoryFilter[];
  filter?: TransferHistoryFilter;
  defaultFilter?: TransferHistoryFilter;
  onFilterChange?: (filter: TransferHistoryFilter) => void;
  /** Filter `records` locally by type. Turn off when you fetch per filter. */
  filterLocally?: boolean;
  /** Build a block-explorer URL for a record. Without it, hashes render as plain text. */
  explorerUrl?: (record: TransferRecord) => string | undefined;
  /** Called after a hash or account id is copied. */
  onCopy?: (text: string) => void;
  formatTime?: (time: TransferRecord["time"]) => string;
  loading?: boolean;
  emptyText?: ReactNode;
  /** Rendered under the list, e.g. pagination. */
  footer?: ReactNode;
  className?: string;
}

const FILTER_LABEL: Record<TransferHistoryFilter, string> = { all: "All", deposit: "Deposits", withdraw: "Withdrawals", transfer: "Transfers" };
const TYPE_LABEL: Record<TransferRecordType, string> = { deposit: "Deposit", withdraw: "Withdraw", transfer: "Transfer" };
const TYPE_ICON: Record<TransferRecordType, typeof ArrowDownToLineIcon> = {
  deposit: ArrowDownToLineIcon,
  withdraw: ArrowUpFromLineIcon,
  transfer: ArrowLeftRightIcon,
};

const STATUS: Record<TransferRecordStatus, { label: string; variant: "success" | "info" | "warning" | "destructive" | "tonal" }> = {
  pending: { label: "Pending", variant: "warning" },
  processing: { label: "Processing", variant: "info" },
  completed: { label: "Completed", variant: "success" },
  failed: { label: "Failed", variant: "destructive" },
  cancelled: { label: "Cancelled", variant: "tonal" },
};

const timeFormatter = new Intl.DateTimeFormat("en-US", { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false });
const defaultFormatTime = (t: TransferRecord["time"]) => (typeof t === "string" ? t : timeFormatter.format(t).replace(",", ""));

function CopyText({ text, label, onCopy }: { text: string; label: string; onCopy?: (text: string) => void }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      type="button"
      aria-label={copied ? `${label} copied` : `Copy ${label}`}
      onClick={() => {
        void navigator.clipboard?.writeText(text).catch(() => {});
        onCopy?.(text);
        setCopied(true);
      }}
      onPointerLeave={() => setCopied(false)}
      onBlur={() => setCopied(false)}
      className="focus-ring grid size-6 shrink-0 place-items-center rounded-md text-muted-foreground hover:bg-container-high hover:text-foreground"
    >
      {copied ? <CheckIcon className="size-3 text-emerald-600 dark:text-emerald-400" /> : <CopyIcon className="size-3" />}
    </button>
  );
}

const amountOf = (r: TransferRecord) => (r.type === "withdraw" ? -r.amount : r.amount);
const networkCell = (r: TransferRecord) =>
  r.vault ? (
    <span className="text-xs">
      <span className="text-muted-foreground">Vault · </span>
      {r.vault}
    </span>
  ) : r.type === "transfer" ? (
    <span className="text-xs text-muted-foreground">Internal</span>
  ) : r.network ? (
    <span className="flex items-center gap-1.5 text-xs">
      <ChainIcon name={r.network} src={r.networkIconSrc} size="2xs" />
      {r.network}
    </span>
  ) : (
    <span className="text-muted-foreground">—</span>
  );
const typeCell = (r: TransferRecord) => {
  const Icon = TYPE_ICON[r.type];
  return (
    <span className="flex items-center gap-1.5 text-xs">
      <Icon className="size-3.5 text-muted-foreground" />
      {TYPE_LABEL[r.type]}
    </span>
  );
};

const amountNode = (r: TransferRecord, valueClassName?: string) => (
  <Numeral value={amountOf(r)} dp={r.decimals ?? 2} pad={false} showSign={r.type !== "transfer"} coloring={r.type !== "transfer" && r.status === "completed"} className={valueClassName} />
);

/** Deposits, withdrawals and internal transfers with status, network, fees and transaction links. */
export function TransferHistory({
  records,
  filters = ["all", "deposit", "withdraw", "transfer"],
  filter,
  defaultFilter = "all",
  onFilterChange,
  filterLocally = true,
  explorerUrl,
  onCopy,
  formatTime = defaultFormatTime,
  loading,
  emptyText = "No transfers yet.",
  footer,
  className,
}: TransferHistoryProps) {
  const [active, setActive] = useControllable(filter, defaultFilter, onFilterChange);
  const rows = filterLocally && active !== "all" ? records.filter((r) => r.type === active) : records;

  const hashCell = (r: TransferRecord) => {
    if (r.type === "transfer" && (r.from || r.to)) {
      return (
        <span className="flex items-center gap-1 font-mono text-xs text-muted-foreground">
          {r.from ? shortenAddress(r.from, 6, 4) : "—"}
          <span aria-hidden="true">→</span>
          <span className="sr-only">to</span>
          {r.to ? shortenAddress(r.to, 6, 4) : "—"}
        </span>
      );
    }
    if (!r.txHash) return <span className="text-muted-foreground">—</span>;
    const href = explorerUrl?.(r);
    return (
      <span className="flex items-center gap-0.5">
        {href ? (
          <a
            href={href}
            target="_blank"
            rel="noreferrer"
            className="focus-ring flex items-center gap-1 rounded font-mono text-xs underline decoration-border decoration-dashed underline-offset-4 hover:text-primary"
          >
            {shortenAddress(r.txHash, 6, 4)}
            <ExternalLinkIcon className="size-3 text-muted-foreground" />
            <span className="sr-only">(opens explorer)</span>
          </a>
        ) : (
          <span className="font-mono text-xs">{shortenAddress(r.txHash, 6, 4)}</span>
        )}
        <CopyText text={r.txHash} label="transaction hash" onCopy={onCopy} />
      </span>
    );
  };

  return (
    <section data-slot="transfer-history" aria-label="Transfer history" className={cn("flex w-full flex-col gap-3 rounded-2xl border border-border bg-container p-4", className)}>
      {filters.length > 1 && (
        <Tabs value={active} onValueChange={(v) => setActive(v as TransferHistoryFilter)}>
          <TabsList variant="line" className="max-w-full overflow-x-auto">
            {filters.map((f) => (
              <TabsTrigger key={f} value={f}>
                {FILTER_LABEL[f]}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      )}

      {loading ? (
        <div className="flex flex-col gap-2">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-10 w-full rounded-lg" />
          ))}
        </div>
      ) : rows.length === 0 ? (
        <div className="flex flex-col items-center gap-2 py-10 text-center text-sm text-muted-foreground">
          <InboxIcon className="size-5" />
          {emptyText}
        </div>
      ) : (
        <>
          <div className="-mx-4 hidden overflow-x-auto md:block">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border">
                  {["Time", "Type", "Token", "Amount", "Fee", "Network", "Status", "Transaction"].map((c) => (
                    <th
                      key={c}
                      scope="col"
                      className={cn("h-9 px-3 text-[11px] font-normal whitespace-nowrap text-muted-foreground first:pl-4 last:pr-4", c === "Amount" || c === "Fee" ? "text-right" : "text-left")}
                    >
                      {c}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={r.id} className="border-b border-border/60 last:border-0 hover:bg-container-high/50">
                    <td className="h-11 py-2 pr-3 pl-4 font-mono text-xs whitespace-nowrap text-muted-foreground tabular-nums">{formatTime(r.time)}</td>
                    <td className="px-3">{typeCell(r)}</td>
                    <th scope="row" className="px-3 text-left font-normal">
                      <span className="flex items-center gap-1.5 text-xs font-medium">
                        <TokenIcon symbol={r.token} src={r.iconSrc} size="xs" />
                        {r.token}
                      </span>
                    </th>
                    <td className="px-3 text-right text-xs">{amountNode(r)}</td>
                    <td className="px-3 text-right text-xs">
                      <Numeral value={r.fee} dp={r.decimals ?? 2} pad={false} className="text-muted-foreground" />
                    </td>
                    <td className="px-3">{networkCell(r)}</td>
                    <td className="px-3">
                      <Badge variant={STATUS[r.status].variant} className="text-[11px]">
                        {STATUS[r.status].label}
                      </Badge>
                    </td>
                    <td className="py-2 pr-4 pl-3">{hashCell(r)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <ul className="flex flex-col gap-2 md:hidden">
            {rows.map((r) => (
              <li key={r.id} className="flex flex-col gap-2 rounded-xl border border-border p-3">
                <div className="flex items-center justify-between gap-2">
                  <span className="font-mono text-[11px] text-muted-foreground tabular-nums">{formatTime(r.time)}</span>
                  <Badge variant={STATUS[r.status].variant} className="text-[11px]">
                    {STATUS[r.status].label}
                  </Badge>
                </div>
                <div className="h-px bg-border" />
                <div className="flex items-center justify-between gap-2">
                  <span className="flex items-center gap-2">
                    <TokenIcon symbol={r.token} src={r.iconSrc} size="sm" />
                    <span className="flex flex-col">
                      <span className="text-xs font-medium">{r.token}</span>
                      {typeCell(r)}
                    </span>
                  </span>
                  <span className="flex flex-col items-end gap-0.5">
                    {amountNode(r, "text-sm font-medium")}
                    {r.fee != null && (
                      <span className="text-[11px] text-muted-foreground">
                        Fee <Numeral value={r.fee} dp={r.decimals ?? 2} pad={false} />
                      </span>
                    )}
                  </span>
                </div>
                <div className="flex items-center justify-between gap-2">
                  {networkCell(r)}
                  {hashCell(r)}
                </div>
              </li>
            ))}
          </ul>
        </>
      )}
      {footer}
    </section>
  );
}
