import React, { useEffect, useMemo, useState } from "react";
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
  Modifier,
  type ClientRect,
} from "@dnd-kit/core";
import { restrictToVerticalAxis } from "@dnd-kit/modifiers";
import {
  arrayMove,
  SortableContext,
  sortableKeyboardCoordinates,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS, Transform } from "@dnd-kit/utilities";
import {
  useBadgeBySymbol,
  useGetRwaSymbolOpenStatus,
  useLocalStorage,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import {
  SideMarketsWidget,
  SymbolInfoBarFullWidget,
  HorizontalMarketsWidget,
} from "@paxeer-network/markets";
import {
  OrderEntrySortKeys,
  TradingviewFullscreenKey,
} from "@paxeer-network/types";
import {
  Box,
  Button,
  ChevronLeftIcon,
  ChevronRightIcon,
  cn,
  Flex,
  Text,
} from "@paxeer-network/ui";
import { OrderEntryWidget } from "@paxeer-network/ui-order-entry";
import { TradingviewWidget } from "@paxeer-network/ui-tradingview";
import { DepositStatusWidget } from "@paxeer-network/ui-transfer";
import { CollapseIcon, ExpandIcon } from "../../components/base/icons";
import { SortablePanel } from "../../components/desktop/layout/sortablePanel";
import { SplitLayout } from "../../components/desktop/layout/splitLayout";
import { showRwaOutsideMarketHoursNotify } from "../../components/desktop/notify/rwaNotification";
import { useShowRwaCountdown } from "../../hooks/useShowRwaCountdown";
import {
  dataListInitialHeight,
  getOffsetSizeNum,
  TradingState,
} from "./trading.script";
import {
  scrollBarWidth,
  topBarHeight,
  bottomBarHeight,
  space,
  orderEntryMinWidth,
  orderEntryMaxWidth,
  orderbookMinWidth,
  orderbookMaxWidth,
  orderbookMinHeight,
  orderbookMaxHeight,
  tradindviewMinHeight,
  tradingViewMinWidth,
  dataListMaxHeight,
} from "./trading.script";

const LazyRiskRateWidget = React.lazy(() =>
  import("../../components/desktop/riskRate").then((mod) => {
    return {
      default: mod.RiskRateWidget,
    };
  }),
);

const LazyAssetViewWidget = React.lazy(() =>
  import("../../components/desktop/assetView").then((mod) => {
    return {
      default: mod.AssetViewWidget,
    };
  }),
);

const LazyDataListWidget = React.lazy(() =>
  import("../../components/desktop/dataList").then((mod) => {
    return {
      default: mod.DataListWidget,
    };
  }),
);

const LazySwitchLayout = React.lazy(() =>
  import("../../components/desktop/layout/switchLayout").then((mod) => {
    return {
      default: mod.SwitchLayout,
    };
  }),
);

const LazyOrderBookAndTradesWidget = React.lazy(() =>
  import("../../components/desktop/orderBookAndTrades").then((mod) => {
    return {
      default: mod.OrderBookAndTradesWidget,
    };
  }),
);

export type DesktopLayoutProps = TradingState & {
  className?: string;
};

const scaleModifier: Modifier = ({
  transform,
  draggingNodeRect,
}: {
  transform: Transform;
  draggingNodeRect: ClientRect | null;
}) => {
  if (draggingNodeRect) {
    return {
      ...transform,
      scaleX: 2.05,
      scaleY: 2.05,
    };
  }
  return transform;
};

function getClampedPanelWidth(
  size: string | null | undefined,
  minWidth: number,
  maxWidth: number,
) {
  const width = Number.parseFloat(size ?? "");

  if (!Number.isFinite(width)) {
    return minWidth;
  }

  return Math.min(Math.max(width, minWidth), maxWidth);
}

export const DesktopLayout: React.FC<DesktopLayoutProps> = (props) => {
  const {
    resizeable,
    panelSize,
    onPanelSizeChange,
    layout,
    onLayout,
    marketLayout,
    onMarketLayout,
    orderBookSplitSize,
    setOrderbookSplitSize,
    dataListSplitSize,
    setDataListSplitSize,
    mainSplitSize,
    setMainSplitSize,
    dataListSplitHeightSM,
    setDataListSplitHeightSM,
    orderBookSplitHeightSM,
    setOrderbookSplitHeightSM,
    max2XL,
    max4XL,
    animating,
    setAnimating,
    updatePositions,
    showPositionIcon,
    horizontalDraggable,
    marketsWidth,
    tradindviewMaxHeight,
    dataListMinHeight,
  } = props;

  const { showCountdown, closeCountdown } = useShowRwaCountdown(props.symbol);
  const { brokerName } = useBadgeBySymbol(props.symbol);
  const { t } = useTranslation();

  const symbolInfoBarHeight = useMemo(() => {
    let height = 56;
    if (brokerName) {
      height += 46;
      height += 8;
    }
    if (showCountdown) {
      height += 48;
    }
    return height;
  }, [showCountdown, brokerName]);

  const { isRwa, open } = useGetRwaSymbolOpenStatus(props.symbol);

  useEffect(() => {
    if (isRwa && !open) {
      showRwaOutsideMarketHoursNotify();
    }
  }, [isRwa, open, props.symbol]);

  const [tradingViewFullScreen] = useLocalStorage(
    TradingviewFullscreenKey,
    false,
  );

  const [sortableItems, setSortableItems] = useLocalStorage<string[]>(
    OrderEntrySortKeys,
    ["margin", "assets", "orderEntry"],
  );

  const dropAnimationConfig = useMemo(() => {
    return {
      keyframes({
        transform,
      }: {
        transform: {
          initial: Transform;
          final: Transform;
        };
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
              // scaleX: 0.85,
              // scaleY: 0.85,
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
        // console.log(active.node);
        active.node.style.opacity = "0";
        const innerElement = dragOverlay.node.querySelector(".inner-content");
        if (innerElement) {
          // innerElement.animate(
          //   [{ transform: "scale(1.05)" }, { transform: "scale(1)" }],
          //   {
          //     duration: 200,
          //     easing: "ease-out",
          //   },
          // );
          innerElement.classList.add("oui-animate-shake");
        }
        return () => {
          active.node.style.opacity = "";
        };
      },
    };
  }, []);

  // Configure sensors for drag and drop interactions
  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: {
        distance: 5,
      },
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );

  // State for drag overlay management
  const [activeId, setActiveId] = useState<string | null>(null);

  /**
   * Handle drag start event for sortable panels
   * Sets the active dragging item for overlay rendering
   */
  function handleDragStart(event: DragStartEvent) {
    setActiveId(event.active.id as string);
  }

  /**
   * Handle drag end event for sortable panels
   * Updates the order of sortable items and corresponding positions
   */
  function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event;

    if (active.id !== over?.id && over) {
      const oldIndex = sortableItems.indexOf(active.id as string);
      const newIndex = sortableItems.indexOf(over.id as string);

      if (oldIndex !== -1 && newIndex !== -1) {
        // Update sortableItems order
        const newItems = arrayMove(sortableItems, oldIndex, newIndex);
        setSortableItems(newItems as string[]);

        // Also update positions to keep them in sync
        // updatePositions(oldIndex, newIndex);
      }
    }

    // Reset active id after drag ends
    setActiveId(null);
  }

  const minScreenHeight = useMemo(() => {
    return tradingViewFullScreen
      ? 0
      : symbolInfoBarHeight +
          orderbookMaxHeight +
          dataListInitialHeight +
          space * 4;
  }, [tradingViewFullScreen]);

  const minScreenHeightSM =
    topBarHeight +
    bottomBarHeight +
    symbolInfoBarHeight +
    tradindviewMinHeight +
    orderbookMinHeight +
    dataListMinHeight +
    space * 4;

  const horizontalMarketsView = (
    <HorizontalMarketsWidget
      symbol={props.symbol}
      onSymbolChange={props.onSymbolChange}
      maxItems={-1} // show all markets
      dropdownPos={marketLayout === "bottom" ? "top" : "bottom"}
    />
  );

  const containerPaddingX = useMemo(() => (max2XL ? 12 : 8), [max2XL]);

  const stickyHorizontalMarketsView = (
    <Box
      className={cn(
        "oui-trading-markets-container",
        "oui-bg-base-10",
        // -8 is for reducing the container's padding
        "oui-sticky oui-z-30 oui-mb-[-8px] oui-py-2",
        // Split line disabled for > 2xl screens
        !max2XL && "oui-mt-[-8px]",
      )}
      style={{
        bottom: 0,
        minWidth:
          (max2XL ? 1024 : 1440) - scrollBarWidth - containerPaddingX * 2,
      }}
    >
      {horizontalMarketsView}
    </Box>
  );

  const marketsWidget = (
    <SideMarketsWidget
      symbol={props.symbol}
      onSymbolChange={props.onSymbolChange}
      panelSize={panelSize}
    />
  );

  const toggleButtoncls = cn(
    "oui-text-base-contrast-36",
    resizeable
      ? "oui-cursor-pointer hover:oui-text-base-contrast-80"
      : "oui-cursor-not-allowed",
  );

  const marketsHeader = (
    <Flex
      className={
        panelSize === "small"
          ? "oui-absolute oui-end-[-20px] oui-z-50"
          : "oui-relative"
      }
      justify={panelSize === "large" ? "between" : "center"}
      width="100%"
      px={3}
    >
      {panelSize === "large" && (
        <Text size="base" intensity={80}>
          {t("common.markets")}
        </Text>
      )}
      {panelSize === "large" && (
        <div
          onClick={resizeable ? () => onPanelSizeChange?.("middle") : undefined}
        >
          <CollapseIcon className={toggleButtoncls} />
        </div>
      )}
      {(panelSize === "middle" || panelSize === "small") && (
        <div
          onClick={resizeable ? () => onPanelSizeChange?.("large") : undefined}
        >
          <ExpandIcon className={toggleButtoncls} />
        </div>
      )}
    </Flex>
  );

  const marketsView = (
    <Box
      intensity={900}
      pt={3}
      r="2xl"
      height="100%"
      width={marketsWidth}
      style={{ minWidth: marketsWidth }}
      className="oui-trading-markets-container oui-min-h-0 oui-min-w-0 oui-max-w-full oui-transition-all oui-duration-150"
      onTransitionEnd={() => setAnimating(false)}
    >
      <Flex
        id="oui-side-markets"
        className="oui-relative oui-min-h-0 oui-min-w-0 oui-font-semibold"
        direction="column"
        gapY={5}
        height="100%"
        width="100%"
      >
        {marketsHeader}

        {/* List: flex-1 min-h-0 under header+gap; overflow-hidden + rounded-b-2xl clips table to card bottom (outer Box r="2xl"). */}
        {!animating && marketLayout === "left" && (
          <Box
            width="100%"
            className="oui-min-h-0 oui-min-w-0 oui-max-w-full oui-flex-1 oui-overflow-hidden oui-rounded-b-2xl"
          >
            {marketsWidget}
          </Box>
        )}
      </Flex>
    </Box>
  );

  const symbolInfoBarView = (
    <Box
      className="oui-trading-symbolInfoBar-container"
      width="100%"
      style={{
        minHeight: symbolInfoBarHeight,
        height: symbolInfoBarHeight,
      }}
    >
      <SymbolInfoBarFullWidget
        symbol={props.symbol}
        onSymbolChange={props.onSymbolChange}
        closeCountdown={closeCountdown}
        showCountdown={showCountdown}
        trailing={
          <React.Suspense fallback={null}>
            <LazySwitchLayout
              layout={layout}
              onLayout={onLayout}
              marketLayout={marketLayout}
              onMarketLayout={onMarketLayout}
            />
          </React.Suspense>
        }
      />
    </Box>
  );

  const { library_path, ...restTradingViewConfig } = props.tradingViewConfig;

  const tradingviewWidget = (
    <TradingviewWidget
      classNames={{
        root: cn(
          tradingViewFullScreen
            ? "!oui-absolute oui-inset-0 oui-z-40 oui-bg-base-10"
            : "oui-z-1",
        ),
        content: cn(
          tradingViewFullScreen
            ? "oui-inset-3 oui-overflow-hidden oui-rounded-[16px] oui-bg-base-9"
            : "",
        ),
      }}
      symbol={props.symbol}
      {...restTradingViewConfig}
      libraryPath={library_path}
    />
  );

  const tradingView = (
    <Box
      width="100%"
      height="100%"
      intensity={900}
      r="2xl"
      style={{ flex: 1, minWidth: tradingViewMinWidth }}
      className="oui-trading-tradingview-container oui-overflow-hidden"
    >
      {tradingviewWidget}
    </Box>
  );

  const orderbookWidget = (
    <React.Suspense fallback={null}>
      <LazyOrderBookAndTradesWidget symbol={props.symbol} />
    </React.Suspense>
  );

  const orderbookView = (
    <Box
      r="2xl"
      height="100%"
      style={{
        minWidth: orderbookMinWidth,
        maxWidth: horizontalDraggable ? orderbookMaxWidth : orderbookMinWidth,
        width: orderBookSplitSize,
      }}
      className="oui-trading-orderBook-container oui-overflow-hidden"
    >
      {orderbookWidget}
    </Box>
  );

  const dataListWidget = (
    <React.Suspense fallback={null}>
      <LazyDataListWidget
        current={undefined}
        symbol={props.symbol}
        sharePnLConfig={props.sharePnLConfig}
      />
    </React.Suspense>
  );

  const dataListView = (
    <Box
      intensity={900}
      r="2xl"
      p={2}
      style={{
        height: dataListSplitSize,
        // height: `calc(100% - ${symbolInfoBarHeight}px - ${orderbookMaxHeight}px - ${space}px)`,
        minHeight: dataListInitialHeight,
        // minHeight: `max(${dataListMinHeight}px, calc(100vh - ${symbolInfoBarHeight}px - ${orderbookMaxHeight}px - ${space}px))`,
      }}
      className="oui-trading-dataList-container oui-overflow-hidden"
    >
      {dataListWidget}
    </Box>
  );

  const orderInteractionWidgets = useMemo(() => {
    return {
      margin: {
        className: "oui-trading-riskRate-container",
        element: (
          <React.Suspense fallback={null}>
            <LazyRiskRateWidget />
          </React.Suspense>
        ),
      },
      assets: {
        className:
          "oui-trading-assetsView-container oui-border oui-border-line-12",
        element: (
          <>
            <React.Suspense fallback={null}>
              <LazyAssetViewWidget
                isFirstTimeDeposit={props.isFirstTimeDeposit}
              />
            </React.Suspense>
            <DepositStatusWidget
              className="oui-mt-3 oui-gap-y-2"
              onClick={props.navigateToPortfolio}
            />
          </>
        ),
      },
      orderEntry: {
        className: "oui-trading-orderEntry-container",
        element: (
          <OrderEntryWidget
            symbol={props.symbol}
            disableFeatures={
              props.disableFeatures as unknown as (
                | "slippageSetting"
                | "feesInfo"
              )[]
            }
          />
        ),
      },
    };
  }, [
    props.isFirstTimeDeposit,
    props.disableFeatures,
    props.navigateToPortfolio,
    props.symbol,
  ]);

  const orderEntryView = (
    <Flex
      className="oui-trading-orderEntry-container"
      gapY={2}
      direction="column"
      height="100%"
      style={{
        minWidth: orderEntryMinWidth,
        maxWidth: horizontalDraggable ? orderEntryMaxWidth : orderEntryMinWidth,
        width: mainSplitSize,
      }}
    >
      {sortableItems.map((key: string) => {
        return (
          <SortablePanel
            key={key}
            id={key}
            showIndicator={showPositionIcon}
            className={
              orderInteractionWidgets[
                key as keyof typeof orderInteractionWidgets
              ].className
            }
          >
            {
              orderInteractionWidgets[
                key as keyof typeof orderInteractionWidgets
              ].element
            }
          </SortablePanel>
        );
      })}
    </Flex>
  );

  const renderTradingView = () => {
    if (max4XL && layout === "right") {
      return (
        <Flex
          gap={2}
          itemAlign="stretch"
          className="oui-min-h-0 oui-flex-1 oui-overflow-hidden"
          style={{ minWidth: marketsWidth + tradingViewMinWidth + space }}
        >
          {marketLayout === "left" && marketsView}
          {tradingView}
        </Flex>
      );
    }

    return tradingView;
  };

  const tradingViewAndOrderbookView = (
    <SplitLayout
      style={{
        // the style width is not set, and a child node style needs to be set to flex: 1 to adapt
        flex: 1,
        minHeight: orderbookMinHeight,
        // maxHeight: orderbookMaxHeight,
      }}
      onSizeChange={setOrderbookSplitSize}
      disable={!horizontalDraggable}
    >
      {renderTradingView()}
      {orderbookView}
    </SplitLayout>
  );

  const renderTradingViewAndOrderbookView = () => {
    if (max4XL && layout === "left") {
      return (
        <Flex
          gapX={2}
          itemAlign="stretch"
          className="oui-min-h-0"
          style={{ minHeight: orderbookMinHeight }}
          height="100%"
        >
          {tradingViewAndOrderbookView}
          {marketLayout === "left" && marketsView}
        </Flex>
      );
    }
    return tradingViewAndOrderbookView;
  };

  const orderEntryPanelWidth =
    max4XL && horizontalDraggable
      ? getClampedPanelWidth(
          mainSplitSize,
          orderEntryMinWidth,
          orderEntryMaxWidth,
        )
      : orderEntryMinWidth;

  const orderBookPanelWidth =
    max4XL && horizontalDraggable
      ? getClampedPanelWidth(
          orderBookSplitSize,
          orderbookMinWidth,
          orderbookMaxWidth,
        )
      : orderbookMinWidth;

  const mainViewMinWidth = max4XL
    ? marketsWidth + tradingViewMinWidth + orderBookPanelWidth + space * 2
    : tradingViewMinWidth + orderBookPanelWidth + space;

  const mainContentMinWidth =
    mainViewMinWidth +
    orderEntryPanelWidth +
    space +
    (!max4XL && marketLayout === "left" ? marketsWidth + space : 0);

  const mainView = (
    <Flex
      direction="column"
      className="oui-flex-1 oui-overflow-hidden"
      gap={2}
      style={{
        minWidth: mainViewMinWidth,
      }}
    >
      {symbolInfoBarView}
      <SplitLayout
        style={{
          // height: orderbookMaxHeight + dataListInitialHeight + space,
          maxHeight: `calc(100% - ${symbolInfoBarHeight}px - ${space}px)`,
        }}
        className="oui-w-full"
        mode="vertical"
        onSizeChange={setDataListSplitSize}
      >
        {renderTradingViewAndOrderbookView()}
        {dataListView}
      </SplitLayout>
    </Flex>
  );

  const onSizeChange = (width: string) =>
    layout === "left"
      ? setMainSplitSize(getOffsetSizeNum(width))
      : setMainSplitSize(width);

  if (max2XL) {
    return (
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragStart={handleDragStart}
        onDragEnd={handleDragEnd}
        modifiers={[restrictToVerticalAxis]}
      >
        <SortableContext
          items={sortableItems}
          strategy={verticalListSortingStrategy}
        >
          <Box height="100%">
            {marketLayout === "top" && (
              <Box
                className={cn(
                  "oui-trading-markets-container oui-mt-2 oui-max-h-8 oui-px-3",
                  props.className,
                )}
              >
                {horizontalMarketsView}
              </Box>
            )}

            <SplitLayout
              ref={props.max2XLSplitRef}
              style={{
                minHeight: minScreenHeightSM,
                minWidth: 1024 - scrollBarWidth,
                // height: props.extraHeight ? props.extraHeight : undefined,
              }}
              className={cn(
                "oui-flex oui-flex-1",
                "oui-size-full oui-min-w-[1018px]",
                "oui-px-3 oui-py-2",
                props.className,
              )}
              onSizeChange={setDataListSplitHeightSM}
              onDragging={props.onDataListSplitHeightDragging}
              mode="vertical"
            >
              <Flex
                gapX={2}
                itemAlign="stretch"
                className={cn(
                  "oui-flex-1",
                  layout === "left" && "oui-flex-row-reverse",
                )}
                style={{
                  minHeight: Math.max(
                    symbolInfoBarHeight +
                      tradindviewMinHeight +
                      orderbookMinHeight +
                      space * 2,
                    props.orderEntryHeight,
                  ),
                  maxHeight:
                    symbolInfoBarHeight +
                    tradindviewMaxHeight +
                    orderbookMaxHeight +
                    space * 2,
                }}
              >
                <Flex
                  height="100%"
                  className="oui-w-[calc(100%_-_280px_-_12px)] oui-flex-1"
                  direction="column"
                  gapY={2}
                >
                  {symbolInfoBarView}
                  <Flex
                    width="100%"
                    height="100%"
                    gapX={2}
                    itemAlign="stretch"
                    style={{
                      minHeight:
                        tradindviewMinHeight + orderbookMinHeight + space,
                      maxHeight:
                        tradindviewMaxHeight + orderbookMaxHeight + space,
                    }}
                    className={cn(
                      "oui-flex-1",
                      layout === "left" && "oui-flex-row-reverse",
                    )}
                  >
                    {marketLayout === "left" && (
                      <Box
                        intensity={900}
                        pt={3}
                        r="2xl"
                        width={marketsWidth}
                        className="oui-overflow-hidden"
                        style={{
                          minHeight:
                            tradindviewMinHeight + orderbookMinHeight + space,
                          maxHeight:
                            tradindviewMaxHeight + orderbookMaxHeight + space,
                        }}
                      >
                        {marketsWidget}
                      </Box>
                    )}
                    <SplitLayout
                      ref={props.tradingviewAndOrderbookSplitRef}
                      mode="vertical"
                      style={{ width: `calc(100% - ${marketsWidth}px)` }}
                      className="oui-flex-1"
                      onSizeChange={setOrderbookSplitHeightSM}
                      onDragging={props.onTradingviewAndOrderbookDragging}
                    >
                      <Box
                        width="100%"
                        intensity={900}
                        r="2xl"
                        style={{
                          minHeight: tradindviewMinHeight,
                          maxHeight: tradindviewMaxHeight,
                          height: 1200,
                        }}
                      >
                        {tradingviewWidget}
                      </Box>

                      <Box
                        r="2xl"
                        height="100%"
                        width="100%"
                        style={{
                          minHeight: orderbookMinHeight,
                          maxHeight: orderbookMaxHeight,
                          height: orderBookSplitHeightSM,
                        }}
                        className="oui-flex-1"
                      >
                        {orderbookWidget}
                      </Box>
                    </SplitLayout>
                  </Flex>
                </Flex>
                <Flex
                  ref={props.orderEntryViewRef}
                  id="orderEntryView"
                  gapY={3}
                  direction="column"
                  className="oui-relative"
                  style={{
                    width: orderEntryMinWidth,
                    // force order entry render actual content height
                    height: "max-content",
                    // height:
                    //   props.extraHeight && props.extraHeight > 100
                    //     ? undefined
                    // : "max-content",
                  }}
                >
                  <Flex
                    gapY={2}
                    direction="column"
                    height="100%"
                    style={{
                      minWidth: orderEntryMinWidth,
                      maxWidth: horizontalDraggable
                        ? orderEntryMaxWidth
                        : orderEntryMinWidth,
                      width: mainSplitSize,
                    }}
                  >
                    {sortableItems.map((key: string) => {
                      return (
                        <SortablePanel
                          key={key}
                          id={key}
                          showIndicator={showPositionIcon}
                          className={
                            orderInteractionWidgets[
                              key as keyof typeof orderInteractionWidgets
                            ].className
                          }
                        >
                          {
                            orderInteractionWidgets[
                              key as keyof typeof orderInteractionWidgets
                            ].element
                          }
                        </SortablePanel>
                      );
                    })}
                  </Flex>
                  <Box height={props.extraHeight} />
                </Flex>
              </Flex>

              <Box
                intensity={900}
                r="2xl"
                p={2}
                style={{
                  height: dataListSplitHeightSM,
                  minHeight: Math.max(dataListMinHeight, props.dataListHeight),
                  maxHeight: dataListMaxHeight,
                }}
                className="oui-overflow-hidden"
              >
                {dataListWidget}
              </Box>

              {marketLayout === "bottom" && stickyHorizontalMarketsView}
            </SplitLayout>
          </Box>
        </SortableContext>
        <DragOverlay dropAnimation={dropAnimationConfig}>
          {activeId ? (
            <SortablePanel
              id={activeId}
              showIndicator={showPositionIcon}
              dragOverlay
              className={`${
                orderInteractionWidgets[
                  activeId as keyof typeof orderInteractionWidgets
                ].className
              } oui-shadow-lg oui-shadow-base-9`}
            >
              {
                orderInteractionWidgets[
                  activeId as keyof typeof orderInteractionWidgets
                ].element
              }
            </SortablePanel>
          ) : null}
        </DragOverlay>
      </DndContext>
    );
  }

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={closestCenter}
      onDragStart={handleDragStart}
      onDragEnd={handleDragEnd}
      modifiers={[restrictToVerticalAxis]}
    >
      <SortableContext
        items={sortableItems}
        strategy={verticalListSortingStrategy}
      >
        <Flex
          style={{
            minHeight: minScreenHeight,
            minWidth: max4XL
              ? Math.max(1440 - scrollBarWidth, mainContentMinWidth + space * 2)
              : 1440 - scrollBarWidth,
          }}
          className={cn(
            props.className,
            "oui-justify-start",
            tradingViewFullScreen &&
              "oui-relative oui-h-[calc(100vh-80px)] oui-w-screen oui-overflow-hidden !oui-p-0",
          )}
          width="100%"
          p={2}
          gap={2}
          itemAlign="stretch"
          direction="column"
        >
          {/* Horizontal Markets View on top for !=2xl screens */}
          {marketLayout === "top" && horizontalMarketsView}

          {/* Main Content Group */}
          <Flex
            itemAlign="stretch"
            className={cn(
              "oui-min-h-0 oui-flex-1 oui-overflow-hidden",
              layout === "left" && "oui-flex-row-reverse",
            )}
            gap={2}
          >
            {!max4XL && marketLayout === "left" && marketsView}
            <SplitLayout
              className={cn("oui-flex oui-flex-1 oui-overflow-hidden")}
              style={max4XL ? { minWidth: mainContentMinWidth } : undefined}
              onSizeChange={onSizeChange}
              disable={!horizontalDraggable}
            >
              {layout === "left" && orderEntryView}
              {mainView}
              {layout === "right" && orderEntryView}
            </SplitLayout>
          </Flex>

          {marketLayout === "bottom" && stickyHorizontalMarketsView}
        </Flex>
      </SortableContext>
      <DragOverlay
        dropAnimation={dropAnimationConfig}

        // style={{
        //   transform: "scale(1.05)",
        // }}
        // transition="transform 200ms ease"
        // className="oui-animate-pop"
      >
        {activeId ? (
          <SortablePanel
            id={activeId}
            showIndicator={showPositionIcon}
            dragOverlay
            className={`${
              orderInteractionWidgets[
                activeId as keyof typeof orderInteractionWidgets
              ].className
            } oui-shadow-lg oui-shadow-base-9`}
          >
            {
              orderInteractionWidgets[
                activeId as keyof typeof orderInteractionWidgets
              ].element
            }
          </SortablePanel>
        ) : null}
      </DragOverlay>
    </DndContext>
  );
};
