import { CheckIcon, ChevronDownIcon, ExternalLinkIcon, XIcon } from "lucide-react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { useId, useState, type ReactNode } from "react";

import { Numeral } from "@/components/trading/numeral";
import { TokenIcon } from "@/components/trading/token-icon";
import { SPRING_LAYOUT } from "@/lib/ease";
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

export type TransferStep = "submitted" | "confirming" | "credited" | "failed";

export type TransferStatusItem = {
  id: string;
  type?: "deposit" | "withdraw";
  token: string;
  iconSrc?: string;
  amount: number;
  network?: string;
  step: TransferStep;
  /** Block confirmations so far, shown as n/m while confirming. */
  confirmations?: number;
  requiredConfirmations?: number;
  /** When it was submitted, preformatted. */
  time?: ReactNode;
  /** Estimated time left, e.g. "~2 min". */
  eta?: ReactNode;
  txHash?: string;
};

export type TransferStatusGroup = "pending" | "failed" | "completed";

export interface DepositStatusProps {
  items: TransferStatusItem[];
  open?: boolean;
  defaultOpen?: boolean;
  onOpenChange?: (open: boolean) => void;
  /** Dismiss a summary line. Omit to hide the close buttons. */
  onDismiss?: (group: TransferStatusGroup) => void;
  /** Build a block-explorer link for a transfer. */
  explorerUrl?: (item: TransferStatusItem) => string | undefined;
  /** Rendered at the end of the expanded list, e.g. a "View history" link. */
  footer?: ReactNode;
  className?: string;
}

const groupOf = (step: TransferStep): TransferStatusGroup => (step === "credited" ? "completed" : step === "failed" ? "failed" : "pending");

const GROUP_TEXT: Record<TransferStatusGroup, [one: string, many: string]> = {
  pending: ["You have a pending transaction", "You have pending transactions"],
  failed: ["A transaction failed", "Some transactions failed"],
  completed: ["You have a successful transaction", "You have successful transactions"],
};

const GROUP_DOT: Record<TransferStatusGroup, string> = {
  pending: "bg-primary",
  failed: "bg-rose-500",
  completed: "bg-emerald-500",
};

const STEP_ORDER = ["submitted", "confirming", "credited"] as const;

/** Submitted → confirming n/m → credited, with the failure shown on the step it stopped at. */
export function TransferSteps({ item }: { item: TransferStatusItem }) {
  const reduce = useReducedMotion();
  const failed = item.step === "failed";
  // A failed transfer stops at confirming unless it never got confirmations.
  const reached = failed ? (item.confirmations ? 1 : 0) : STEP_ORDER.indexOf(item.step as (typeof STEP_ORDER)[number]);
  const confirmText =
    item.requiredConfirmations != null ? `${Math.min(item.confirmations ?? 0, item.requiredConfirmations)}/${item.requiredConfirmations}` : undefined;
  const labels = ["Submitted", confirmText ? `Confirming ${confirmText}` : "Confirming", item.type === "withdraw" ? "Sent" : "Credited"];
  const confirmFraction =
    item.step === "confirming" && item.requiredConfirmations ? Math.min(1, (item.confirmations ?? 0) / item.requiredConfirmations) : reached >= 2 ? 1 : 0;

  return (
    <ol className="grid grid-cols-3 items-start" aria-label="Progress">
      {labels.map((label, i) => {
        const done = i < reached || (i === reached && item.step === "credited");
        const current = i === reached && !done;
        const isFail = failed && current;
        const fill = i === 1 ? (reached >= 1 ? 1 : 0) : confirmFraction;
        return (
          <li key={label} className="relative flex flex-col items-center gap-1 text-center" aria-current={current ? "step" : undefined}>
            {i > 0 && (
              <span aria-hidden="true" className="absolute top-[7px] right-[calc(50%+12px)] left-[calc(-50%+12px)] h-0.5 overflow-hidden rounded-full bg-border">
                <motion.span
                  className="absolute inset-y-0 left-0 rounded-full bg-emerald-500"
                  initial={false}
                  animate={{ width: `${fill * 100}%` }}
                  transition={reduce ? { duration: 0 } : SPRING_LAYOUT}
                />
              </span>
            )}
            <span
              className={cn(
                "grid size-4 place-items-center rounded-full border-[1.5px]",
                done && "border-emerald-500 bg-emerald-500 text-white dark:text-emerald-950",
                current && !isFail && "border-primary",
                isFail && "border-rose-500 bg-rose-500 text-white",
                !done && !current && "border-border",
              )}
            >
              {done && <CheckIcon className="size-2.5" strokeWidth={3} />}
              {isFail && <XIcon className="size-2.5" strokeWidth={3} />}
              {current && !isFail && <span className={cn("size-1.5 rounded-full bg-primary", !reduce && "animate-pulse")} />}
            </span>
            <span className={cn("text-[10px] leading-tight", done || current ? "text-foreground" : "text-muted-foreground", isFail && "text-rose-600 dark:text-rose-400")}>
              {isFail ? "Failed" : label}
            </span>
          </li>
        );
      })}
    </ol>
  );
}

