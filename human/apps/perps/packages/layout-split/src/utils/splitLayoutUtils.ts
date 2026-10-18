import {
  SPLIT_BREAKPOINT_ORDER,
  DEFAULT_SPLIT_BREAKPOINTS,
} from "../constants";
import type {
  SplitLayoutNode,
  SplitLayoutModel,
  SplitLayoutBreakpointKey,
} from "../types";

/**
 * Returns a stable sortable id for a child node (panel uses id, others use path).
 * Never returns empty string to avoid React duplicate key warnings when id is missing.
 */
export function getSortableIdForChild(
  child: SplitLayoutNode,
  path: number[],
  index: number,
): string {
  if (child.type === "panel" && child.id) {
    return child.id;
  }
  return `sortable-${path.join("-")}-${index}`;
}

/**
 * Reorders children at the given path and returns the updated root.
 * Used when user drags to reorder within a sort container.
 */
export function updateOrderAtPath(
  node: SplitLayoutNode,
  path: number[],
  newOrder: SplitLayoutNode[],
): SplitLayoutNode {
  if (node.type === "panel") return node;
  if (path.length === 0) {
    return { ...node, children: newOrder };
  }
  const [childIndex, ...restPath] = path;
  const updatedChildren = [...node.children];
  if (childIndex < updatedChildren.length) {
    updatedChildren[childIndex] = updateOrderAtPath(
      updatedChildren[childIndex],
      restPath,
      newOrder,
    );
  }
  return { ...node, children: updatedChildren };
}

/**
 * Creates default split layout for given panel IDs (strategy.defaultLayout).
 * This is a fallback when getInitialLayout is not provided.
 * Same simple horizontal tree for all breakpoints.
 *
 * @param panelIds - Panel IDs to include
 * @returns Split layout model with layouts and breakpoints
 */
export function createDefaultSplitLayout(panelIds: string[]): SplitLayoutModel {
  if (panelIds.length === 0) {
    throw new Error(
      "Cannot create default split layout: no panel IDs provided",
    );
  }

  let singleRoot: SplitLayoutNode;
  if (panelIds.length === 1) {
    singleRoot = { type: "panel", id: panelIds[0] };
  } else {
    singleRoot = {
      type: "split",
      orientation: "horizontal",
      children: panelIds.map((panelId) => ({
        type: "panel" as const,
        id: panelId,
      })),
    };
  }

  const layouts = {
    lg: singleRoot,
    md: singleRoot,
    sm: singleRoot,
    xs: singleRoot,
  };

  return {
    layouts,
    breakpoints: DEFAULT_SPLIT_BREAKPOINTS,
  };
}

/**
 * Rule-based layout types for preset rules.
 */
interface SplitLayoutRule {
  lg?: SplitLayoutNode;
  md?: SplitLayoutNode;
  sm?: SplitLayoutNode;
  xs?: SplitLayoutNode;
}

/**
 * Builds runtime SplitLayoutNode for one breakpoint from a rule.
 * Falls back to md when the breakpoint has no rule tree.
 */
export function buildSplitLayoutFromRule(
  rule: SplitLayoutRule,
  breakpointKey: SplitLayoutBreakpointKey,
): SplitLayoutNode {
  const tree = rule[breakpointKey] ?? rule.md;
  if (!tree) {
    throw new Error(
      `Split layout rule must define at least 'md'; missing for ${breakpointKey}`,
    );
  }
  return tree as SplitLayoutNode;
}

/**
 * Builds full responsive SplitLayoutModel from a rule (all breakpoints).
 * Missing rule entries for a breakpoint use the lg tree.
 */
export function createDefaultSplitLayoutFromRule(
  rule: SplitLayoutRule,
): SplitLayoutModel {
  const layouts = {} as SplitLayoutModel["layouts"];
  for (const bp of SPLIT_BREAKPOINT_ORDER) {
    layouts[bp] = buildSplitLayoutFromRule(rule, bp);
  }

  return {
    layouts,
    breakpoints: DEFAULT_SPLIT_BREAKPOINTS,
  };
}

/**
 * Creates a default trading split layout.
 * Uses the first built-in preset rule for structure.
 */
