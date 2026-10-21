"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import type { KeyboardEvent as ReactKeyboardEvent } from "react";
import { ExternalLink, Loader2, Search as SearchIcon, X as XIcon } from "lucide-react";

import {
  paxscanAddressUrl,
  paxscanBlockUrl,
  paxscanSearchUrl,
  paxscanTokenUrl,
  paxscanTxUrl,
  useBlockscoutInfiniteQuery,
  useBlockscoutQuery,
} from "@/lib/paxscan";
import type {
  SearchRedirectResult,
  SearchResultItem,
} from "@/lib/paxscan";
import { cn } from "@/ui/utils/cn";

/**
 * Global Paxscan search box.
 *
 * Typeahead-routed: debounced (300ms) calls to `general:search` for result
 * items plus `general:search_check_redirect` for "jump to exact match" hints.
 * Results open in a new tab against the canonical explorer
 * (`PAXSCAN_BASE_URL`, FROZEN in Round 1 §1 ledger).
 *
 * Keyboard:
 *   - ArrowUp / ArrowDown — move highlight across rows
 *   - Enter               — open highlighted row (or the "all results" page
 *                           if nothing highlighted)
 *   - Escape              — close dropdown + blur input
 *
 * Mobile note: hidden on `<md` per Header conventions (the Header already
 * uses a separate `MobileNav` drawer for small viewports). A bespoke mobile
 * search surface is a future enhancement — out of Round 2 scope.
 */
export interface PaxscanSearchProps {
  className?: string;
  /** Overrides the default placeholder. */
  placeholder?: string;
}

const MIN_QUERY_LENGTH = 2;
const MAX_ITEMS = 8;
const DEBOUNCE_MS = 300;

