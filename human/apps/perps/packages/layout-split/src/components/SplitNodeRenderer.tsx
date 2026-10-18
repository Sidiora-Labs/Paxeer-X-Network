import React from "react";
import { cn } from "@paxeer-network/ui";
import { useSplitLayoutConfig } from "../SplitLayoutConfigContext";
import type { SplitLayoutNode } from "../types";
import { CollapsiblePanel } from "./CollapsiblePanel";
import { SortNodeRenderer } from "./SortNodeRenderer";
import { SplitLayout } from "./SplitLayout";

/** Shared props for recursively rendering a split layout node tree. */
export interface SplitNodeRendererProps {
  node: SplitLayoutNode;
  path: number[];
  rootNode: SplitLayoutNode;
}

export function SplitNodeRenderer({
  node,
  path,
  rootNode,
}: SplitNodeRendererProps): React.ReactElement | null {
  const {
    panels,
    onSizeChange,
    onSizePersist,
    classNames,
    gap,
    isPanelCollapsed,
    togglePanelCollapse,
    isPanelCollapsible,
  } = useSplitLayoutConfig();

  if (node.type === "panel") {
    const panelWrapper = panels.get(node.id);
    const panel = panelWrapper?.node;
    if (!panel) {
      /** Placeholder when panel not in registry (empty or unknown id). */
      return (
        <div
          className={cn(
            "oui-text-base-contrast-40 oui-rounded-2xl oui-bg-base-9 oui-p-3 oui-text-xs",
            node.size !== "fixed" && "oui-size-full",
            classNames?.panel,
            node.className,
          )}
          style={node.style}
          data-panel-id={node.id}
        >
          {node.id ? `Panel not found: ${node.id}` : "Panel not found"}
        </div>
      );
    }
    const title =
      node.title ?? (panelWrapper?.props?.title as string | undefined);

    // Use CollapsiblePanel only when collapsible is explicitly set
    if (typeof node.collapsible !== "undefined") {
      return (
        <CollapsiblePanel
          title={title}
          collapsible={node.collapsible || isPanelCollapsible(node.id)}
          collapsed={isPanelCollapsed(node.id)}
          onToggle={() => togglePanelCollapse(node.id)}
          minSize={node.minSize}
          maxSize={node.maxSize}
          className={cn(
            node.size !== "fixed" && "oui-size-full",
            classNames?.panel,
            node.className,
          )}
          style={node.style}
          data-panel-id={node.id}
        >
          {panel}
        </CollapsiblePanel>
      );
    }

    // Non-collapsible panel: render as regular div
    return (
      <div
        className={cn(
          "oui-overflow-hidden oui-rounded-2xl oui-bg-base-9",
          node.size !== "fixed" && "oui-size-full",
          classNames?.panel,
          node.className,
        )}
        style={node.style}
        data-panel-id={node.id}
      >
        {panel}
      </div>
    );
  }

  if (node.type === "sort") {
    return (
      <SortNodeRenderer
        node={node as Extract<SplitLayoutNode, { type: "sort" }>}
        path={path}
        rootNode={rootNode}
        classNames={classNames}
      />
    );
  }

  // type === "split"
  const { orientation, children } = node;
  const sizes = children.map((child) => child.size ?? "auto");
  const panelConstraints = children.map((child) => ({
    // Nested split/sort nodes need minSize to prevent collapse (lib default 0% causes inner area to collapse)
    minSize:
      child.minSize ??
      (child.type === "split" || child.type === "sort" ? "10%" : undefined),
    maxSize: child.maxSize,
    disabled: child.disabled,
  }));

  return (
    <SplitLayout
      orientation={orientation}
      sizes={sizes}
      panelConstraints={panelConstraints}
      onSizeChange={(sizesAsStrings) => {
        onSizeChange(path, sizesAsStrings);
        onSizePersist?.(path, sizesAsStrings);
      }}
      classNames={classNames}
      gap={gap}
      className={node.className}
      style={node.style}
    >
      {children.map((child, index) => (
        <SplitNodeRenderer
          key={`child-${index}`}
          node={child}
          path={[...path, index]}
          rootNode={rootNode}
        />
      ))}
    </SplitLayout>
  );
}