export function createTradingSplitLayout(): SplitLayoutModel {
  const presets = getDefaultSplitPresets();
  const preset = presets[0];
  if (!preset) throw new Error("Split layout: no default presets");
  return createDefaultSplitLayoutFromRule(preset.rule);
}

/**
 * Trading split layout preset.
 */
export interface SplitLayoutPreset {
  id: string;
  name: string;
  rule: SplitLayoutRule;
}

/**
 * Default split layout presets for trading.
 * These are the built-in presets that define the trading layout structure.
 */
export function getDefaultSplitPresets(): SplitLayoutPreset[] {
  // These presets are loaded dynamically to avoid circular dependencies
  // In production, this would come from the trading package
  return [];
}

/** Checks that value is a valid orientation. */
function isValidOrientation(v: unknown): v is "horizontal" | "vertical" {
  return v === "horizontal" || v === "vertical";
}

/** Validates a container node (split or sort): orientation + children array. */
function validateContainerNode(n: Record<string, unknown>): boolean {
  return (
    isValidOrientation(n.orientation) &&
    Array.isArray(n.children) &&
    (n.children as unknown[]).every((c) => validateSplitLayoutNode(c))
  );
}

/**
 * Validates a runtime split layout node recursively.
 */
function validateSplitLayoutNode(node: unknown): node is SplitLayoutNode {
  if (!node || typeof node !== "object") return false;
  const n = node as Record<string, unknown>;

  if (n.type === "panel") {
    return (
      typeof n.id === "string" &&
      n.id.length > 0 &&
      (n.size === undefined || typeof n.size === "string") &&
      (n.minSize === undefined || typeof n.minSize === "string") &&
      (n.maxSize === undefined || typeof n.maxSize === "string") &&
      (n.disabled === undefined || typeof n.disabled === "boolean") &&
      (n.className === undefined || typeof n.className === "string") &&
      (n.style === undefined || typeof n.style === "object")
    );
  }
  if (n.type === "split" || n.type === "sort") return validateContainerNode(n);
  return false;
}

/**
 * Validates that an object has required layouts and breakpoints for the model shape.
 */
function validateSplitLayoutModel(parsed: unknown): parsed is SplitLayoutModel {
  if (!parsed || typeof parsed !== "object") return false;
  const p = parsed as Record<string, unknown>;
  const layoutsSource = p.layouts ?? p.roots;
  if (!layoutsSource || typeof layoutsSource !== "object") return false;
  if (!p.breakpoints || typeof p.breakpoints !== "object") return false;
  const layouts = layoutsSource as Record<string, unknown>;
  const bp = p.breakpoints as Record<string, unknown>;
  for (const k of SPLIT_BREAKPOINT_ORDER) {
    if (!validateSplitLayoutNode(layouts[k]) || typeof bp[k] !== "number") {
      return false;
    }
  }
  return true;
}

/**
 * Serializes split layout model to JSON (layouts + breakpoints only).
 */
export function serializeSplitLayout(layout: SplitLayoutModel): string {
  return JSON.stringify(layout);
}

/**
 * Deserializes JSON to split layout model; validates layouts and breakpoints.
 * No conversion: panel nodes keep `id` as stored.
 */
export function deserializeSplitLayout(json: string): SplitLayoutModel {
  try {
    const parsed = JSON.parse(json) as unknown;
    if (!validateSplitLayoutModel(parsed)) {
      throw new Error(
        "Invalid split layout: must have layouts (lg,md,sm,xs) and breakpoints",
      );
    }
    const layout = parsed as SplitLayoutModel & {
      roots?: SplitLayoutModel["layouts"];
    };
    const layoutsSource = layout.layouts ?? layout.roots;
    const layouts = {} as SplitLayoutModel["layouts"];
    for (const bp of SPLIT_BREAKPOINT_ORDER) {
      layouts[bp] = layoutsSource[bp];
    }
    // eslint-disable-next-line @typescript-eslint/no-unused-vars -- omit roots when returning layouts
    const { roots: _roots, ...rest } = layout;
    return { ...rest, layouts };
  } catch (error) {
    if (error instanceof Error) {
      throw new Error(`Failed to deserialize split layout: ${error.message}`);
    }
    throw new Error(`Failed to deserialize split layout: ${error}`);
  }
}
