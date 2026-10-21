"use client";

import * as React from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { Command } from "cmdk";
import { ArrowLeft, Clock, Search } from "lucide-react";
import { cn } from "../lib/utils";
import { usePlatform, type PlatformSetting } from "../lib/platform";
import { IconButton } from "./button";

export interface SearchResultItem {
  id: string;
  title: string;
  subtitle?: string;
  icon?: React.ReactNode;
  /** Extra match terms. */
  keywords?: string[];
}

export interface SearchResultGroup {
  id: string;
  label: string;
  items: SearchResultItem[];
}

/* ------------------------------------------------ desktop: command bar */

function CommandBar({
  open,
  onOpenChange,
  groups,
  onSelect,
  placeholder = "Search agents, transactions, actions…",
  portalContainer,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  groups: SearchResultGroup[];
  onSelect?: (item: SearchResultItem) => void;
  placeholder?: string;
  portalContainer?: HTMLElement | null;
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal container={portalContainer ?? undefined}>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/40 data-[state=open]:animate-fade-in" />
        <Dialog.Content
          className={cn(
            "fixed top-[18%] left-1/2 z-50 w-[calc(100vw-2rem)] max-w-[560px] -translate-x-1/2",
            "overflow-hidden rounded-xl bg-surface shadow-overlay outline-none",
            "data-[state=open]:animate-fade-in",
          )}
        >
          <Dialog.Title className="sr-only">Search</Dialog.Title>
          <Command label="Global search" className="flex flex-col">
            <div className="flex items-center gap-3 border-b border-border px-4">
              <Search className="size-[18px] shrink-0 text-muted-foreground" aria-hidden />
              <Command.Input
                autoFocus
                placeholder={placeholder}
                className="h-14 w-full bg-transparent text-[15px] text-foreground outline-none placeholder:text-faint-foreground"
              />
              <kbd className="shrink-0 rounded border border-border bg-surface-sunken px-1.5 py-0.5 text-[10px] font-semibold text-muted-foreground">
                ESC
              </kbd>
            </div>
            <Command.List className="lx-scroll max-h-[320px] overflow-y-auto p-2">
              <Command.Empty className="py-10 text-center text-sm text-muted-foreground">
                No results found.
              </Command.Empty>
              {groups.map((g) => (
                <Command.Group
                  key={g.id}
                  heading={g.label}
                  className="[&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:font-bold [&_[cmdk-group-heading]]:tracking-wide [&_[cmdk-group-heading]]:text-faint-foreground [&_[cmdk-group-heading]]:uppercase"
                >
                  {g.items.map((item) => (
                    <Command.Item
                      key={item.id}
                      value={`${item.title} ${item.subtitle ?? ""} ${(item.keywords ?? []).join(" ")}`}
                      onSelect={() => {
                        onSelect?.(item);
                        onOpenChange(false);
                      }}
                      className="flex cursor-pointer items-center gap-3 rounded-md px-3 py-2.5 data-[selected=true]:bg-surface-sunken"
                    >
                      {item.icon && (
                        <span className="inline-flex size-9 shrink-0 items-center justify-center rounded-full bg-surface-sunken text-foreground-secondary [&_svg]:size-4">
                          {item.icon}
                        </span>
                      )}
                      <span className="flex min-w-0 flex-col">
                        <span className="truncate text-sm font-semibold text-foreground">
                          {item.title}
                        </span>
                        {item.subtitle && (
                          <span className="truncate text-xs text-muted-foreground">
                            {item.subtitle}
                          </span>
                        )}
                      </span>
                    </Command.Item>
                  ))}
                </Command.Group>
              ))}
            </Command.List>
          </Command>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

/* ------------------------------------------------- mobile: search screen */

function SearchScreen({
  open,
  onOpenChange,
  groups,
  onSelect,
  recents,
  placeholder = "Search",
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  groups: SearchResultGroup[];
  onSelect?: (item: SearchResultItem) => void;
  recents?: SearchResultItem[];
  placeholder?: string;
}) {
  const [query, setQuery] = React.useState("");

  React.useEffect(() => {
    if (open) setQuery("");
  }, [open]);

  if (!open) return null;

  const q = query.trim().toLowerCase();
  const matches = (item: SearchResultItem) =>
    !q ||
    item.title.toLowerCase().includes(q) ||
    item.subtitle?.toLowerCase().includes(q) ||
    item.keywords?.some((k) => k.toLowerCase().includes(q));

  const shownGroups = groups
    .map((g) => ({ ...g, items: g.items.filter(matches) }))
    .filter((g) => g.items.length > 0);

  return (
    <div className="fixed inset-0 z-50 flex flex-col bg-background animate-fade-in">
      <header className="flex items-center gap-3 border-b border-border bg-surface px-4 py-3">
        <IconButton variant="outline" size="sm" onClick={() => onOpenChange(false)} aria-label="Back">
          <ArrowLeft />
        </IconButton>
        <div className="flex h-10 flex-1 items-center gap-2.5 rounded-full border border-border bg-surface px-4 focus-within:border-accent focus-within:ring-2 focus-within:ring-accent/20">
          <Search className="size-4 shrink-0 text-muted-foreground" aria-hidden />
          <input
            autoFocus
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={placeholder}
            className="w-full bg-transparent text-[15px] text-foreground outline-none placeholder:text-faint-foreground"
          />
        </div>
      </header>

      <div className="lx-scroll flex-1 overflow-y-auto p-4">
        {!q && recents && recents.length > 0 && (
          <section>
            <h4 className="pb-1 text-xs font-bold tracking-wide text-faint-foreground uppercase">
              Recent
            </h4>
            {recents.map((item) => (
              <button
                key={item.id}
                type="button"
                onClick={() => {
                  onSelect?.(item);
                  onOpenChange(false);
                }}
                className="flex w-full items-center gap-3 rounded-md py-2.5 text-left"
              >
                <span className="inline-flex size-9 shrink-0 items-center justify-center rounded-full bg-surface-sunken text-muted-foreground [&_svg]:size-4">
                  {item.icon ?? <Clock />}
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[15px] font-semibold text-foreground">
                    {item.title}
                  </span>
                  {item.subtitle && (
                    <span className="block truncate text-[13px] text-muted-foreground">
                      {item.subtitle}
                    </span>
                  )}
                </span>
              </button>
            ))}
          </section>
        )}

        {shownGroups.map((g) => (
          <section key={g.id} className="pt-3">
            <h4 className="pb-1 text-xs font-bold tracking-wide text-faint-foreground uppercase">
              {g.label}
            </h4>
            {g.items.map((item) => (
              <button
                key={item.id}
                type="button"
                onClick={() => {
                  onSelect?.(item);
                  onOpenChange(false);
                }}
                className="flex w-full items-center gap-3 rounded-md py-2.5 text-left"
              >
                {item.icon && (
                  <span className="inline-flex size-9 shrink-0 items-center justify-center rounded-full bg-surface-sunken text-foreground-secondary [&_svg]:size-4">
                    {item.icon}
                  </span>
                )}
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[15px] font-semibold text-foreground">
                    {item.title}
                  </span>
                  {item.subtitle && (
                    <span className="block truncate text-[13px] text-muted-foreground">
                      {item.subtitle}
                    </span>
                  )}
                </span>
              </button>
            ))}
          </section>
        ))}

        {q && shownGroups.length === 0 && (
          <p className="py-10 text-center text-sm text-muted-foreground">No results for “{query}”.</p>
        )}
      </div>
    </div>
  );
}

/* ------------------------------------------------------------- wrapper */

/**
 * Search, per platform:
 * - mobile:  a pushed full-screen search page with autofocus + recents
 * - desktop: a global command bar (binds Cmd+K / Ctrl+K) with type-ahead
 */
export function GlobalSearch({
  open,
  onOpenChange,
  groups,
  onSelect,
  recents,
  placeholder,
  enableHotkey = true,
  platform,
  portalContainer,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  groups: SearchResultGroup[];
  onSelect?: (item: SearchResultItem) => void;
  recents?: SearchResultItem[];
  placeholder?: string;
  /** Bind Cmd+K / Ctrl+K to open. Desktop only. Default true. */
  enableHotkey?: boolean;
  platform?: PlatformSetting;
  portalContainer?: HTMLElement | null;
}) {
  const resolved = usePlatform(platform);

  React.useEffect(() => {
    if (!enableHotkey || resolved !== "desktop") return;
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        onOpenChange(!open);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [enableHotkey, resolved, open, onOpenChange]);

  return resolved === "mobile" ? (
    <SearchScreen
      open={open}
      onOpenChange={onOpenChange}
      groups={groups}
      onSelect={onSelect}
      recents={recents}
      placeholder={placeholder}
    />
  ) : (
    <CommandBar
      open={open}
      onOpenChange={onOpenChange}
      groups={groups}
      onSelect={onSelect}
      placeholder={placeholder}
      portalContainer={portalContainer}
    />
  );
}
