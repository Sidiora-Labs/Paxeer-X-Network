"use client";

import * as React from "react";
import { ChevronDown, ListFilter } from "lucide-react";
import { cn } from "../lib/utils";
import { usePlatform, type PlatformSetting } from "../lib/platform";
import { Sheet, SheetHeader, SheetBody, SheetFooter } from "./sheet";
import { Popover, PopoverTrigger, PopoverContent } from "./popover";
import { OptionList } from "./option-list";
import {
  CalendarRangePicker,
  dayPickerClassNames,
  dayPickerModifiersClassNames,
  type DateRange,
} from "./calendar-range-picker";
import { DayPicker } from "react-day-picker";
import { Button } from "./button";

export interface FilterDef {
  id: string;
  label: string;
  type: "options" | "date-range";
  options?: { value: string; label: string }[];
}

export type FilterValues = Record<string, string | DateRange | undefined>;

export function isFilterActive(v: FilterValues[string]): boolean {
  if (!v) return false;
  if (typeof v === "string") return v.length > 0 && v !== "all";
  return Boolean(v.from);
}

function filterSummary(def: FilterDef, v: FilterValues[string]): string | null {
  if (!isFilterActive(v)) return null;
  if (def.type === "options" && typeof v === "string") {
    return def.options?.find((o) => o.value === v)?.label ?? null;
  }
  return "Custom range";
}

/**
 * Filters, per platform:
 * - mobile:  a Filter button opens a sheet with every filter stacked,
 *            Clear + Apply footer (Apply commits a draft state)
 * - desktop: each filter is a chip; its editor is a popover anchored to
 *            the chip (option lists or a calendar range picker)
 */
export function FilterBar({
  filters,
  values,
  onChange,
  platform,
  portalContainer,
  className,
}: {
  filters: FilterDef[];
  values: FilterValues;
  onChange: (values: FilterValues) => void;
  platform?: PlatformSetting;
  portalContainer?: HTMLElement | null;
  className?: string;
}) {
  const resolved = usePlatform(platform);
  const [sheetOpen, setSheetOpen] = React.useState(false);
  const [draft, setDraft] = React.useState<FilterValues>(values);

  const appliedCount = filters.filter((f) => isFilterActive(values[f.id])).length;

  const openSheet = () => {
    setDraft(values);
    setSheetOpen(true);
  };

  if (resolved === "mobile") {
    return (
      <>
        <button
          type="button"
          onClick={openSheet}
          className={cn(
            "flex h-11 items-center gap-2 rounded-full border border-border bg-surface px-4 text-sm font-semibold text-foreground-secondary transition-colors hover:bg-surface-sunken/60",
            className,
          )}
        >
          <ListFilter className="size-4" aria-hidden />
          Filter
          {appliedCount > 0 && (
            <span className="inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-accent px-1.5 text-xs font-bold text-accent-foreground">
              {appliedCount}
            </span>
          )}
        </button>

        <Sheet open={sheetOpen} onOpenChange={setSheetOpen} portalContainer={portalContainer}>
          <SheetHeader title="Filter" />
          <SheetBody className="flex flex-col gap-6">
            {filters.map((def) => (
              <section key={def.id} className="flex flex-col gap-1">
                <h4 className="pb-1 text-[15px] font-bold text-foreground">{def.label}</h4>
                {def.type === "options" ? (
                  <OptionList
                    aria-label={def.label}
                    items={def.options ?? []}
                    value={(draft[def.id] as string) ?? def.options?.[0]?.value ?? ""}
                    onValueChange={(v) => setDraft((d) => ({ ...d, [def.id]: v }))}
                  />
                ) : (
                  <div className="rounded-md border border-border p-2">
                    <DayPicker
                      mode="range"
                      numberOfMonths={1}
                      selected={(draft[def.id] as DateRange) ?? undefined}
                      onSelect={(r) => setDraft((d) => ({ ...d, [def.id]: r }))}
                      classNames={{ ...dayPickerClassNames, root: "w-full text-sm text-foreground" }}
                      modifiersClassNames={dayPickerModifiersClassNames}
                    />
                  </div>
                )}
              </section>
            ))}
          </SheetBody>
          <SheetFooter>
            <Button
              variant="secondary"
              size="lg"
              onClick={() => {
                const cleared: FilterValues = {};
                filters.forEach((f) => (cleared[f.id] = f.type === "options" ? "all" : undefined));
                setDraft(cleared);
                onChange(cleared);
                setSheetOpen(false);
              }}
            >
              Clear
            </Button>
            <Button
              size="lg"
              onClick={() => {
                onChange(draft);
                setSheetOpen(false);
              }}
            >
              Apply
            </Button>
          </SheetFooter>
        </Sheet>
      </>
    );
  }

  // Desktop: chips + anchored popovers
  return (
    <div className={cn("flex flex-wrap items-center gap-2", className)}>
      {filters.map((def) => {
        const v = values[def.id];
        const summary = filterSummary(def, v);
        const active = isFilterActive(v);

        if (def.type === "date-range") {
          return (
            <CalendarRangePicker
              key={def.id}
              value={(v as DateRange) ?? undefined}
              onChange={(r) => onChange({ ...values, [def.id]: r })}
              placeholder={def.label}
            />
          );
        }

        return (
          <Popover key={def.id}>
            <PopoverTrigger asChild>
              <button
                type="button"
                className={cn(
                  "flex h-10 items-center gap-2 rounded-full border px-4 text-sm font-semibold transition-colors outline-none focus-visible:ring-2 focus-visible:ring-accent/30",
                  active
                    ? "border-accent/40 bg-accent-soft text-accent-strong"
                    : "border-border bg-surface text-foreground-secondary hover:bg-surface-sunken/60",
                )}
              >
                {summary ?? def.label}
                <ChevronDown className="size-4 opacity-60" aria-hidden />
              </button>
            </PopoverTrigger>
            <PopoverContent className="w-[240px] p-2">
              <OptionList
                aria-label={def.label}
                items={def.options ?? []}
                value={(v as string) ?? "all"}
                onValueChange={(nv) => onChange({ ...values, [def.id]: nv })}
              />
            </PopoverContent>
          </Popover>
        );
      })}
    </div>
  );
}
