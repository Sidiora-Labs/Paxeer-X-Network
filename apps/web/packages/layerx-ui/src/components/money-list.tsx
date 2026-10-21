"use client";

import * as React from "react";
import { ArrowDownWideNarrow, ArrowUpNarrowWide, Download } from "lucide-react";
import { cn } from "../lib/utils";
import { usePlatform, type PlatformSetting } from "../lib/platform";
import { formatMoney, downloadCsv } from "../lib/format";
import type { MoneyGroup, MoneyItem } from "../lib/types";
import { AmountText } from "./amount";
import { Badge } from "./badge";
import { IconTile, List, ListItem } from "./list";
import { ArrowDownLeft, ArrowUpRight } from "lucide-react";
import { Button } from "./button";

/* -------------------------------------------------------------- shared */

function defaultLeading(item: MoneyItem) {
  const incoming = item.amount >= 0;
  return (
    <IconTile
      shape="circle"
      tone={incoming ? "accent" : "neutral"}
      className="size-10 [&_svg]:size-4"
    >
      {incoming ? <ArrowDownLeft /> : <ArrowUpRight />}
    </IconTile>
  );
}

function statusTone(status?: string) {
  if (!status) return "neutral" as const;
  const s = status.toLowerCase();
  if (s === "settled" || s === "completed" || s === "active") return "success" as const;
  if (s === "failed" || s === "blocked") return "destructive" as const;
  return "neutral" as const;
}

/* ------------------------------------------- mobile: bands + rows view */

function MoneyBands({
  groups,
  onItemClick,
}: {
  groups: MoneyGroup[];
  onItemClick?: (item: MoneyItem) => void;
}) {
  return (
    <div className="flex flex-col gap-5">
      {groups.map((g) => (
        <section key={g.id}>
          <div className="flex items-baseline justify-between pb-1">
            <h4 className="text-sm font-bold text-muted-foreground">{g.label}</h4>
            <span className="text-sm font-semibold tabular-nums text-foreground-secondary">
              {formatMoney(g.subtotal, { currency: g.currency, signed: false })}
            </span>
          </div>
          <List>
            {g.items.map((item) => (
              <ListItem
                key={item.id}
                leading={item.leading ?? defaultLeading(item)}
                title={item.title}
                subtitle={
                  item.status ? (
                    <span className="flex items-center gap-1.5">
                      {item.subtitle}
                      <Badge variant={statusTone(item.status)} size="sm">
                        {item.status}
                      </Badge>
                    </span>
                  ) : (
                    item.subtitle
                  )
                }
                trailing={<AmountText value={item.amount} currency={item.currency} />}
                trailingCaption={item.date.toLocaleDateString("en-US", {
                  day: "numeric",
                  month: "short",
                  year: "numeric",
                })}
                onClick={onItemClick ? () => onItemClick(item) : undefined}
              />
            ))}
          </List>
        </section>
      ))}
    </div>
  );
}

/* ------------------------------ desktop: sortable table w/ sticky bands */

type SortKey = "date" | "title" | "amount";
type SortDir = "asc" | "desc";

