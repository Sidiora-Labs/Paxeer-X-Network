import React, { useCallback } from "react";
import { RestrictToVerticalAxis } from "@dnd-kit/abstract/modifiers";
import { DragDropProvider } from "@dnd-kit/react";
import { useSortable, isSortable } from "@dnd-kit/react/sortable";
import { cn } from "@paxeer-network/ui";
import { useSplitLayoutConfig } from "../SplitLayoutConfigContext";
import { useSplitPresetContext } from "../SplitPresetContext";
import type { SplitLayoutNode } from "../types";
import {
  getSortableIdForChild,
  updateOrderAtPath,
} from "../utils/splitLayoutUtils";
import { SplitNodeRenderer } from "./SplitNodeRenderer";

const DEFAULT_SHOW_INDICATOR = true;

/**
 * Drag handle icon used for sortable panels.
 */
function IndicatorIcon(
  props: React.SVGProps<SVGSVGElement>,
): React.ReactElement {
  return (
    <svg
      width="10"
      height="16"
      viewBox="0 0 10 16"
      fill="currentColor"
      xmlns="http://www.w3.org/2000/svg"
      {...props}
    >
      <rect x="2" y="2" width="6" height="2" rx="1" />
      <rect x="2" y="7" width="6" height="2" rx="1" />
      <rect x="2" y="12" width="6" height="2" rx="1" />
    </svg>
  );
}

interface SortableSortChildProps {
  id: string;
  child: SplitLayoutNode;
  path: number[];
  rootNode: SplitLayoutNode;
  showIndicator: boolean;
  index: number;
}

/**
 * Wraps a split layout child in a sortable wrapper using the new dnd-kit React API.
 */
function SortableSortChild({
  id,
  child,
  showIndicator,
  path,
  rootNode,
  index,
}: SortableSortChildProps): React.ReactElement {
  const { ref, handleRef, isDragging } = useSortable({
    id,
    index,
  });

  const showHandle = showIndicator && child.type === "panel";

  return (
    <div
      ref={ref}
      className={cn("oui-relative", isDragging && "oui-opacity-50")}
    >
      {showHandle && (
        <button
          className="oui-absolute oui-end-0 oui-top-4 oui-cursor-move oui-py-1"
          style={{ touchAction: "none" }}
          // Connect drag handle explicitly so dragging from the handle starts the sortable interaction.
          ref={handleRef}
        >
          <IndicatorIcon
            className={cn(
              "oui-text-base-contrast-20 hover:oui-text-base-contrast-80",
            )}
          />
        </button>
      )}
      <SplitNodeRenderer node={child} path={path} rootNode={rootNode} />
    </div>
  );
}

/** Props for sort container renderer (node is narrowed to sort type). */
export interface SortNodeRendererProps {
  node: Extract<SplitLayoutNode, { type: "sort" }>;
  path: number[];
  rootNode: SplitLayoutNode;
  classNames?: {
    panel?: string;
  };
}

/**
 * Renders a sort container using the latest dnd-kit React API.
 */
export function SortNodeRenderer({
  node,
  path,
  rootNode,
  classNames,
}: SortNodeRendererProps): React.ReactElement {
  const { onLayoutChange, layout, breakpoint } = useSplitLayoutConfig();
  const { orientation, children } = node;
  const ctx = useSplitPresetContext();
  const showIndicator = ctx?.showIndicator ?? DEFAULT_SHOW_INDICATOR;
  const items = children.map((child: SplitLayoutNode, index: number) =>
    getSortableIdForChild(child, path, index),
  );
  const isVertical = orientation === "vertical";

  const handleDragEnd = useCallback(
    (event: unknown) => {
      const anyEvent = event as {
        operation?: { source: unknown };
        canceled?: boolean;
      };

      if (!anyEvent.operation || anyEvent.canceled) return;

      const { source } = anyEvent.operation as {
        source: unknown;
      };

      if (!isSortable(source)) return;

      const { initialIndex, index } = source;
      if (initialIndex === index) return;

      const newOrder = [...(children as SplitLayoutNode[])];
      const [removed] = newOrder.splice(initialIndex, 1);
      newOrder.splice(index, 0, removed);

      const updatedRoot = updateOrderAtPath(rootNode, path, newOrder);

      onLayoutChange({
        ...layout,
        layouts: {
          ...layout.layouts,
          [breakpoint]: updatedRoot,
        },
      });
    },
    [children, path, rootNode, breakpoint, layout, onLayoutChange],
  );

  return (
    <div>
      <DragDropProvider
        onDragEnd={handleDragEnd}
        modifiers={(defaults) => [...defaults, RestrictToVerticalAxis]}
      >
        <div
          className={cn(
            isVertical
              ? "oui-flex oui-w-full oui-flex-col oui-gap-2"
              : "oui-flex oui-w-full oui-flex-row oui-gap-2",
            node.className,
            classNames?.panel,
          )}
          style={node.style}
        >
          {children.map((child: SplitLayoutNode, index: number) => (
            <SortableSortChild
              key={items[index]}
              id={items[index]}
              child={child}
              showIndicator={showIndicator}
              path={[...path, index]}
              rootNode={rootNode}
              index={index}
            />
          ))}
        </div>
      </DragDropProvider>
    </div>
  );
}
