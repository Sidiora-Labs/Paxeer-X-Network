import React, { PropsWithChildren, useCallback, useMemo } from "react";
import { cn } from "@paxeer-network/ui";
import type { SplitLayoutClassNames } from "../types";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "./ui/resizable";

/**
 * Converts sizes array (e.g. ["40%", "auto", "30%"]) to defaultLayout object for ResizablePanelGroup.
 * Ensures preset sizes are applied to child panels (including nested split/sort).
 * Skips "fixed" entries (they are not wrapped in ResizablePanel).
 */
function sizesToDefaultLayout(
  sizes: string[] | undefined,
  count: number,
): Record<string, number> | undefined {
  if (!sizes || sizes.length !== count) return undefined;
  const result: Record<string, number> = {};
  let fixedTotal = 0;
  const autoIndices: number[] = [];
  for (let i = 0; i < count; i++) {
    const s = sizes[i];
    if (s === "fixed") continue; // fixed panels are not in Group
    const num = parseFloat(String(s).replace(/%/g, ""));
    if (!isNaN(num)) {
      result[String(i)] = Math.round(num);
      fixedTotal += num;
    } else {
      autoIndices.push(i);
    }
  }
  if (autoIndices.length > 0) {
    const remainder = Math.max(0, 100 - fixedTotal);
    const autoSize = remainder / autoIndices.length;
    for (const i of autoIndices) {
      result[String(i)] = Math.round(autoSize);
    }
  }
  return Object.keys(result).length > 0 ? result : undefined;
}

/** Per-panel constraints for minSize, maxSize (both "%" format), and fixed. */
export interface PanelConstraints {
  minSize?: string;
  maxSize?: string;
  /** When true, panel cannot be resized (ResizablePanel disabled) */
  disabled?: boolean;
}

/**
 * Props for SplitLayout component.
 * Uses Resizable (react-resizable-panels); no dependency on @uiw/react-split.
 */
export interface SplitLayoutProps extends PropsWithChildren {
  /** Layout orientation: horizontal or vertical */
  orientation: "horizontal" | "vertical";
  /** Optional initial sizes per panel (e.g. ["40%", "60%"], "auto" for equal share, "fixed" for non-resizable content) */
  sizes?: string[];
  /** Optional per-panel constraints (minSize, maxSize in "%", disabled for fixed) */
  panelConstraints?: PanelConstraints[];
  /** Callback when panel sizes change (full array of percentage strings) */
  onSizeChange?: (sizes: string[]) => void;
  /** Optional class name for the group container */
  className?: string;
  /** Optional inline style for the group container */
  style?: React.CSSProperties;
  /** Optional classNames for panel group, panel, and handle (from plugin options). */
  classNames?: SplitLayoutClassNames;
  /** Optional gap between panels in px (total; handle margin = gap/2 each side). Undefined preserves default 3px per side. */
  gap?: number;
}

/**
 * SplitLineBar-equivalent className for ResizableHandle.
 * Keeps existing handle style: transparent default, primary-light + shadow on hover/active/focus.
 * Thickness and orientation are delegated to the underlying ResizableHandle implementation.
 */
const SPLIT_HANDLE_BASE_CLASSNAME = [
  "!oui-transition-none",
  "!oui-shadow-none",
  "!oui-bg-transparent",
  "hover:!oui-bg-primary-light",
  "hover:!oui-shadow-[0px_0px_2px_0px]",
  "hover:!oui-shadow-primary-light/80",
  "active:!oui-bg-primary-light",
  "active:!oui-shadow-[0px_0px_2px_0px]",
  "active:!oui-shadow-primary-light/80",
  // "focus:!oui-bg-primary-light",
  // "focus:!oui-shadow-[0px_0px_4px_0px]",
  // "focus:!oui-shadow-primary-light/80",
];

export function SplitLayout({
  orientation,
  sizes,
  panelConstraints,
  onSizeChange,
  children,
  className,
  style,
  classNames,
  gap,
}: SplitLayoutProps): React.ReactElement {
  const childArray = React.Children.toArray(children);
  const count = childArray.length;

  /** Apply preset sizes to Group so child panels (including nested split/sort) get correct initial layout. */
  const defaultLayout = useMemo(
    () => sizesToDefaultLayout(sizes, count),
    [sizes, count],
  );

  const handleLayoutChanged = useCallback(
    (layout: { [id: string]: number }) => {
      const panelIds = Array.from({ length: count }, (_, i) => String(i));
      const sizesAsStrings = panelIds.map((id, index) => {
        const value = layout[id];
        if (typeof value === "number") {
          return `${Math.round(value)}%`;
        }
        const originalSize = sizes?.[index];
        return originalSize ?? "auto";
      });
      onSizeChange?.(sizesAsStrings);
    },
    [onSizeChange, count, sizes],
  );

  if (count === 0) return <></>;

  /** Handle margin: gap = total px between panels (gap/2 each side). Undefined → 3px per side (backward compat). */
  const marginPx = gap != null ? gap * 2 : 4;
  const handleStyle: React.CSSProperties =
    orientation === "horizontal"
      ? { width: 2, marginLeft: marginPx, marginRight: marginPx }
      : { height: 2, marginTop: marginPx, marginBottom: marginPx };

  return (
    <ResizablePanelGroup
      orientation={orientation}
      defaultLayout={defaultLayout}
      onLayoutChanged={handleLayoutChanged}
      className={cn(className, classNames?.panelGroup)}
      style={style}
    >
      {childArray.map((child, index) => {
        const constraints = panelConstraints?.[index];
        const size = sizes?.[index];
        const isFixed = size === "fixed";
        const nextIsFixed = sizes?.[index + 1] === "fixed";

        return (
          <React.Fragment key={`panel-${index}`}>
            {isFixed ? (
              child
            ) : (
              <ResizablePanel
                id={String(index)}
                defaultSize={size}
                minSize={constraints?.minSize}
                maxSize={constraints?.maxSize}
                disabled={constraints?.disabled}
                className={classNames?.panel}
              >
                {child}
              </ResizablePanel>
            )}
            {index < count - 1 && !isFixed && !nextIsFixed && (
              <ResizableHandle
                className={cn(SPLIT_HANDLE_BASE_CLASSNAME, classNames?.handle)}
                style={handleStyle}
              />
            )}
          </React.Fragment>
        );
      })}
    </ResizablePanelGroup>
  );
}

SplitLayout.displayName = "SplitLayout";
