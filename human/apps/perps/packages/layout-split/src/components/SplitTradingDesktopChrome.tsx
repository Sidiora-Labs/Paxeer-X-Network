/**
 * Split layout chrome: DnD for order-entry sortable, Flex wrapper.
 * Wraps the minimal desktop layout (Trading’s DesktopLayout); strategy is injected by the plugin.
 */
import React, { useMemo, useState } from "react";
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  DragOverlay,
  DragStartEvent,
  DragEndEvent,
} from "@dnd-kit/core";
import { restrictToVerticalAxis } from "@dnd-kit/modifiers";
import {
  arrayMove,
  SortableContext,
  sortableKeyboardCoordinates,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS, Transform } from "@dnd-kit/utilities";
import { Box, cn, Flex } from "@paxeer-network/ui";
import type { DesktopLayoutProps } from "../types";
import { SplitTradingDesktopContext } from "./SplitTradingDesktopContext";

export interface SplitTradingDesktopChromeProps extends DesktopLayoutProps {
  /** The minimal desktop layout (panels + LayoutHost) to wrap */
  children: React.ReactNode;
}

/**
 * Drop animation for drag overlay (scale + shake)
 */
function useDropAnimationConfig() {
  return useMemo(
    () => ({
      keyframes({
        transform,
      }: {
        transform: { initial: Transform; final: Transform };
      }) {
        return [
          {
            transform: CSS.Transform.toString({
              ...transform.initial,
              scaleX: 1.05,
              scaleY: 1.05,
            }),
          },
          {
            transform: CSS.Transform.toString({
              ...transform.final,
              scaleX: 1,
              scaleY: 1,
            }),
          },
        ];
      },
      sideEffects: ({
        active,
        dragOverlay,
      }: {
        active: { node: HTMLElement };
        dragOverlay: { node: HTMLElement };
      }) => {
        active.node.style.opacity = "0";
        const inner = dragOverlay.node.querySelector(".inner-content");
        if (inner) inner.classList.add("oui-animate-shake");
        return () => {
          active.node.style.opacity = "";
        };
      },
    }),
    [],
  );
}

/**
 * Wraps Trading’s minimal desktop layout with split-specific chrome:
 * DnD (order-entry sortable), Flex structure. Markets rendered by LayoutHost from rule tree.
 */
export function SplitTradingDesktopChrome(
  props: SplitTradingDesktopChromeProps,
): React.ReactElement {
  // max2XL: trading script breakpoint (max-width 1279px); split chrome used to read isSM from a local stub.
  const {
    children,
    max2XL,
    tradingViewFullScreen,
    sortableItems,
    setSortableItems,
    className,
  } = props;

  const [activeId, setActiveId] = useState<string | null>(null);

  const dropAnimationConfig = useDropAnimationConfig();
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );

  const handleDragStart = (event: DragStartEvent) => {
    setActiveId(event.active.id as string);
  };

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (active.id !== over?.id && over) {
      const oldIndex = sortableItems?.indexOf(active.id as string) ?? -1;
      const newIndex = sortableItems?.indexOf(over.id as string) ?? -1;
      if (oldIndex !== -1 && newIndex !== -1) {
        setSortableItems?.(
          arrayMove(sortableItems ?? [], oldIndex, newIndex) as string[],
        );
      }
    }
    setActiveId(null);
  };

  // const minScreenHeight = useMemo(
  //   () =>
  //     tradingViewFullScreen
  //       ? 0
  //       : orderbookMaxHeight + dataListInitialHeight + space * 4 + 56,
  //   [tradingViewFullScreen],
  // );

  return (
    <SplitTradingDesktopContext.Provider value={props}>
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragStart={handleDragStart}
        onDragEnd={handleDragEnd}
        modifiers={[restrictToVerticalAxis]}
      >
        <SortableContext
          items={sortableItems ?? []}
          strategy={verticalListSortingStrategy}
        >
          {max2XL ? (
            <Box height="100%" className={className}>
              {children}
            </Box>
          ) : (
            <Flex
              style={
                {
                  // minHeight: minScreenHeight,
                  // minWidth: 1440 - scrollBarWidth,
                }
              }
              className={cn(
                className,
                "oui-flex-1 oui-justify-start oui-overflow-hidden",
                tradingViewFullScreen &&
                  "oui-relative oui-h-[calc(100vh-80px)] oui-w-screen oui-overflow-hidden !oui-p-0",
              )}
              width="100%"
              p={2}
              gap={2}
              itemAlign="stretch"
              direction="column"
            >
              {children}
            </Flex>
          )}
        </SortableContext>
        <DragOverlay dropAnimation={dropAnimationConfig}>
          {/* DragOverlay children must be ReactNode (not a render prop in @dnd-kit/core v6). */}
          {activeId ? (
            <div className="oui-pointer-events-none oui-opacity-80">
              id: {activeId}
            </div>
          ) : null}
          {/* <OrderEntryDragOverlayContent
            activeId={activeId}
            showPositionIcon={props.showPositionIcon}
            symbol={props.symbol}
            disableFeatures={props.disableFeatures}
            navigateToPortfolio={props.navigateToPortfolio}
            isFirstTimeDeposit={props.isFirstTimeDeposit}
          /> */}
        </DragOverlay>
      </DndContext>
    </SplitTradingDesktopContext.Provider>
  );
}
