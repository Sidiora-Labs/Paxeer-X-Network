import React, { useMemo, useCallback, useEffect, useRef } from "react";
import {
  GridLayout,
  useContainerWidth,
  getCompactor,
  type Layout,
  type LayoutItem,
  type CompactType,
} from "react-grid-layout";
import type { LayoutRendererProps } from "@paxeer-network/layout-core";
import { cn, Flex, Text } from "@paxeer-network/ui";
import type { GridLayoutModel, GridLayoutItem, BreakpointKey } from "./types";
import { DEFAULT_BREAKPOINTS, DEFAULT_COLS } from "./utils/gridLayoutUtils";

type ResponsiveLayouts = Partial<Record<string, Layout>>;

/** GridLayoutItem[] -> react-grid-layout Layout */
function toReactGridLayouts(
  layouts: GridLayoutModel["layouts"],
): ResponsiveLayouts {
  const result: Record<string, LayoutItem[]> = {};

  (Object.keys(layouts) as BreakpointKey[]).forEach((breakpoint) => {
    const items = layouts[breakpoint];
    if (items) {
      result[breakpoint] = items.map((item) => ({
        i: item.i,
        x: item.x,
        y: item.y,
        w: item.w,
        h: item.h,
        minW: item.minW,
        minH: item.minH,
        maxW: item.maxW,
        maxH: item.maxH,
        static: item.static,
        isDraggable: item.isDraggable,
        isResizable: item.isResizable,
        resizeHandles: item.resizeHandles,
      }));
    }
  });

  return result as ResponsiveLayouts;
}