/** A compact status chip for in-flight transfers that expands into per-transfer step progress. */
export function DepositStatus({ items, open, defaultOpen = false, onOpenChange, onDismiss, explorerUrl, footer, className }: DepositStatusProps) {
  const [isOpen, setOpen] = useControllable(open, defaultOpen, onOpenChange);
  const reduce = useReducedMotion();
  const listId = useId();
  if (items.length === 0) return null;

  const groups = (["pending", "failed", "completed"] as const)
    .map((g) => ({ group: g, list: items.filter((i) => groupOf(i.step) === g) }))
    .filter((g) => g.list.length > 0);

  return (
    <div data-slot="deposit-status" className={cn("flex w-full flex-col overflow-hidden rounded-xl border border-border bg-container text-xs", className)}>
      {groups.map(({ group, list }) => {
        const single = list.length === 1;
        return (
          <div key={group} className="flex items-center gap-1 border-b border-border/60 pr-1.5 last-of-type:border-0">
            <button
              type="button"
              aria-expanded={isOpen}
              aria-controls={listId}
              onClick={() => setOpen(!isOpen)}
              className="focus-ring flex h-9 min-w-0 flex-1 items-center gap-2 rounded-lg pl-3 text-left text-foreground/80 hover:text-foreground"
            >
              <span className={cn("size-2 shrink-0 rounded-full", GROUP_DOT[group], group === "pending" && !reduce && "animate-pulse")} />
              <span className="truncate">{GROUP_TEXT[group][single ? 0 : 1]}</span>
              <span className="ml-auto flex items-center gap-1.5">
                {group === "pending" && single && list[0].eta ? (
                  <span className="text-[11px] text-primary">{list[0].eta}</span>
                ) : (
                  <span className="grid h-[18px] min-w-[18px] place-items-center rounded-full bg-container-higher px-1 text-[10px] font-medium tabular-nums">{list.length}</span>
                )}
                <ChevronDownIcon className={cn("size-3.5 text-muted-foreground transition-transform duration-200", isOpen && "rotate-180")} />
              </span>
            </button>
            {onDismiss && (
              <button
                type="button"
                aria-label={`Dismiss ${group} transactions`}
                onClick={() => onDismiss(group)}
                className="focus-ring grid size-6 shrink-0 place-items-center rounded-full text-muted-foreground hover:bg-container-high hover:text-foreground"
              >
                <XIcon className="size-3.5" />
              </button>
            )}
          </div>
        );
      })}

      <AnimatePresence initial={false}>
        {isOpen && (
          <motion.div
            id={listId}
            key="list"
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={reduce ? { duration: 0 } : SPRING_LAYOUT}
            className="overflow-hidden border-t border-border"
          >
            <ul className="flex max-h-80 flex-col gap-1 overflow-y-auto p-1.5">
              {items.map((item) => {
                const href = item.txHash ? explorerUrl?.(item) : undefined;
                return (
                  <li key={item.id} className="flex flex-col gap-2.5 rounded-lg bg-container-high/60 p-2.5">
                    <div className="flex items-start gap-2">
                      <TokenIcon symbol={item.token} src={item.iconSrc} size="sm" />
                      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                        <span className="flex items-baseline gap-1 text-xs font-medium">
                          {item.type === "withdraw" ? "Withdraw" : "Deposit"}
                          <Numeral value={item.amount} dp={6} pad={false} unit={item.token} />
                        </span>
                        <span className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
                          {item.network}
                          {item.network && item.time != null && <span aria-hidden="true">·</span>}
                          {item.time}
                        </span>
                      </div>
                      {item.txHash &&
                        (href ? (
                          <a
                            href={href}
                            target="_blank"
                            rel="noreferrer"
                            className="focus-ring flex items-center gap-1 rounded font-mono text-[11px] text-muted-foreground hover:text-foreground"
                          >
                            {shortenAddress(item.txHash, 6, 4)}
                            <ExternalLinkIcon className="size-3" />
                            <span className="sr-only">View transaction on explorer</span>
                          </a>
                        ) : (
                          <span className="font-mono text-[11px] text-muted-foreground">{shortenAddress(item.txHash, 6, 4)}</span>
                        ))}
                    </div>
                    <TransferSteps item={item} />
                  </li>
                );
              })}
            </ul>
            {footer && <div className="border-t border-border px-3 py-2">{footer}</div>}
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}
