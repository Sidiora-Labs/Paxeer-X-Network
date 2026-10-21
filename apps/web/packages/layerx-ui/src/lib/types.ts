import type * as React from "react";

/** One row of money movement (transaction, earning, agent spend…). */
export interface MoneyItem {
  id: string;
  title: string;
  subtitle?: string;
  /** Signed amount in major units. */
  amount: number;
  currency?: string;
  status?: string; // "Settled" | "Pending" | "Failed"
  /** Used for month bands + sort. */
  date: Date;
  /** Leading visual: Avatar, IconTile, flag… */
  leading?: React.ReactNode;
}

/** A month band with its rows and subtotal. */
export interface MoneyGroup {
  id: string;
  label: string; // "February 2025"
  subtotal: number;
  currency?: string;
  items: MoneyItem[];
}

export interface NotificationItem {
  id: string;
  title: string;
  body: React.ReactNode;
  /** When it happened — drives recency segments + labels. */
  date: Date;
  icon?: React.ReactNode;
  read?: boolean;
  href?: string;
}

/** Bucket a notification lands in for the recency segments. */
export type RecencySegment = "today" | "week" | "month";

export function recencyOf(date: Date, now: Date = new Date()): RecencySegment {
  const days = (now.getTime() - date.getTime()) / 86400000;
  if (days < 1) return "today";
  if (days < 7) return "week";
  return "month";
}