/** Responsive grid layout renderer (react-grid-layout). */
export function GridRenderer(
  props: LayoutRendererProps<GridLayoutModel>,
): React.ReactElement {
  const { layout, panels, onLayoutChange, onLayoutPersist, className, style } =
    props;
  const { width, containerRef, mounted } = useContainerWidth();

  // Native layout state belongs to LayoutHost. Resolve the active breakpoint
  // from the measured container and commit RGL state when interaction ends.
  const breakpoint = useMemo(() => {
    const thresholds = Object.entries(
      layout.breakpoints ?? DEFAULT_BREAKPOINTS,
    ).sort((left, right) => right[1] - left[1]);
    return (thresholds.find(([, minimum]) => width >= minimum)?.[0] ??
      "xs") as BreakpointKey;
  }, [layout.breakpoints, width]);
  const currentLayout = useMemo(() => {
    const layouts = toReactGridLayouts(layout.layouts);
    return layouts[breakpoint] ?? layouts.lg ?? [];
  }, [breakpoint, layout.layouts]);
  const cols =
    layout.cols?.[breakpoint as keyof typeof DEFAULT_COLS] ??
    DEFAULT_COLS[breakpoint as keyof typeof DEFAULT_COLS] ??
    24;
  const handlePersistLayout = useCallback(
    (
      newLayout: Layout,
      _previousItem?: LayoutItem | null,
      activeItem?: LayoutItem | null,
    ) => {
      const originalItems = layout.layouts[breakpoint] ?? [];
      const items = newLayout.map((item) => {
        const original = originalItems.find((value) => value.i === item.i);
        const current =
          activeItem?.i === item.i ? { ...item, ...activeItem } : item;
        return {
          ...original,
          ...current,
          panelId: original?.panelId ?? item.i,
        } as GridLayoutItem;
      });
      const updated = {
        ...layout,
        layouts: { ...layout.layouts, [breakpoint]: items },
      };
      onLayoutChange(updated);
      onLayoutPersist?.(updated);
    },
    [breakpoint, layout, onLayoutChange, onLayoutPersist],
  );

  // Chart iframes otherwise consume mouseup when a panel crosses the chart.
  const framePointers = useRef(new Map<HTMLIFrameElement, string>());
  const beginInteraction = useCallback(() => {
    for (const frame of document.querySelectorAll("iframe")) {
      if (!framePointers.current.has(frame))
        framePointers.current.set(frame, frame.style.pointerEvents);
      frame.style.pointerEvents = "none";
    }
  }, []);
  const restoreFrames = useCallback(() => {
    for (const [frame, value] of framePointers.current)
      frame.style.pointerEvents = value;
    framePointers.current.clear();
  }, []);
  useEffect(() => {
    window.addEventListener("blur", restoreFrames);
    window.addEventListener("mouseup", restoreFrames);
    return () => {
      restoreFrames();
      window.removeEventListener("blur", restoreFrames);
      window.removeEventListener("mouseup", restoreFrames);
    };
  }, [restoreFrames]);
  const endInteraction = useCallback(
    (
      next: Layout,
      previous?: LayoutItem | null,
      active?: LayoutItem | null,
    ) => {
      restoreFrames();
      handlePersistLayout(next, previous, active);
    },
    [handlePersistLayout, restoreFrames],
  );

  const layoutItems = useMemo(() => {
    const breakpoints: BreakpointKey[] = ["lg", "md", "sm", "xs", "xxs"];
    for (const bp of breakpoints) {
      const items = layout.layouts[bp];
      if (items?.length) return items;
    }
    return [];
  }, [layout.layouts]);

  const children = useMemo(
    () =>
      layoutItems.map((config) => {
        const panelId = config.panelId;
        const panelWrapper = panels.get(panelId);
        const panel = panelWrapper?.node;
        if (!panel) {
          console.warn(`Panel ${panelId} not found in registry`);
          return null;
        }

        /** Resolve title from panel registration props (if any). */
        const title = panelWrapper?.props?.title as
          | string
          | React.ReactNode
          | undefined;

        /** Determine whether this item should participate in collapse logic. */
        const isCollapsible = config.collapsible === true;

        /**
         * Derive collapsed state from current layout width.
         *
         * - For collapsible items, `w` is the expanded width and `minW` is the
         *   collapsed width.
         * - When the user drags the item so that its current width is at or
         *   below `minW`, we treat it as collapsed.
         */
        const layoutItem = currentLayout?.find(
          (item) => item.i === config.i,
        ) as LayoutItem | undefined;

        let collapsed = false;
        // console.log(">>>>>>>>>layoutItem", layoutItem);
        if (
          isCollapsible &&
          layoutItem &&
          typeof config.minW === "number" &&
          Number.isFinite(config.minW)
        ) {
          collapsed = layoutItem.w <= config.minW;
        }

        /** Body height adjusts when a title header is present. */
        const bodyClassName =
          typeof title !== "undefined" && title !== null
            ? "oui-h-[calc(100%_-_44px)]"
            : "oui-h-full";

        return (
          <div
            key={panelId}
            className={cn(
              "oui-layout-grid-item oui-relative oui-size-full oui-overflow-hidden oui-rounded oui-bg-base-9",
              config.className,
            )}
            style={config.style}
          >
            {title && (
              <Flex
                className="oui-text-base-contrast-36"
                justify="between"
                width="100%"
                px={3}
                pt={3}
                pb={2}
              >
                <Text size="base" intensity={80}>
                  {title}
                </Text>
              </Flex>
            )}

            <div className={cn("oui-collapsible-content", bodyClassName)}>
              {React.isValidElement<{ collapsed?: boolean }>(panel)
                ? React.cloneElement(panel, {
                    /** Pass collapsed state down so panel can adjust its own UI. */
                    collapsed,
                  })
                : panel}
            </div>
          </div>
        );
      }),
    [currentLayout, layoutItems, panels],
  );

  if (panels.size === 0) {
    return (
      <div
        ref={containerRef as React.RefObject<HTMLDivElement>}
        className={className}
        style={style}
      ></div>
    );
  }

  if (!mounted) {
    return (
      <div
        ref={containerRef as React.RefObject<HTMLDivElement>}
        className={className}
        style={style}
      />
    );
  }

  return (
    <div
      ref={containerRef as React.RefObject<HTMLDivElement>}
      className={cn("oui-grid-layout-container oui-p-1", className)}
      style={style}
    >
      <GridLayout
        width={width}
        layout={currentLayout}
        gridConfig={{
          cols: cols,
          rowHeight: layout.rowHeight ?? 30,
          margin: layout.margin || [4, 4],
          containerPadding: layout.containerPadding || [0, 0],
        }}
        compactor={getCompactor(
          layout.compactType as CompactType,
          false,
          layout.compactType === null,
        )}
        dragConfig={{
          enabled: layout.isDraggable !== false,
        }}
        resizeConfig={{
          enabled: layout.isResizable !== false,
        }}
        onDragStart={beginInteraction}
        onDragStop={endInteraction}
        onResizeStart={beginInteraction}
        onResizeStop={endInteraction}
      >
        {children}
      </GridLayout>
    </div>
  );
}
