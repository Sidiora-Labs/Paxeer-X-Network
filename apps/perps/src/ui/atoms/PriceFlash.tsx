import React, { useEffect, useRef } from "react";
import { cn } from "@/ui/utils/cn";

/**
 * Wraps a price value display and briefly flashes green/red when the numeric
 * value changes. Ignores zero-to-nonzero transitions (initial load).
 *
 * Consumed via the [data-flash="up|down"] CSS rule in globals.css.
 */
export function PriceFlash({
  value,
  children,
  className,
  as: Tag = "span",
  durationMs = 420,
}: {
  value: number;
  children: React.ReactNode;
  className?: string;
  as?: keyof JSX.IntrinsicElements;
  durationMs?: number;
}) {
  const ref = useRef<HTMLElement | null>(null);
  const prevRef = useRef<number>(value);

  useEffect(() => {
    const prev = prevRef.current;
    prevRef.current = value;
    const el = ref.current;
    if (!el) return;
    if (prev === value) return;
    if (prev <= 0 || value <= 0) return; // skip initial load / loss-of-data
    const dir = value > prev ? "up" : "down";
    el.setAttribute("data-flash", dir);
    // Force reflow so consecutive same-direction changes restart the animation
    void (el as HTMLElement).offsetWidth;
    const id = window.setTimeout(() => {
      el.removeAttribute("data-flash");
    }, durationMs);
    return () => window.clearTimeout(id);
  }, [value, durationMs]);

  // @ts-expect-error — dynamic tag
  return <Tag ref={ref} className={cn("inline-block rounded px-0.5", className)}>{children}</Tag>;
}
