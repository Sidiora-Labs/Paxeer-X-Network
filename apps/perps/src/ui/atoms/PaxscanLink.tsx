import { ExternalLink } from "lucide-react";
import * as React from "react";

import { paxscanAddressUrl, paxscanBlockUrl, paxscanTxUrl } from "@/lib/paxscan";
import { cn } from "@/ui/utils/cn";

/**
 * Kind of on-chain object this link resolves to.
 *
 * - `tx`      → transaction hash (32-byte 0x-prefixed hex)
 * - `block`   → block number (decimal or 0x-prefixed hex)
 * - `address` → account / contract address (20-byte 0x-prefixed hex)
 */
export type PaxscanLinkKind = "tx" | "block" | "address";

export interface PaxscanLinkProps {
  /** What the `value` refers to. Determines which URL helper is invoked. */
  kind: PaxscanLinkKind;
  /** The hash / block number / address to deep-link. */
  value: string | number | bigint;
  /** Optional inline label (e.g. a truncated hash). Icon-only when omitted. */
  label?: React.ReactNode;
  /** Override the default `h-3 w-3` icon sizing. */
  iconClassName?: string;
  /** Override the default muted-foreground anchor class list. */
  className?: string;
  /** Tooltip override. Defaults to a per-kind label. */
  title?: string;
}

/**
 * Small inline "view on Paxscan" chip.
 *
 * Matches the existing `features/spot/*` explorer-link pattern:
 * `text-muted-foreground hover:text-foreground` + `<ExternalLink className="h-3 w-3" />`.
 *
 * Phase-1 contract (per supervisor DECISION 017): emits an external
 * `<a target="_blank" rel="noopener noreferrer">`. Internal-routing
 * (e.g. `/explorer?focus=<hash>` via `next/link`) is a Phase-3 enhancement
 * and is deliberately NOT added here.
 *
 * Returns `null` for empty / `"0"` / `"0x"` values so subgraph nulls
 * do not render broken explorer links.
 */
export function PaxscanLink({
  kind,
  value,
  label,
  iconClassName,
  className,
  title,
}: PaxscanLinkProps): React.ReactElement | null {
  const stringValue = typeof value === "string" ? value : String(value);
  if (!stringValue || stringValue === "0" || stringValue === "0x") {
    return null;
  }

  let href: string;
  let defaultTitle: string;
  switch (kind) {
    case "tx":
      href = paxscanTxUrl(stringValue);
      defaultTitle = "View transaction on Paxscan";
      break;
    case "block":
      href = paxscanBlockUrl(stringValue);
      defaultTitle = "View block on Paxscan";
      break;
    case "address":
      href = paxscanAddressUrl(stringValue);
      defaultTitle = "View address on Paxscan";
      break;
  }

  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      title={title ?? defaultTitle}
      onClick={(event) => event.stopPropagation()}
      className={cn(
        "inline-flex items-center gap-1 text-muted-foreground hover:text-foreground transition-colors",
        className,
      )}
    >
      {label != null ? <span className="font-mono text-[10px]">{label}</span> : null}
      <ExternalLink className={cn("h-3 w-3", iconClassName)} aria-hidden />
    </a>
  );
}
