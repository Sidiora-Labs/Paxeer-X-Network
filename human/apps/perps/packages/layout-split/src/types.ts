import type React from "react";

/**
 * Same props as `Trading.Layout.Desktop` in @paxeer-network/trading-next so interceptors
 * and `<Original />` stay assignable (structural duplicates break on `ComponentType` variance).
 */
export type { DesktopLayoutProps } from "@paxeer-network/trading-next";

/** Breakpoint keys for responsive split layout (viewport-based). */
export type SplitLayoutBreakpointKey = "lg" | "md" | "sm" | "xs";

/**
 * Constraints shared by panel and split-child nodes (size per child).
 * minSize/maxSize use "%" format, e.g. "20%", "80%".
 */
export interface SplitLayoutChildConstraints {
  /** Default size, e.g. "40%", "auto" */
  size?: string;
  /** Minimum size in "%" format, e.g. "20%" */
  minSize?: string;
  /** Maximum size in "%" format, e.g. "80%" */
  maxSize?: string;
  /** When true, panel cannot be resized (maps to ResizablePanel disabled) */
  disabled?: boolean;
  /** When true, panel can be collapsed/expanded by user */
  collapsible?: boolean;
  /** When true, panel is initially collapsed */
  defaultCollapsed?: boolean;
  /** Panel title displayed in collapsible header */
  title?: string;
}

/**
 * Split layout node: single panel, split container, or sort container.
 * Panel identifier is kept as `id` with no conversion.
 */
export type SplitLayoutNode =
  | ({
      type: "panel";
      /** Panel ID */
      id: string;
      /** Optional class name applied to the outer panel wrapper. */
      className?: string;
      /** Optional inline style applied to the outer panel wrapper. */
      style?: React.CSSProperties;
    } & SplitLayoutChildConstraints)
  | ({
      type: "split";
      orientation: "horizontal" | "vertical";
      /** Children; each has size/minSize/maxSize/disabled. */
      children: SplitLayoutNode[];
      /** Size of this split when used as a child (e.g. "30%", "auto"). Used by parent SplitLayout for ResizablePanel. */
      size?: string;
      /** Optional class name applied to the ResizablePanelGroup wrapper. */
      className?: string;
      /** Optional inline style applied to the ResizablePanelGroup wrapper. */
      style?: React.CSSProperties;
    } & Partial<SplitLayoutChildConstraints>)
  | ({
      type: "sort";
      /** Layout orientation for the sortable list (vertical = list, horizontal = row). */
      orientation: "horizontal" | "vertical";
      /** Sortable panel children; order can be changed via drag-and-drop. */
      children: SplitLayoutNode[];
      /** Size of this sort when used as a child (e.g. "30%", "auto"). Used by parent SplitLayout for ResizablePanel. */
      size?: string;
      /** Optional class name applied to the sort container wrapper. */
      className?: string;
      /** Optional inline style applied to the sort container wrapper. */
      style?: React.CSSProperties;
    } & Partial<SplitLayoutChildConstraints>);

/** Breakpoint width map (min width for each key). */
export interface SplitLayoutBreakpoints {
  lg: number;
  md: number;
  sm: number;
  xs: number;
}

/** Plugin classNames applied to panel group, panel, and handle. */
export type SplitLayoutClassNames = {
  panelGroup?: string;
  panel?: string;
  handle?: string;
};

/**
 * Split layout model for backward compatibility.
 * In the fixed layout approach, this is less relevant but kept for plugin API compatibility.
 */
export interface SplitLayoutModel {
  layouts: {
    lg: SplitLayoutNode;
    md: SplitLayoutNode;
    sm: SplitLayoutNode;
    xs: SplitLayoutNode;
  };
  breakpoints: SplitLayoutBreakpoints;
}