function MoneyTable({
  groups,
  onItemClick,
  exportName = "transactions.csv",
  maxHeight = 560,
}: {
  groups: MoneyGroup[];
  onItemClick?: (item: MoneyItem) => void;
  exportName?: string;
  maxHeight?: number;
}) {
  const [sortKey, setSortKey] = React.useState<SortKey>("date");
  const [sortDir, setSortDir] = React.useState<SortDir>("desc");

  const toggleSort = (key: SortKey) => {
    if (key === sortKey) setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    else {
      setSortKey(key);
      setSortDir("desc");
    }
  };

  const sortedGroups = React.useMemo(() => {
    const cmp = (a: MoneyItem, b: MoneyItem) => {
      let v = 0;
      if (sortKey === "date") v = a.date.getTime() - b.date.getTime();
      if (sortKey === "title") v = a.title.localeCompare(b.title);
      if (sortKey === "amount") v = a.amount - b.amount;
      return sortDir === "asc" ? v : -v;
    };
    return groups.map((g) => ({ ...g, items: [...g.items].sort(cmp) }));
  }, [groups, sortKey, sortDir]);

  const doExport = () => {
    const rows = groups.flatMap((g) =>
      g.items.map((i) => [
        g.label,
        i.title,
        i.subtitle ?? "",
        i.status ?? "",
        i.date.toISOString().slice(0, 10),
        i.amount.toFixed(2),
        i.currency ?? "",
      ]),
    );
    downloadCsv(exportName, ["Group", "Title", "Subtitle", "Status", "Date", "Amount", "Currency"], rows);
  };

  const SortButton = ({ id, children }: { id: SortKey; children: React.ReactNode }) => (
    <button
      type="button"
      onClick={() => toggleSort(id)}
      className={cn(
        "inline-flex items-center gap-1 text-xs font-bold tracking-wide uppercase transition-colors",
        sortKey === id ? "text-foreground" : "text-faint-foreground hover:text-muted-foreground",
      )}
    >
      {children}
      {sortKey === id &&
        (sortDir === "asc" ? (
          <ArrowUpNarrowWide className="size-3.5" />
        ) : (
          <ArrowDownWideNarrow className="size-3.5" />
        ))}
    </button>
  );

  return (
    <div className="overflow-hidden rounded-lg border border-border bg-surface">
      <div className="flex items-center justify-between border-b border-border px-4 py-2.5">
        <span className="text-sm font-semibold text-foreground-secondary">
          {groups.reduce((n, g) => n + g.items.length, 0)} records
        </span>
        <Button variant="soft" size="sm" onClick={doExport}>
          <Download />
          Export
        </Button>
      </div>
      <div className="lx-scroll overflow-y-auto" style={{ maxHeight }}>
        <table className="w-full border-collapse text-sm">
          <thead className="sticky top-0 z-20 bg-surface">
            <tr className="border-b border-border text-left">
              <th className="px-4 py-2.5 font-medium"><SortButton id="title">Description</SortButton></th>
              <th className="px-4 py-2.5 font-medium"><SortButton id="date">Date</SortButton></th>
              <th className="px-4 py-2.5 font-medium">Status</th>
              <th className="px-4 py-2.5 text-right font-medium">
                <span className="inline-flex justify-end"><SortButton id="amount">Amount</SortButton></span>
              </th>
            </tr>
          </thead>
          {sortedGroups.map((g) => (
            <tbody key={g.id}>
              {/* sticky group band */}
              <tr>
                <td
                  colSpan={4}
                  className="sticky top-[41px] z-10 border-b border-border bg-surface-sunken/70 px-4 py-1.5 backdrop-blur"
                >
                  <div className="flex items-baseline justify-between">
                    <span className="text-xs font-bold text-muted-foreground">{g.label}</span>
                    <span className="text-xs font-semibold tabular-nums text-foreground-secondary">
                      {formatMoney(g.subtotal, { currency: g.currency, signed: false })}
                    </span>
                  </div>
                </td>
              </tr>
              {g.items.map((item) => (
                <tr
                  key={item.id}
                  onClick={onItemClick ? () => onItemClick(item) : undefined}
                  className={cn(
                    "border-b border-border/60 transition-colors last:border-0 hover:bg-surface-sunken/40",
                    onItemClick && "cursor-pointer",
                  )}
                >
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-3">
                      {item.leading ?? defaultLeading(item)}
                      <div className="flex min-w-0 flex-col">
                        <span className="truncate font-semibold text-foreground">{item.title}</span>
                        {item.subtitle && (
                          <span className="truncate text-xs text-muted-foreground">{item.subtitle}</span>
                        )}
                      </div>
                    </div>
                  </td>
                  <td className="px-4 py-3 whitespace-nowrap text-muted-foreground tabular-nums">
                    {item.date.toLocaleDateString("en-US", {
                      day: "numeric",
                      month: "short",
                      year: "numeric",
                    })}
                  </td>
                  <td className="px-4 py-3">
                    {item.status && <Badge variant={statusTone(item.status)} size="sm">{item.status}</Badge>}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <AmountText value={item.amount} currency={item.currency} />
                  </td>
                </tr>
              ))}
            </tbody>
          ))}
        </table>
      </div>
    </div>
  );
}

/* ------------------------------------------------------------ wrapper */

/**
 * Lists of money, per platform:
 * - mobile:  stacked rows under month bands with subtotals
 * - desktop: a true table — sortable columns, hover states, sticky group
 *            rows, CSV export
 */
export function MoneyList({
  groups,
  onItemClick,
  platform,
  exportName,
  className,
}: {
  groups: MoneyGroup[];
  onItemClick?: (item: MoneyItem) => void;
  platform?: PlatformSetting;
  exportName?: string;
  className?: string;
}) {
  const resolved = usePlatform(platform);
  return (
    <div className={className}>
      {resolved === "mobile" ? (
        <MoneyBands groups={groups} onItemClick={onItemClick} />
      ) : (
        <MoneyTable groups={groups} onItemClick={onItemClick} exportName={exportName} />
      )}
    </div>
  );
}