export function PaxscanSearch({
  className,
  placeholder = "Search tx, address, block, token…",
}: PaxscanSearchProps) {
  const [q, setQ] = useState<string>("");
  const [debouncedQ, setDebouncedQ] = useState<string>("");
  const [isOpen, setIsOpen] = useState<boolean>(false);
  const [highlighted, setHighlighted] = useState<number>(-1);

  const rootRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  // Debounce raw input → debouncedQ used for queries.
  useEffect(() => {
    const t = setTimeout(() => setDebouncedQ(q.trim()), DEBOUNCE_MS);
    return () => clearTimeout(t);
  }, [q]);

  const queryEnabled = debouncedQ.length >= MIN_QUERY_LENGTH;

  const searchQ = useBlockscoutInfiniteQuery("general:search", {
    queryParams: { q: debouncedQ },
    queryOptions: {
      enabled: queryEnabled,
      staleTime: 30_000,
    },
  });

  const redirectQ = useBlockscoutQuery("general:search_check_redirect", {
    queryParams: { q: debouncedQ },
    queryOptions: {
      enabled: queryEnabled,
      staleTime: 30_000,
      retry: false,
    },
  });

  const items = useMemo<Array<SearchResultItem>>(() => {
    const first = searchQ.data?.pages?.[0]?.items ?? [];
    return first.slice(0, MAX_ITEMS);
  }, [searchQ.data]);

  const redirect: SearchRedirectResult | null = useMemo(() => {
    const r = redirectQ.data;
    if (!r || !r.redirect || !r.parameter) return null;
    return r;
  }, [redirectQ.data]);

  // Click-outside closes the dropdown.
  useEffect(() => {
    if (!isOpen) return;
    const onDown = (e: MouseEvent) => {
      if (!rootRef.current) return;
      if (!rootRef.current.contains(e.target as Node)) setIsOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [isOpen]);

  // Reset the keyboard-highlight when the visible set changes.
  useEffect(() => {
    setHighlighted(-1);
  }, [items, redirect, debouncedQ]);

  const totalRows = (redirect ? 1 : 0) + items.length;

  const urlForRow = (idx: number): string | null => {
    if (idx < 0) {
      return queryEnabled ? paxscanSearchUrl(debouncedQ) : null;
    }
    if (redirect && idx === 0) {
      return urlForRedirect(redirect, debouncedQ);
    }
    const itemIdx = redirect ? idx - 1 : idx;
    const item = items[itemIdx];
    return item ? urlForItem(item, debouncedQ) : null;
  };

  const openUrl = (url: string | null) => {
    if (!url) return;
    window.open(url, "_blank", "noopener,noreferrer");
  };

  const onKeyDown = (e: ReactKeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Escape") {
      setIsOpen(false);
      inputRef.current?.blur();
      return;
    }
    if (e.key === "Enter") {
      e.preventDefault();
      openUrl(urlForRow(highlighted));
      setIsOpen(false);
      return;
    }
    if (!isOpen || totalRows === 0) return;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setHighlighted((h) => (h + 1 >= totalRows ? 0 : h + 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setHighlighted((h) => (h <= 0 ? totalRows - 1 : h - 1));
    }
  };

  const onClear = () => {
    setQ("");
    setDebouncedQ("");
    inputRef.current?.focus();
  };

  const showDropdown =
    isOpen &&
    queryEnabled &&
    (items.length > 0 || !!redirect || searchQ.isLoading || searchQ.isError);

  return (
    <div ref={rootRef} className={cn("relative", className)}>
      <div
        className={cn(
          "flex items-center gap-1.5 h-8 px-2 rounded-[var(--radius-md)] border border-white/[0.08] bg-surface-1",
          "focus-within:border-white/[0.18] transition-colors",
        )}
      >
        <SearchIcon className="h-3.5 w-3.5 text-muted-foreground shrink-0" aria-hidden />
        <input
          ref={inputRef}
          type="text"
          role="combobox"
          aria-expanded={showDropdown}
          aria-controls="paxscan-search-listbox"
          aria-autocomplete="list"
          className="flex-1 bg-transparent text-xs font-medium placeholder:text-muted-foreground/60 text-foreground outline-none min-w-0"
          placeholder={placeholder}
          value={q}
          onChange={(e) => {
            setQ(e.target.value);
            setIsOpen(true);
          }}
          onFocus={() => setIsOpen(true)}
          onKeyDown={onKeyDown}
          autoComplete="off"
          spellCheck={false}
        />
        {searchQ.isFetching && queryEnabled && (
          <Loader2 className="h-3 w-3 text-muted-foreground animate-spin shrink-0" aria-hidden />
        )}
        {q.length > 0 && (
          <button
            type="button"
            onClick={onClear}
            aria-label="Clear search"
            className="text-muted-foreground hover:text-foreground shrink-0"
          >
            <XIcon className="h-3 w-3" />
          </button>
        )}
      </div>

      {showDropdown && (
        <div
          id="paxscan-search-listbox"
          role="listbox"
          className="absolute left-0 right-0 top-full mt-1 z-50 rounded-[var(--radius-lg)] border border-white/[0.08] bg-surface-1 shadow-xl overflow-hidden max-h-[26rem] overflow-y-auto"
        >
          {redirect && (
            <RedirectRow
              redirect={redirect}
              q={debouncedQ}
              isHighlighted={highlighted === 0}
              onHover={() => setHighlighted(0)}
            />
          )}

          {items.map((item, i) => {
            const rowIdx = redirect ? i + 1 : i;
            const url = urlForItem(item, debouncedQ);
            return (
              <ResultRow
                key={`${item.type}-${i}`}
                item={item}
                url={url}
                isHighlighted={highlighted === rowIdx}
                isLast={i === items.length - 1 && !queryEnabled}
                onHover={() => setHighlighted(rowIdx)}
              />
            );
          })}

          {items.length === 0 && !redirect && searchQ.isLoading && (
            <div className="px-3 py-6 text-center text-xs text-muted-foreground">
              Searching…
            </div>
          )}

          {items.length === 0 && !redirect && !searchQ.isLoading && !searchQ.isError && (
            <div className="px-3 py-6 text-center text-xs text-muted-foreground">
              No results for “{debouncedQ}”
            </div>
          )}

          {searchQ.isError && (
            <div className="px-3 py-3 text-xs text-red-400 border-t border-white/[0.06]">
              {searchQ.error?.message ?? "Search failed"}
            </div>
          )}

          {queryEnabled && (items.length > 0 || !!redirect) && (
            <a
              href={paxscanSearchUrl(debouncedQ)}
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-center justify-center gap-1 px-3 py-2 text-[10px] text-muted-foreground hover:text-foreground bg-surface-2/40 border-t border-white/[0.06]"
            >
              View all results on Paxscan <ExternalLink className="h-3 w-3" aria-hidden />
            </a>
          )}
        </div>
      )}
    </div>
  );
}

// ─────────────────────────────────────────────────────────────────────────────
// Row components
// ─────────────────────────────────────────────────────────────────────────────

function RedirectRow({
  redirect,
  q,
  isHighlighted,
  onHover,
}: {
  redirect: SearchRedirectResult;
  q: string;
  isHighlighted: boolean;
  onHover: () => void;
}) {
  const url = urlForRedirect(redirect, q);
  return (
    <a
      href={url}
      target="_blank"
      rel="noopener noreferrer"
      role="option"
      aria-selected={isHighlighted}
      onMouseEnter={onHover}
      className={cn(
        "flex items-center gap-2 px-3 py-2 text-xs border-b border-white/[0.06]",
        isHighlighted ? "bg-surface-2" : "hover:bg-surface-2",
      )}
    >
      <span className="text-[9px] uppercase tracking-wider px-1.5 py-0.5 rounded font-semibold bg-emerald-500/10 text-emerald-300 shrink-0">
        Jump
      </span>
      <span className="font-mono text-foreground truncate min-w-0">
        {redirect.parameter}
      </span>
      {redirect.type && (
        <span className="ml-auto text-[10px] uppercase tracking-wider text-muted-foreground shrink-0">
          {typeDisplay(redirect.type)}
        </span>
      )}
      <ExternalLink className="h-3 w-3 text-muted-foreground shrink-0" aria-hidden />
    </a>
  );
}

function ResultRow({
  item,
  url,
  isHighlighted,
  isLast,
  onHover,
}: {
  item: SearchResultItem;
  url: string;
  isHighlighted: boolean;
  isLast: boolean;
  onHover: () => void;
}) {
  return (
    <a
      href={url}
      target="_blank"
      rel="noopener noreferrer"
      role="option"
      aria-selected={isHighlighted}
      onMouseEnter={onHover}
      className={cn(
        "flex items-center gap-2 px-3 py-2 text-xs",
        !isLast && "border-b border-white/[0.06]",
        isHighlighted ? "bg-surface-2" : "hover:bg-surface-2",
      )}
    >
      <span
        className={cn(
          "text-[9px] uppercase tracking-wider px-1.5 py-0.5 rounded font-semibold shrink-0",
          typeBadgeClass(item.type),
        )}
      >
        {typeDisplay(item.type)}
      </span>
      <span className="text-foreground font-mono truncate min-w-0">
        {primaryLabel(item)}
      </span>
      {secondaryLabel(item) && (
        <span className="ml-auto text-muted-foreground font-mono truncate max-w-[40%] shrink-0">
          {secondaryLabel(item)}
        </span>
      )}
      <ExternalLink className="h-3 w-3 text-muted-foreground shrink-0" aria-hidden />
    </a>
  );
}

// ─────────────────────────────────────────────────────────────────────────────
// URL resolution
// ─────────────────────────────────────────────────────────────────────────────

function urlForItem(item: SearchResultItem, q: string): string {
  switch (item.type) {
    case "token":
      return paxscanTokenUrl(item.address_hash);
    case "address":
    case "contract":
    case "label":
    case "metadata_tag":
    case "cluster":
      return paxscanAddressUrl(item.address_hash);
    case "ens_domain":
      return item.address_hash
        ? paxscanAddressUrl(item.address_hash)
        : paxscanSearchUrl(q);
    case "transaction":
      return paxscanTxUrl(item.transaction_hash);
    case "block":
      return paxscanBlockUrl(item.block_number);
    case "user_operation":
    case "blob":
    case "tac_operation":
    default:
      // No URL helper exists for these MVP-deferred types — send the user to
      // the explorer's own search page so they can click through from there.
      return paxscanSearchUrl(q);
  }
}

function urlForRedirect(r: SearchRedirectResult, q: string): string {
  if (!r.parameter) return paxscanSearchUrl(q);
  switch (r.type) {
    case "address":
    case "ens_domain":
      return paxscanAddressUrl(r.parameter);
    case "block":
      return paxscanBlockUrl(r.parameter);
    case "transaction":
      return paxscanTxUrl(r.parameter);
    case "user_operation":
    case "blob":
    default:
      return paxscanSearchUrl(q);
  }
}

// ─────────────────────────────────────────────────────────────────────────────
// Display helpers
// ─────────────────────────────────────────────────────────────────────────────

function typeDisplay(t: string): string {
  switch (t) {
    case "transaction":
      return "tx";
    case "user_operation":
      return "userop";
    case "metadata_tag":
      return "tag";
    case "ens_domain":
      return "ens";
    case "tac_operation":
      return "tac";
    default:
      return t;
  }
}

function typeBadgeClass(t: string): string {
  switch (t) {
    case "token":
      return "bg-violet-500/10 text-violet-300";
    case "address":
    case "contract":
    case "label":
    case "metadata_tag":
    case "cluster":
    case "ens_domain":
      return "bg-blue-500/10 text-blue-300";
    case "transaction":
      return "bg-emerald-500/10 text-emerald-300";
    case "block":
      return "bg-amber-500/10 text-amber-300";
    default:
      return "bg-zinc-500/10 text-zinc-300";
  }
}

function primaryLabel(item: SearchResultItem): string {
  switch (item.type) {
    case "token":
      return item.symbol ? `${item.symbol} · ${item.name}` : item.name;
    case "address":
    case "contract":
    case "label":
    case "metadata_tag":
    case "cluster":
      return item.name || shortHex(item.address_hash);
    case "ens_domain":
      return item.ens_info?.name || shortHex(item.address_hash ?? "");
    case "transaction":
      return shortHex(item.transaction_hash);
    case "block":
      return `#${item.block_number}`;
    case "user_operation":
      return shortHex(item.user_operation_hash);
    case "blob":
      return shortHex(item.blob_hash);
    case "tac_operation":
      return "TAC operation";
    default:
      return "";
  }
}

function secondaryLabel(item: SearchResultItem): string | null {
  switch (item.type) {
    case "token":
      return shortHex(item.address_hash);
    case "address":
    case "contract":
    case "label":
    case "metadata_tag":
    case "cluster":
      return shortHex(item.address_hash);
    case "ens_domain":
      return item.address_hash ? shortHex(item.address_hash) : null;
    case "block":
      return shortHex(item.block_hash);
    default:
      return null;
  }
}

function shortHex(s: string): string {
  if (!s) return "";
  if (s.length <= 14) return s;
  return `${s.slice(0, 8)}…${s.slice(-4)}`;
}
