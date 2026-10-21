"use client";

import * as React from "react";
import { ArrowLeft, Bell } from "lucide-react";
import { cn } from "../lib/utils";
import { usePlatform, type PlatformSetting } from "../lib/platform";
import { formatRecency } from "../lib/format";
import { recencyOf, type NotificationItem, type RecencySegment } from "../lib/types";
import { SegmentedControl } from "./segmented-control";
import { IconTile, List, ListItem } from "./list";
import { IconButton } from "./button";
import { Popover, PopoverContent, PopoverTrigger } from "./popover";
import { EmptyState } from "./empty-state";

const SEGMENTS: { value: RecencySegment; label: string }[] = [
  { value: "today", label: "Today" },
  { value: "week", label: "This week" },
  { value: "month", label: "This month" },
];

function NotificationRow({
  item,
  onClick,
}: {
  item: NotificationItem;
  onClick?: (item: NotificationItem) => void;
}) {
  return (
    <ListItem
      leading={
        <IconTile shape="circle" className="size-10 [&_svg]:size-4">
          {item.icon ?? <Bell />}
        </IconTile>
      }
      title={
        <span className="flex items-center gap-2">
          <span className="truncate">{item.title}</span>
          {!item.read && <span className="size-2 shrink-0 rounded-full bg-accent" aria-label="Unread" />}
        </span>
      }
      subtitle={<span className="line-clamp-2 whitespace-normal">{item.body}</span>}
      trailing={<span className="text-xs text-faint-foreground">{formatRecency(item.date)}</span>}
      onClick={onClick ? () => onClick(item) : undefined}
      className="items-start"
    />
  );
}

function filterBySegment(items: NotificationItem[], segment: RecencySegment) {
  return items.filter((n) => recencyOf(n.date) === segment);
}

/* ------------------------------------------- mobile: pushed screen */

export function NotificationsScreen({
  items,
  onBack,
  onItemClick,
  segment: segmentProp,
  onSegmentChange,
  className,
}: {
  items: NotificationItem[];
  onBack?: () => void;
  onItemClick?: (item: NotificationItem) => void;
  segment?: RecencySegment;
  onSegmentChange?: (s: RecencySegment) => void;
  className?: string;
}) {
  const [internal, setInternal] = React.useState<RecencySegment>("today");
  const segment = segmentProp ?? internal;
  const setSegment = onSegmentChange ?? setInternal;
  const shown = filterBySegment(items, segment);

  return (
    <div className={cn("flex h-full flex-col bg-background", className)}>
      <header className="relative flex items-center justify-center border-b border-border bg-surface px-4 pt-[max(0.875rem,env(safe-area-inset-top))] pb-3.5">
        <IconButton
          variant="outline"
          size="sm"
          onClick={onBack}
          aria-label="Back"
          className="absolute left-4"
        >
          <ArrowLeft />
        </IconButton>
        <h2 className="text-[17px] font-bold text-foreground">Notifications</h2>
      </header>
      <div className="p-4 pb-2">
        <SegmentedControl
          aria-label="Recency"
          options={SEGMENTS}
          value={segment}
          onValueChange={(v) => setSegment(v as RecencySegment)}
        />
      </div>
      <div className="lx-scroll flex-1 overflow-y-auto px-4 pb-[max(1.5rem,env(safe-area-inset-bottom))]">
        {shown.length > 0 ? (
          <List>
            {shown.map((n) => (
              <NotificationRow key={n.id} item={n} onClick={onItemClick} />
            ))}
          </List>
        ) : (
          <EmptyState
            className="mt-6"
            icon={<Bell />}
            title="Nothing here yet"
            description="You're all caught up for this period."
          />
        )}
      </div>
    </div>
  );
}

/* -------------------------------------- desktop: bell popover + archive */

export function BellPopover({
  items,
  onItemClick,
  onViewAll,
  unreadCount,
}: {
  items: NotificationItem[];
  onItemClick?: (item: NotificationItem) => void;
  onViewAll?: () => void;
  unreadCount?: number;
}) {
  const unread = unreadCount ?? items.filter((n) => !n.read).length;
  const recent = [...items].sort((a, b) => b.date.getTime() - a.date.getTime()).slice(0, 5);

  return (
    <Popover>
      <PopoverTrigger asChild>
        <span className="relative inline-flex">
          <IconButton variant="outline" size="sm" aria-label="Notifications">
            <Bell />
          </IconButton>
          {unread > 0 && (
            <span className="pointer-events-none absolute -top-0.5 -right-0.5 inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-destructive px-1 text-[10px] font-bold text-destructive-foreground">
              {unread}
            </span>
          )}
        </span>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-[380px] p-0">
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <span className="text-sm font-bold text-foreground">Notifications</span>
          {unread > 0 && (
            <span className="text-xs font-semibold text-muted-foreground">{unread} unread</span>
          )}
        </div>
        <div className="lx-scroll max-h-[360px] overflow-y-auto px-2 py-1">
          {recent.length > 0 ? (
            <List className="divide-border/50">
              {recent.map((n) => (
                <NotificationRow key={n.id} item={n} onClick={onItemClick} />
              ))}
            </List>
          ) : (
            <p className="py-8 text-center text-sm text-muted-foreground">You're all caught up.</p>
          )}
        </div>
        <div className="border-t border-border p-2">
          <button
            type="button"
            onClick={onViewAll}
            className="flex w-full items-center justify-center rounded-md py-2 text-sm font-semibold text-accent transition-colors hover:bg-accent-soft"
          >
            View all notifications
          </button>
        </div>
      </PopoverContent>
    </Popover>
  );
}

/** Full archive page (desktop counterpart to the mobile pushed screen). */
export function NotificationsArchive({
  items,
  onItemClick,
  segment: segmentProp,
  onSegmentChange,
  className,
}: {
  items: NotificationItem[];
  onItemClick?: (item: NotificationItem) => void;
  segment?: RecencySegment;
  onSegmentChange?: (s: RecencySegment) => void;
  className?: string;
}) {
  const [internal, setInternal] = React.useState<RecencySegment>("today");
  const segment = segmentProp ?? internal;
  const setSegment = onSegmentChange ?? setInternal;
  const shown = filterBySegment(items, segment);

  return (
    <div className={cn("mx-auto flex max-w-[720px] flex-col gap-4", className)}>
      <div className="flex items-center justify-between gap-4">
        <h2 className="text-xl font-bold text-foreground">Notifications</h2>
        <SegmentedControl
          aria-label="Recency"
          size="sm"
          className="w-[320px]"
          options={SEGMENTS}
          value={segment}
          onValueChange={(v) => setSegment(v as RecencySegment)}
        />
      </div>
      <div className="rounded-lg border border-border bg-surface px-5 py-2">
        {shown.length > 0 ? (
          <List>
            {shown.map((n) => (
              <NotificationRow key={n.id} item={n} onClick={onItemClick} />
            ))}
          </List>
        ) : (
          <EmptyState
            icon={<Bell />}
            title="Nothing here yet"
            description="You're all caught up for this period."
            className="my-4"
          />
        )}
      </div>
    </div>
  );
}
