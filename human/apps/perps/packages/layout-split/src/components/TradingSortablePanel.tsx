/**
 * TradingSortablePanel - Drag-and-drop sortable panel for order-entry column.
 *
 * Ported from packages/trading-next/src/components/desktop/layout/sortablePanel.tsx.
 * Uses @dnd-kit/sortable (already a layout-split dep). Renders a drag-indicator
 * button when showIndicator is true and a placeholder while the item is being dragged.
 */
import React, {
  type FC,
  type PropsWithChildren,
  type SVGProps,
  useEffect,
  useRef,
} from "react";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Box, cn } from "@paxeer-network/ui";

export type TradingSortablePanelProps = {
  /** Unique DnD id for this panel (matches SortableContext items array). */
  id: string;
  /** Additional class name for the outer box. */
  className?: string;
  /**
   * When true, shows the vertical drag-handle indicator button.
   * Typically set when the user can trade (canTrade && !isFirstTimeDeposit).
   */
  showIndicator: boolean;
  /**
   * When true, renders the drag-overlay variant (slightly scaled down).
   * Pass this when rendering inside DragOverlay.
   */
  dragOverlay?: boolean;
};

/**
 * Three-dot vertical drag handle icon.
 */
const IndicatorIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
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

/**
 * Sortable panel that wraps order-entry widgets (margin, assets, order form).
 * While actively dragging, renders a fixed-size placeholder with a hatched border
 * so the layout doesn't collapse; the real content is rendered in DragOverlay.
 */
export const TradingSortablePanel: FC<
  PropsWithChildren<TradingSortablePanelProps>
> = (props) => {
  const { showIndicator, dragOverlay } = props;
  const nodeRef = useRef<HTMLDivElement>(null);
  const [dimensions, setDimensions] = React.useState<{
    width: number;
    height: number;
  } | null>(null);

  const {
    setNodeRef,
    attributes,
    listeners,
    transform,
    transition,
    isDragging,
    setActivatorNodeRef,
  } = useSortable({ id: props.id });

  /** Capture element dimensions when drag starts so the placeholder can match. */
  useEffect(() => {
    if (isDragging && nodeRef.current && !dimensions) {
      const rect = nodeRef.current.getBoundingClientRect();
      setDimensions({ width: rect.width, height: rect.height });
    } else if (!isDragging && dimensions) {
      setDimensions(null);
    }
  }, [isDragging, dimensions]);

  /** Combine sortable ref with our measurement ref. */
  const combinedRef = (node: HTMLDivElement | null) => {
    setNodeRef(node);
    (nodeRef as React.MutableRefObject<HTMLDivElement | null>).current = node;
  };

  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
  };

  /** While dragging render a dashed placeholder that preserves space. */
  if (isDragging && dimensions && !dragOverlay) {
    return (
      <Box
        intensity={900}
        r="2xl"
        p={3}
        className="oui-relative"
        style={{
          ...style,
          width: dimensions.width,
          height: dimensions.height,
          minWidth: dimensions.width,
          minHeight: dimensions.height,
          maxWidth: dimensions.width,
          maxHeight: dimensions.height,
          border: "1px solid rgb(var(--oui-color-primary))",
          backgroundImage: `repeating-linear-gradient(135deg, rgb(var(--oui-color-base-6)) 0px, rgb(var(--oui-color-base-6)) 4px, transparent 4px, transparent 8px)`,
        }}
      />
    );
  }

  return (
    <Box
      intensity={900}
      r="2xl"
      p={3}
      width="100%"
      className={cn("oui-relative", props.className)}
      ref={combinedRef}
      style={style}
    >
      <div
        className={cn(
          "inner-content oui-transition-transform",
          dragOverlay && "oui-scale-95",
        )}
      >
        {props.children}
      </div>
      {showIndicator && (
        <button
          {...attributes}
          {...listeners}
          className="oui-absolute oui-end-0 oui-top-4 oui-cursor-move oui-py-1"
          style={{ touchAction: "none" }}
          ref={setActivatorNodeRef}
        >
          <IndicatorIcon className="oui-text-base-contrast-20 hover:oui-text-base-contrast-80" />
        </button>
      )}
    </Box>
  );
};
