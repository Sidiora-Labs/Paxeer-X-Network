import React, { type FC, PropsWithChildren, SVGProps } from "react";
import { useRef, useEffect } from "react";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Box, cn } from "@paxeer-network/ui";

type SortablePanelProps = {
  id: string;
  className?: string;
  showIndicator: boolean;
  dragOverlay?: boolean;
};

export const SortablePanel: FC<PropsWithChildren<SortablePanelProps>> = (
  props,
) => {
  const { showIndicator, dragOverlay } = props;
  const nodeRef = useRef<HTMLDivElement>(null);
  const [dimensions, setDimensions] = React.useState<{
    width: number;
    height: number;
  } | null>(null);

  // useSortable hook with custom configuration to prevent dimension changes
  const sortableResult = useSortable({
    id: props.id,
  });

  const {
    setNodeRef,
    attributes,
    listeners,
    transform,
    transition,
    isDragging,
    setActivatorNodeRef,
  } = sortableResult;

  // Store original dimensions when dragging starts
  useEffect(() => {
    if (isDragging && nodeRef.current && !dimensions) {
      const rect = nodeRef.current.getBoundingClientRect();
      // Store dimensions for placeholder
      setDimensions({
        width: rect.width,
        height: rect.height,
      });
    } else if (!isDragging && dimensions) {
      // Reset dimensions when dragging ends
      setDimensions(null);
    }
  }, [isDragging, dimensions]);

  // Combine refs to use both sortable ref and our dimension ref
  const combinedRef = (node: HTMLDivElement | null) => {
    setNodeRef(node);
    // Use a callback ref to properly set the node reference
    (nodeRef as React.MutableRefObject<HTMLDivElement | null>).current = node;
  };

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    // Ensure the item maintains its box model during drag
    // boxSizing: "border-box" as const,
  };

  // When dragging, render a placeholder with fixed dimensions
  if (isDragging && dimensions && !dragOverlay) {
    return (
      <Box
        intensity={900}
        r="2xl"
        p={3}
        className={cn("oui-relative")}
        // ref={combinedRef}
        style={{
          ...style,
          width: dimensions.width,
          height: dimensions.height,
          minWidth: dimensions.width,
          minHeight: dimensions.height,
          maxWidth: dimensions.width,
          maxHeight: dimensions.height,
          // Placeholder styling with subtle visual feedback
          border: "1px solid rgb(var(--oui-color-primary))",
          backgroundImage: `repeating-linear-gradient(135deg, rgb(var(--oui-color-base-6)) 0px, rgb(var(--oui-color-base-6)) 4px, transparent 4px, transparent 8px)`,
        }}
      ></Box>
    );
  }

  return (
    <Box
      intensity={900}
      r="2xl"
      p={3}
      width="100%"
      className={cn(
        "oui-relative",
        props.className,
        // dragOverlay && "oui-scale-105",
      )}
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
          <IndicatorIcon
            className={cn(
              "oui-text-base-contrast-20 hover:oui-text-base-contrast-80",
            )}
          />
        </button>
      )}
    </Box>
  );
};

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
