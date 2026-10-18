import type { LayoutItem, CompactType } from "react-grid-layout";
import type { LayoutModel } from "@paxeer-network/layout-core";

export type BreakpointKey = "lg" | "md" | "sm" | "xs" | "xxs";
export interface GridConfig {
  breakpoints?: { lg: number; md: number; sm: number; xs: number };
  cols?: { lg: number; md: number; sm: number; xs: number };
  rowHeight?: number;
  margin?: [number, number];
  containerPadding?: [number, number];
  compactType?: CompactType;
  isDraggable?: boolean;
  isResizable?: boolean;
}

export interface LayoutGridPluginOptions {
  presets?: GridLayoutPreset[];
  grid?: GridConfig;
  persistLayout?: boolean;
  getInitialLayout?: () => GridLayoutModel;
  classNames?: {
    item?: string;
  };
}

export type GridLayoutItemSpec = Omit<LayoutItem, "i"> & {
  panelId: string;
  className?: string;
  style?: React.CSSProperties;
  /** When true, item height follows content (measured via ResizeObserver and pushed to layout). */
  autoHeight?: boolean;
  /**
   * Whether this grid item is collapsible.
   *
   * For collapsible items, `w` is treated as the expanded width
   * and `minW` is treated as the collapsed width. The actual
   * collapsed / expanded state is derived from the current layout
   * width at render time (e.g. when dragged down to `minW`).
   */
  collapsible?: boolean;
};

export interface GridLayoutRule {
  lg?: GridLayoutItemSpec[];
  md?: GridLayoutItemSpec[];
  sm?: GridLayoutItemSpec[];
  xs?: GridLayoutItemSpec[];
  xxs?: GridLayoutItemSpec[];
}

export interface GridLayoutPreset {
  id: string;
  name: string;
  rule: GridLayoutRule;
  rowHeight?: number;
}

export interface GridLayoutItem extends LayoutItem {
  panelId: string;
  className?: string;
  style?: React.CSSProperties;
  /** When true, item height follows content (measured via ResizeObserver and pushed to layout). */
  autoHeight?: boolean;
  /**
   * Whether this grid item is collapsible.
   *
   * For collapsible items, `w` is treated as the expanded width
   * and `minW` is treated as the collapsed width. The actual
   * collapsed / expanded state is derived from the current layout
   * width at render time (e.g. when dragged down to `minW`).
   */
  collapsible?: boolean;
}

export interface GridLayoutModel extends LayoutModel {
  layouts: {
    lg?: GridLayoutItem[];
    md?: GridLayoutItem[];
    sm?: GridLayoutItem[];
    xs?: GridLayoutItem[];
    xxs?: GridLayoutItem[];
  };
  breakpoints?: { lg: number; md: number; sm: number; xs: number };
  cols?: { lg: number; md: number; sm: number; xs: number };
  compactType?: "vertical" | "horizontal" | null;
  isDraggable?: boolean;
  isResizable?: boolean;
  margin?: [number, number];
  containerPadding?: [number, number];
  rowHeight?: number;
}
