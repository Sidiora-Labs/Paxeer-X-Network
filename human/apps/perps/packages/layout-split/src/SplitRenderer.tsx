import React, { useRef, useCallback, useMemo } from "react";
import type { LayoutRendererProps } from "@paxeer-network/layout-core";
import { SplitLayoutConfigProvider } from "./SplitLayoutConfigContext";
import { useSplitPresetContext } from "./SplitPresetContext";
import { SplitNodeRenderer } from "./components/SplitNodeRenderer";
import { BREAKPOINT_KEYS } from "./constants";
import { useViewportBreakpoint } from "./hooks/useViewportBreakpoint";
import type { SplitLayoutModel, SplitLayoutNode } from "./types";
import {
  getNodeAtPath,
  getSizesFromChildren,
  sizesAreEqual,
  updateSizeAtPath,
  updateDefaultCollapsedAtPath,
} from "./utils/splitRendererUtils";

/**
 * Single tree walk: panel collapsible flags and panels that start collapsed.
 */
function collectPanelCollapseState(root: SplitLayoutNode): {
  collapsiblePanels: Map<string, boolean>;
  collapsedPanels: Set<string>;
} {
  const collapsiblePanels = new Map<string, boolean>();
  const collapsedPanels = new Set<string>();

  const traverse = (node: SplitLayoutNode) => {
    if (node.type === "panel" && node.id) {
      collapsiblePanels.set(node.id, node.collapsible ?? false);
      if (node.defaultCollapsed) {
        collapsedPanels.add(node.id);
      }
    }
    if (node.type === "split" || node.type === "sort") {
      for (const child of node.children ?? []) {
        traverse(child);
      }
    }
  };

  traverse(root);
  return { collapsiblePanels, collapsedPanels };
}

/**
 * Creates an updated layout with new sizes at the given path.
 */
function createUpdatedLayout(
  rootNode: SplitLayoutNode,
  layout: SplitLayoutModel,
  breakpoint: string,
  path: number[],
  sizes: string[],
): SplitLayoutModel {
  const updatedRoot = updateSizeAtPath(rootNode, path, sizes);
  return {
    ...layout,
    layouts: {
      ...layout.layouts,
      [breakpoint]: updatedRoot,
    },
  };
}

/**
 * Split layout renderer: picks root by current breakpoint (from viewport width),
 * renders that tree, and on size change updates only the current breakpoint's root.
 */
export function SplitRenderer(
  props: LayoutRendererProps<SplitLayoutModel>,
): React.ReactElement {
  const { layout, panels, onLayoutChange, onLayoutPersist, className, style } =
    props;

  const ctx = useSplitPresetContext();
  const containerRef = useRef<HTMLDivElement>(null);
  const presetName = ctx?.presets?.find(
    (p) => p.id === ctx?.selectedPresetId,
  )?.name;
  const breakpoint = useViewportBreakpoint({
    fallbackWidth: typeof window !== "undefined" ? window.innerWidth : 1440,
  });

  const rootNode = layout.layouts[breakpoint];

  const computeUpdatedLayout = useCallback(
    (path: number[], sizes: string[]) => {
      const nodeAtPath = getNodeAtPath(rootNode, path);
      const currentSizes =
        nodeAtPath?.type === "split"
          ? getSizesFromChildren(nodeAtPath.children)
          : undefined;
      if (sizesAreEqual(currentSizes, sizes)) {
        return null;
      }
      return createUpdatedLayout(rootNode, layout, breakpoint, path, sizes);
    },
    [layout, breakpoint, rootNode],
  );

  const handleSizeChange = useCallback(
    (path: number[], sizes: string[]) => {
      const updatedLayout = computeUpdatedLayout(path, sizes);
      if (updatedLayout) {
        onLayoutChange(updatedLayout);
      }
    },
    [computeUpdatedLayout, onLayoutChange],
  );

  const handleSizePersist = useCallback(
    (path: number[], sizes: string[]) => {
      if (!onLayoutPersist) return;
      const updatedLayout = computeUpdatedLayout(path, sizes);
      if (updatedLayout) {
        onLayoutPersist(updatedLayout);
      }
    },
    [computeUpdatedLayout, onLayoutPersist],
  );

  const { collapsiblePanels, collapsedPanels } = useMemo(
    () => collectPanelCollapseState(rootNode),
    [rootNode],
  );

  const computeUpdatedLayoutForCollapse = useCallback(
    (panelId: string, collapsed: boolean) => {
      const layouts = { ...layout.layouts };
      for (const bp of BREAKPOINT_KEYS) {
        const nodeAtBreakpoint = layouts[bp];
        if (nodeAtBreakpoint) {
          layouts[bp] = updateDefaultCollapsedAtPath(
            nodeAtBreakpoint,
            panelId,
            collapsed,
          );
        }
      }
      return { ...layout, layouts };
    },
    [layout],
  );

  const isPanelCollapsible = useCallback(
    (panelId: string) => collapsiblePanels.get(panelId) ?? false,
    [collapsiblePanels],
  );

  const togglePanelCollapse = useCallback(
    (panelId: string) => {
      const isCurrentlyCollapsed = collapsedPanels.has(panelId);
      const updatedLayout = computeUpdatedLayoutForCollapse(
        panelId,
        !isCurrentlyCollapsed,
      );
      onLayoutChange(updatedLayout);
      onLayoutPersist?.(updatedLayout);
    },
    [
      collapsedPanels,
      computeUpdatedLayoutForCollapse,
      onLayoutChange,
      onLayoutPersist,
    ],
  );

  const isPanelCollapsed = useCallback(
    (panelId: string) => collapsedPanels.has(panelId),
    [collapsedPanels],
  );

  if (panels.size === 0) {
    return (
      <div ref={containerRef} className={className} style={style}>
        {/* Empty state - no panels registered */}
      </div>
    );
  }

  return (
    <div ref={containerRef} className={className}>
      <SplitLayoutConfigProvider
        panels={panels}
        layout={layout}
        breakpoint={breakpoint}
        onLayoutChange={onLayoutChange}
        onSizeChange={handleSizeChange}
        onSizePersist={handleSizePersist}
        classNames={ctx?.classNames}
        gap={ctx?.gap}
        collapsedPanels={collapsedPanels}
        togglePanelCollapse={togglePanelCollapse}
        isPanelCollapsed={isPanelCollapsed}
        collapsiblePanels={collapsiblePanels}
        isPanelCollapsible={isPanelCollapsible}
      >
        <SplitNodeRenderer node={rootNode} path={[]} rootNode={rootNode} />
      </SplitLayoutConfigProvider>
      <div
        className="oui-fixed oui-bottom-5 oui-end-2 oui-flex oui-flex-col oui-items-end oui-text-lg oui-text-trade-loss"
        style={{ zIndex: 1000 }}
      >
        <span>{breakpoint}</span>
        {presetName && <span className="oui-text-sm">{presetName}</span>}
        <button
          onClick={() => ctx?.reset()}
          className="oui-mt-1 oui-rounded oui-bg-gray-500 oui-px-2 oui-py-1 oui-text-sm oui-text-white"
        >
          Reset
        </button>
      </div>
    </div>
  );
}
