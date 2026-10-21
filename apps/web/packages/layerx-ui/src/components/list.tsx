"use client";

import * as React from "react";
import { ChevronRight } from "lucide-react";
import { cn } from "../lib/utils";

/* ------------------------------------------------------------------ List */

/** Vertical stack of rows with hairline dividers between them. */
export function List({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn("flex flex-col divide-y divide-border/70", className)}
      role="list"
      {...props}
    />
  );
}

/* ------------------------------------------------------------- IconTile */

/** Soft rounded-square icon container (quick actions, list leading icons). */
export function IconTile({
  className,
  tone = "neutral",
  shape = "square",
  ...props
}: React.HTMLAttributes<HTMLSpanElement> & {
  tone?: "neutral" | "accent" | "success" | "destructive";
  shape?: "square" | "circle";
}) {
  const tones = {
    neutral: "bg-surface-sunken text-foreground-secondary",
    accent: "bg-accent-soft text-accent",
    success: "bg-success-soft text-success",
    destructive: "bg-destructive-soft text-destructive",
  } as const;
  return (
    <span
      className={cn(
        "inline-flex size-11 shrink-0 items-center justify-center [&_svg]:size-5",
        shape === "square" ? "rounded-md" : "rounded-full",
        tones[tone],
        className,
      )}
      {...props}
    />
  );
}

/* -------------------------------------------------------------- ListItem */

export interface ListItemProps extends Omit<React.HTMLAttributes<HTMLDivElement>, "title"> {
  /** Avatar, IconTile, flag, or any leading node. */
  leading?: React.ReactNode;
  title: React.ReactNode;
  subtitle?: React.ReactNode;
  /** Amount, Badge, Switch, chevron… right-aligned. */
  trailing?: React.ReactNode;
  /** Adds a chevron and pointer cursor. */
  navigates?: boolean;
  /** Text under the trailing node (e.g. timestamp). */
  trailingCaption?: React.ReactNode;
}

export function ListItem({
  className,
  leading,
  title,
  subtitle,
  trailing,
  trailingCaption,
  navigates,
  onClick,
  ...props
}: ListItemProps) {
  const interactive = Boolean(onClick) || navigates;
  return (
    <div
      role="listitem"
      tabIndex={interactive ? 0 : undefined}
      onClick={onClick}
      onKeyDown={
        onClick
          ? (e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                onClick(e as unknown as React.MouseEvent<HTMLDivElement>);
              }
            }
          : undefined
      }
      className={cn(
        "flex w-full items-center gap-3 py-3.5 text-left",
        interactive && "cursor-pointer transition-colors hover:bg-surface-sunken/40 -mx-2 px-2 rounded-md",
        className,
      )}
      {...props}
    >
      {leading}
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate text-[15px] font-semibold text-foreground">{title}</span>
        {subtitle && (
          <span className="truncate text-[13px] text-muted-foreground">{subtitle}</span>
        )}
      </span>
      {(trailing || navigates) && (
        <span className="flex shrink-0 flex-col items-end gap-0.5">
          <span className="flex items-center gap-1.5">
            {trailing}
            {navigates && <ChevronRight className="size-4 text-faint-foreground" aria-hidden />}
          </span>
          {trailingCaption && (
            <span className="text-xs text-faint-foreground">{trailingCaption}</span>
          )}
        </span>
      )}
    </div>
  );
}

/* -------------------------------------------------------- SectionHeader */

export function SectionHeader({
  title,
  action,
  className,
}: {
  title: React.ReactNode;
  /** e.g. a "View all" chip button. */
  action?: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex items-center justify-between gap-3", className)}>
      <h3 className="text-[17px] font-bold text-foreground">{title}</h3>
      {action}
    </div>
  );
}

/** Small pill button used for "View all" actions in section headers. */
export function ViewAllChip({
  className,
  children = "View all",
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      type="button"
      className={cn(
        "inline-flex h-8 items-center rounded-full bg-surface border border-border px-3.5 text-[13px] font-semibold text-foreground-secondary transition-colors hover:bg-surface-sunken/60",
        className,
      )}
      {...props}
    >
      {children}
    </button>
  );
}

/* -------------------------------------------------------------- Divider */

export function Divider({ className }: { className?: string }) {
  return <hr className={cn("border-0 border-t border-border", className)} />;
}
