import type {
  GridLayoutModel,
  GridLayoutItem,
  GridLayoutRule,
  GridLayoutItemSpec,
  BreakpointKey,
  GridConfig,
} from "../types";
import { DEFAULT_GRID_LAYOUT_RULE } from "./defaultPresets";

const BREAKPOINT_ORDER: BreakpointKey[] = ["lg", "md", "sm", "xs"];

/** Default grid configuration shared by layout utils and plugin config. */
export const DEFAULT_GRID_CONFIG: GridConfig = {
  rowHeight: 30,
  margin: [4, 4],
  containerPadding: [0, 0],
  compactType: "vertical",
  isDraggable: true,
  isResizable: true,
};

/** Merge user grid config with defaults and optional preset rowHeight. */
export function mergeGridConfig(
  userConfig: GridConfig | undefined,
  presetRowHeight?: number,
): GridConfig {
  return {
    ...DEFAULT_GRID_CONFIG,
    ...(presetRowHeight !== undefined && { rowHeight: presetRowHeight }),
    ...userConfig,
  };
}

export const DEFAULT_BREAKPOINTS = {
  lg: 1680,
  md: 1440,
  sm: 1280,
  xs: 768,
};

export const DEFAULT_COLS = {
  lg: 24,
  md: 24,
  sm: 24,
  xs: 24,
};

function specsToItems(
  specs: GridLayoutItemSpec[],
  panelIds: string[],
): GridLayoutItem[] {
  const idSet = new Set(panelIds);
  return specs
    .filter((spec) => idSet.has(spec.panelId))
    .map((spec) => ({
      i: spec.panelId,
      panelId: spec.panelId,
      x: spec.x,
      y: spec.y,
      w: spec.w,
      h: spec.h,
      minW: spec.minW,
      minH: spec.minH,
      maxW: spec.maxW,
      maxH: spec.maxH,
      isDraggable: spec.isDraggable,
      isResizable: spec.isResizable,
      resizeHandles: spec.resizeHandles,
      static: spec.static,
      className: spec.className,
      style: spec.style,
      autoHeight: spec.autoHeight,
    }));
}

function buildLayoutsFromRule(
  rule: GridLayoutRule,
  panelIds: string[],
): GridLayoutModel["layouts"] {
  const result: GridLayoutModel["layouts"] = {};
  const fallbackSpecs = rule.lg ?? [];
  for (const bp of BREAKPOINT_ORDER) {
    const specs = rule[bp] ?? fallbackSpecs;
    result[bp] = specsToItems(specs, panelIds);
  }
  return result;
}

/** Build grid layout from panelIds; optional rule (missing breakpoints fall back to lg), else default lg rule from defaultPresets. */
export function createDefaultGridLayout(
  panelIds: string[],
  rule?: GridLayoutRule,
  rowHeight?: number,
): GridLayoutModel {
  const effectiveRule = rule ?? DEFAULT_GRID_LAYOUT_RULE;
  const layouts = buildLayoutsFromRule(effectiveRule, panelIds);

  const effectiveConfig = mergeGridConfig(undefined, rowHeight);

  return {
    layouts,
    breakpoints: DEFAULT_BREAKPOINTS,
    cols: DEFAULT_COLS,
    // For the default layout we always use vertical compaction.
    compactType: "vertical",
    isDraggable: effectiveConfig.isDraggable,
    isResizable: effectiveConfig.isResizable,
    margin: effectiveConfig.margin,
    containerPadding: effectiveConfig.containerPadding,
    rowHeight: effectiveConfig.rowHeight,
  };
}

export function serializeGridLayout(layout: GridLayoutModel): string {
  return JSON.stringify(layout);
}

export function deserializeGridLayout(json: string): GridLayoutModel {
  try {
    const parsed = JSON.parse(json);
    if (!parsed.layouts) {
      throw new Error("Invalid grid layout: missing layouts");
    }
    return parsed as GridLayoutModel;
  } catch (error) {
    throw new Error(`Failed to deserialize grid layout: ${error}`);
  }
}
