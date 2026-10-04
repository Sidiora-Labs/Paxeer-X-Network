"use client";

import * as React from "react";
import * as RadioGroup from "@radix-ui/react-radio-group";
import { cn } from "../lib/utils";

export interface OptionListItem {
  value: string;
  label: React.ReactNode;
  description?: React.ReactNode;
  disabled?: boolean;
}

/**
 * Right-aligned radio rows — the filter sheet option list
 * ("All time / Today / Last 7 days…") from the design set.
 */
export function OptionList({
  items,
  value,
  onValueChange,
  className,
  "aria-label": ariaLabel,
}: {
  items: OptionListItem[];
  value: string;
  onValueChange: (value: string) => void;
  className?: string;
  "aria-label"?: string;
}) {
  return (
    <RadioGroup.Root
      value={value}
      onValueChange={onValueChange}
      className={cn("flex flex-col divide-y divide-border/70", className)}
      aria-label={ariaLabel}
    >
      {items.map((item) => {
        const checked = item.value === value;
        return (
          <RadioGroup.Item
            key={item.value}
            value={item.value}
            disabled={item.disabled === true}
            className="group flex w-full cursor-pointer items-center justify-between gap-3 py-4 text-left outline-none disabled:cursor-not-allowed disabled:opacity-50"
          >
            <span className="flex min-w-0 flex-col gap-0.5">
              <span className="text-[15px] font-medium text-foreground">{item.label}</span>
              {item.description && (
                <span className="text-[13px] text-muted-foreground">{item.description}</span>
              )}
            </span>
            <span
              className={cn(
                "inline-flex size-[22px] shrink-0 items-center justify-center rounded-full border-2 transition-colors",
                checked ? "border-accent" : "border-border-strong group-hover:border-faint-foreground",
              )}
              aria-hidden
            >
              {checked && <span className="size-3 rounded-full bg-accent" />}
            </span>
          </RadioGroup.Item>
        );
      })}
    </RadioGroup.Root>
  );
}
