/**
 * SplitTradingLayout - LayoutStrategy Renderer for the split trading desktop layout.
 *
 * Implements LayoutRendererProps<Record<string, unknown>> so it can be used as
 * the Renderer in splitTradingStrategy. Receives the PanelRegistry from LayoutHost
 * (populated by trading-next's createTradingPanelRegistry) and all layout state
 * comes from useSplitLayout internally.
 *
 * This mirrors the JSX structure of trading.ui.desktop.tsx but all panel content
 * is sourced from the panels registry - no direct widget imports.
 */
import React, {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  DragOverlay,
  type DragStartEvent,
  type DragEndEvent,
} from "@dnd-kit/core";
import { restrictToVerticalAxis } from "@dnd-kit/modifiers";
import {
  arrayMove,
  SortableContext,
  sortableKeyboardCoordinates,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS, type Transform } from "@dnd-kit/utilities";
import { useLocalStorage } from "@paxeer-network/hooks";
import type {
  LayoutRendererProps,
  PanelRegistry,
} from "@paxeer-network/layout-core";
import { TRADING_PANEL_IDS } from "@paxeer-network/layout-core";
import { TradingviewFullscreenKey } from "@paxeer-network/types";
import { Box, cn, Flex } from "@paxeer-network/ui";
import {
  useSplitLayout,
  ORDER_ENTRY_MIN_WIDTH,
  ORDER_ENTRY_MAX_WIDTH,
  ORDERBOOK_MIN_WIDTH,
  ORDERBOOK_MAX_WIDTH,
  ORDERBOOK_MIN_HEIGHT,
  ORDERBOOK_MAX_HEIGHT,
  TRADINGVIEW_MIN_HEIGHT,
  TRADINGVIEW_MIN_WIDTH,
  DATA_LIST_INITIAL_HEIGHT,
  DATA_LIST_MAX_HEIGHT,
  SPACE,
  SYMBOL_INFO_BAR_HEIGHT,
} from "../hooks/useSplitLayout";
import { TradingSortablePanel } from "./TradingSortablePanel";
import { TradingSplitLayout } from "./TradingSplitLayout";

// ─── Legacy re-exports (keep for backward compat) ────────────────────────────

/** @deprecated Use PanelRegistry from @paxeer-network/layout-core instead. */
export type SplitLayoutPanelRegistryEntry = {
  node: React.ReactNode;
  props?: Record<string, unknown>;
};

/** @deprecated Use PanelRegistry from @paxeer-network/layout-core instead. */
export type SplitLayoutPanelRegistry = PanelRegistry;

// ─── Layout constants ─────────────────────────────────────────────────────────

/** Scroll-bar width reserve for min-width calculations. */
const SCROLLBAR_WIDTH = 6;
const TOP_BAR_HEIGHT = 48;
const BOTTOM_BAR_HEIGHT = 29;

/** LocalStorage key for sortable order-entry panel ordering. */
const ORDER_ENTRY_SORT_KEY = "paxeerNetwork_order_entry_sortable_items";

/** Default sortable items using TRADING_PANEL_IDS (panel registry keys). */
const DEFAULT_SORTABLE_ITEMS = [
  TRADING_PANEL_IDS.MARGIN,
  TRADING_PANEL_IDS.ASSETS,
  TRADING_PANEL_IDS.ORDER_ENTRY,
];

/**
 * Sortable column class names per registry id (and legacy short keys used in stored order).
 */
function orderEntrySortablePanelClassName(id: string): string | undefined {
  if (id === TRADING_PANEL_IDS.MARGIN || id === "margin") {
    return "oui-trading-riskRate-container";
  }
  if (id === TRADING_PANEL_IDS.ASSETS || id === "assets") {
    return "oui-trading-assetsView-container oui-border oui-border-line-12";
  }
  if (id === TRADING_PANEL_IDS.ORDER_ENTRY || id === "orderEntry") {
    return "oui-trading-orderEntry-container";
  }
  return undefined;
}

// ─── Props ────────────────────────────────────────────────────────────────────

/**
 * SplitTradingLayout implements LayoutRendererProps so it works as the Renderer
 * in splitTradingStrategy. The panels registry is built by trading-next's
 * DesktopLayout and passed through LayoutHost.
 */
export type SplitTradingLayoutProps = LayoutRendererProps<
  Record<string, unknown>
>;

// ─── Drop animation ──────────────────────────────────────────────────────────

/**
 * Scale + shake drop animation for drag overlay.
 * Matches the animation in the original trading DesktopLayout.
 */
const dropAnimationConfig = {
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
};

// ─── Component ───────────────────────────────────────────────────────────────

/**
 * Full trading desktop layout that implements LayoutRendererProps.
 *
 * Receives the PanelRegistry from LayoutHost (populated by trading-next's
 * createTradingPanelRegistry). All layout state is managed by useSplitLayout
 * and local hooks — no dependency on DesktopLayoutProps.
 */
export function SplitTradingLayout(
  props: SplitTradingLayoutProps,
): React.ReactElement {
  const { panels, className } = props;

  // ── Layout state from useSplitLayout (split sizes, positions, markets) ────
  const {
    layout,
    marketLayout,
    max2XL,
    max4XL,
    horizontalDraggable,
    marketsWidth,
    mainSplitSize,
    setMainSplitSize,
    orderBookSplitSize,
    setOrderbookSplitSize,
    dataListSplitSize,
    setDataListSplitSize,
    dataListSplitHeightSM,
    setDataListSplitHeightSM,
    orderBookSplitHeightSM,
    setOrderbookSplitHeightSM,
    tradingviewMaxHeight,
    extraHeight,
    setExtraHeight,
    dataListHeight: dataListMinHeight,
  } = useSplitLayout();

  /**
   * Live height of symbol-info chrome (panel + padding box). ResizeObserver keeps split max-heights aligned when
   * community-listing banners or countdown increase row count — fixed constants mis-sized the chart/data-list split.
   */
  const symbolInfoBarChromeRef = useRef<React.ElementRef<typeof Box>>(null);
  const [symbolBarChromeHeight, setSymbolBarChromeHeight] = useState(
    SYMBOL_INFO_BAR_HEIGHT,
  );

  useLayoutEffect(() => {
    const el = symbolInfoBarChromeRef.current;
    if (!el) return;
    const sync = (): void => {
      const next = el.getBoundingClientRect().height;
      if (!(Number.isFinite(next) && next > 0)) return;
      setSymbolBarChromeHeight((prev) =>
        Math.round(Math.abs(prev - next)) >= 1 ? Math.round(next) : prev,
      );
    };
    sync();
    const observer = new ResizeObserver(sync);
    observer.observe(el);
    return (): void => observer.disconnect();
  }, []);

  /** TradingView fullscreen state — shared via localStorage with ui-tradingview. */
  const [tradingViewFullScreen] = useLocalStorage(
    TradingviewFullscreenKey,
    false,
  );

  // ── DnD sortable state for order-entry panels (persisted) ─────────────────
  const [sortableItems, setSortableItems] = useLocalStorage<string[]>(
    ORDER_ENTRY_SORT_KEY,
    DEFAULT_SORTABLE_ITEMS,
  );

  // ── Refs for split containers and order entry measurement ─────────────────
  const tradingviewAndOrderbookSplitRef =
    useRef<React.ElementRef<typeof TradingSplitLayout>>(null);
  const max2XLSplitRef =
    useRef<React.ElementRef<typeof TradingSplitLayout>>(null);
  const orderEntryViewRef = useRef<HTMLDivElement>(null);
  const [orderEntryHeight, setOrderEntryHeight] = useState(0);

  /** Observe order entry column height for max2XL min-height constraint. */
  useEffect(() => {
    const el = orderEntryViewRef.current;
    if (!el) return;
    const observer = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (entry) setOrderEntryHeight(entry.contentRect.height);
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  /** Reset extra height spacer when data list is dragged (avoids stale offset). */
  const onDataListSplitHeightDragging = useCallback(() => {
    setExtraHeight(0);
  }, [setExtraHeight]);

  /** No-op dragging handler; extraHeight is managed by useSplitLayout. */
  const onTradingviewAndOrderbookDragging = useCallback(() => {}, []);

  const [activeId, setActiveId] = useState<string | null>(null);

  /** showPositionIcon: show drag handle when there are sortable panels. Always true for split layout. */
  const showPositionIcon = true;

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );

  /**
   * Track which item is being dragged so DragOverlay can render it.
   */
  function handleDragStart(event: DragStartEvent) {
    setActiveId(event.active.id as string);
  }

  /**
   * Reorder sortableItems when an item is dropped onto a new position.
   */
  function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event;
    if (active.id !== over?.id && over) {
      const oldIndex = sortableItems.indexOf(active.id as string);
      const newIndex = sortableItems.indexOf(over.id as string);
      if (oldIndex !== -1 && newIndex !== -1) {
        setSortableItems(arrayMove(sortableItems, oldIndex, newIndex));
      }
    }
    setActiveId(null);
  }

  // ── Layout size callbacks ─────────────────────────────────────────────────

  const onMainSplitSizeChange = (width: string) =>
    layout === "left"
      ? setMainSplitSize(`${100 - Math.min(Number(width), 100)}`)
      : setMainSplitSize(width);

  // ── Derived layout values ─────────────────────────────────────────────────

  const minScreenHeight = useMemo(() => {
    return tradingViewFullScreen
      ? 0
      : symbolBarChromeHeight +
          ORDERBOOK_MAX_HEIGHT +
          DATA_LIST_INITIAL_HEIGHT +
          SPACE * 4;
  }, [tradingViewFullScreen, symbolBarChromeHeight]);

  const minScreenHeightSM =
    TOP_BAR_HEIGHT +
    BOTTOM_BAR_HEIGHT +
    symbolBarChromeHeight +
    TRADINGVIEW_MIN_HEIGHT +
    ORDERBOOK_MIN_HEIGHT +
    dataListMinHeight +
    SPACE * 4;

  const containerPaddingX = useMemo(() => (max4XL ? 12 : 8), [max4XL]);

  // ── Panel helpers ─────────────────────────────────────────────────────────

  /** Retrieve a panel ReactNode from the registry by TRADING_PANEL_IDS key. */
  const getPanel = (id: string): React.ReactNode =>
    panels.get(id)?.node ?? null;

  // ── Sub-views (match trading.ui.desktop.tsx naming exactly) ───────────────

  const horizontalMarketsView = getPanel(TRADING_PANEL_IDS.HORIZONTAL_MARKETS);

  const stickyHorizontalMarketsView = (
    <Box
      className={cn(
        "oui-trading-markets-container",
        "oui-bg-base-10",
        "oui-sticky oui-z-30 oui-mb-[-8px] oui-py-2",
        !max4XL && "oui-mt-[-8px]",
      )}
      style={{
        bottom: 0,
        minWidth:
          (max4XL ? 1024 : 1440) - SCROLLBAR_WIDTH - containerPaddingX * 2,
      }}
    >
      {horizontalMarketsView}
    </Box>
  );

  const marketsWidget = getPanel(TRADING_PANEL_IDS.MARKETS);

  const marketsView = (
    <Box
      intensity={900}
      pt={3}
      r="2xl"
      height="100%"
      width={marketsWidth}
      style={{ minWidth: marketsWidth }}
      className="oui-trading-markets-container oui-transition-all oui-duration-150"
    >
      {marketLayout === "left" && marketsWidget}
    </Box>
  );

  // const symbolInfoBarView = (
  //   <Box
  //     ref={symbolInfoBarChromeRef}
  //     className="oui-trading-symbolInfoBar-container"
  //     intensity={900}
  //     r="2xl"
  //     px={3}
  //     width="100%"
  //     style={{ flexShrink: 0 }}
  //   >
  //     {getPanel(TRADING_PANEL_IDS.SYMBOL_INFO_BAR)}
  //   </Box>
  // );

  const symbolInfoBarView = getPanel(TRADING_PANEL_IDS.SYMBOL_INFO_BAR);

  const tradingviewWidget = getPanel(TRADING_PANEL_IDS.TRADING_VIEW);

  const tradingView = (
    <Box
      width="100%"
      height="100%"
      intensity={900}
      r="2xl"
      style={{ flex: 1, minWidth: TRADINGVIEW_MIN_WIDTH }}
      className="oui-trading-tradingview-container oui-overflow-hidden"
    >
      {tradingviewWidget}
    </Box>
  );

  const orderbookView = (
    <Box
      r="2xl"
      height="100%"
      style={{
        minWidth: ORDERBOOK_MIN_WIDTH,
        maxWidth: horizontalDraggable
          ? ORDERBOOK_MAX_WIDTH
          : ORDERBOOK_MIN_WIDTH,
        width: orderBookSplitSize,
      }}
      className="oui-trading-orderBook-container oui-overflow-hidden"
    >
      {getPanel(TRADING_PANEL_IDS.ORDERBOOK)}
    </Box>
  );

  const dataListView = (
    <Box
      intensity={900}
      r="2xl"
      p={2}
      style={{
        height: dataListSplitSize,
        minHeight: DATA_LIST_INITIAL_HEIGHT,
      }}
      className="oui-trading-dataList-container oui-overflow-hidden"
    >
      {getPanel(TRADING_PANEL_IDS.DATA_LIST)}
    </Box>
  );

  /**
   * The right-column sortable panels: margin/risk, assets, order entry.
   * Panels are rendered in the order stored in sortableItems.
   */
  const orderEntryView = (
    <Flex
      className="oui-trading-orderEntry-container"
      gapY={2}
      direction="column"
      height="100%"
      style={{
        minWidth: ORDER_ENTRY_MIN_WIDTH,
        maxWidth: horizontalDraggable
          ? ORDER_ENTRY_MAX_WIDTH
          : ORDER_ENTRY_MIN_WIDTH,
        width: mainSplitSize,
      }}
    >
      {sortableItems.map((id: string) => (
        <TradingSortablePanel
          key={id}
          id={id}
          showIndicator={showPositionIcon}
          className={orderEntrySortablePanelClassName(id)}
        >
          {getPanel(id)}
        </TradingSortablePanel>
      ))}
    </Flex>
  );

  /**
   * TradingView + orderbook horizontal split.
   * On ≥4xl screens with layout="right", also includes the markets sidebar.
   */
  const renderTradingView = () => {
    if (max4XL && layout === "right") {
      return (
        <Flex
          gap={2}
          className="oui-flex-1 oui-overflow-hidden"
          style={{ minWidth: marketsWidth + TRADINGVIEW_MIN_WIDTH + SPACE }}
        >
          {marketLayout === "left" && marketsView}
          {tradingView}
        </Flex>
      );
    }
    return tradingView;
  };

  const tradingViewAndOrderbookView = (
    <TradingSplitLayout
      style={{ flex: 1, minHeight: ORDERBOOK_MIN_HEIGHT }}
      onSizeChange={setOrderbookSplitSize}
      disable={!horizontalDraggable}
    >
      {renderTradingView()}
      {orderbookView}
    </TradingSplitLayout>
  );

  const renderTradingViewAndOrderbookView = () => {
    if (max4XL && layout === "left") {
      return (
        <Flex
          gapX={2}
          style={{ minHeight: ORDERBOOK_MIN_HEIGHT }}
          height="100%"
        >
          {tradingViewAndOrderbookView}
          {marketLayout === "left" && marketsView}
        </Flex>
      );
    }
    return tradingViewAndOrderbookView;
  };

  const mainView = (
    <Flex
      direction="column"
      className="oui-min-h-0 oui-flex-1 oui-overflow-hidden"
      gap={2}
      style={{
        minWidth: max4XL
          ? marketsWidth +
            TRADINGVIEW_MIN_WIDTH +
            ORDERBOOK_MIN_WIDTH +
            SPACE * 2
          : TRADINGVIEW_MIN_WIDTH + ORDERBOOK_MIN_WIDTH + SPACE,
      }}
    >
      {symbolInfoBarView}
      {/*
       * Do not use maxHeight: calc(100% - symbolBarPx): the symbol row is already flex item 1.
       * Subtracting bar height from 100% double-counts and expands the Split under the risk banner.
       */}
      <TradingSplitLayout
        style={{
          flex: 1,
          minHeight: 0,
          width: "100%",
          overflow: "hidden",
        }}
        className="oui-flex oui-min-h-0 oui-w-full oui-flex-1 oui-overflow-hidden"
        mode="vertical"
        onSizeChange={setDataListSplitSize}
      >
        {renderTradingViewAndOrderbookView()}
        {dataListView}
      </TradingSplitLayout>
    </Flex>
  );

  // ── Shared DnD overlay content ────────────────────────────────────────────

  const dragOverlayContent = activeId ? (
    <TradingSortablePanel
      id={activeId}
      showIndicator={showPositionIcon}
      dragOverlay
      className="oui-shadow-lg oui-shadow-base-9"
    >
      {getPanel(activeId)}
    </TradingSortablePanel>
  ) : null;

  // ─────────────────────────────────────────────────────────────────────────
  // ≤1279px (max2XL) — compact vertical layout
  // Matches the max2XL branch in trading.ui.desktop.tsx DesktopLayout
  // ─────────────────────────────────────────────────────────────────────────
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
                  className,
                )}
              >
                {horizontalMarketsView}
              </Box>
            )}

            <TradingSplitLayout
              ref={max2XLSplitRef}
              style={{
                minHeight: minScreenHeightSM,
                minWidth: 1024 - SCROLLBAR_WIDTH,
              }}
              className={cn(
                "oui-flex oui-flex-1",
                "oui-size-full oui-min-w-[1018px]",
                "oui-px-3 oui-py-2",
                className,
              )}
              onSizeChange={setDataListSplitHeightSM}
              onDragging={onDataListSplitHeightDragging}
              mode="vertical"
            >
              {/* Top: symbol bar + TradingView/Orderbook split + order entry */}
              <Flex
                gapX={2}
                itemAlign="stretch"
                className={cn(
                  "oui-flex-1",
                  layout === "left" && "oui-flex-row-reverse",
                )}
                style={{
                  minHeight: Math.max(
                    symbolBarChromeHeight +
                      TRADINGVIEW_MIN_HEIGHT +
                      ORDERBOOK_MIN_HEIGHT +
                      SPACE * 2,
                    orderEntryHeight,
                  ),
                  maxHeight:
                    symbolBarChromeHeight +
                    tradingviewMaxHeight +
                    ORDERBOOK_MAX_HEIGHT +
                    SPACE * 2,
                }}
              >
                {/* Chart + orderbook column */}
                <Flex
                  height="100%"
                  className="oui-min-h-0 oui-w-[calc(100%_-_280px_-_12px)] oui-flex-1"
                  direction="column"
                  gapY={2}
                >
                  {symbolInfoBarView}
                  {/*
                   * flex-1 + minHeight:0 consumes space below symbol bar without height="100%",
                   * which overlaps the taller risk-banner row inside the same column.
                   */}
                  <Flex
                    width="100%"
                    gapX={2}
                    itemAlign="stretch"
                    style={{
                      flex: 1,
                      minHeight:
                        TRADINGVIEW_MIN_HEIGHT + ORDERBOOK_MIN_HEIGHT + SPACE,
                      maxHeight:
                        tradingviewMaxHeight + ORDERBOOK_MAX_HEIGHT + SPACE,
                    }}
                    className={cn(
                      "oui-min-h-0 oui-flex-1",
                      layout === "left" && "oui-flex-row-reverse",
                    )}
                  >
                    {marketLayout === "left" && (
                      <Box
                        intensity={900}
                        pt={3}
                        r="2xl"
                        width={marketsWidth}
                        style={{
                          minHeight:
                            TRADINGVIEW_MIN_HEIGHT +
                            ORDERBOOK_MIN_HEIGHT +
                            SPACE,
                          maxHeight:
                            tradingviewMaxHeight + ORDERBOOK_MAX_HEIGHT + SPACE,
                        }}
                      >
                        {marketsWidget}
                      </Box>
                    )}
                    {/* TradingView / orderbook vertical split */}
                    <TradingSplitLayout
                      ref={tradingviewAndOrderbookSplitRef}
                      mode="vertical"
                      style={{ width: `calc(100% - ${marketsWidth}px)` }}
                      className="oui-flex-1"
                      onSizeChange={setOrderbookSplitHeightSM}
                      onDragging={onTradingviewAndOrderbookDragging}
                    >
                      <Box
                        width="100%"
                        intensity={900}
                        r="2xl"
                        style={{
                          minHeight: TRADINGVIEW_MIN_HEIGHT,
                          maxHeight: tradingviewMaxHeight,
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
                          minHeight: ORDERBOOK_MIN_HEIGHT,
                          maxHeight: ORDERBOOK_MAX_HEIGHT,
                          height: orderBookSplitHeightSM,
                        }}
                        className="oui-flex-1"
                      >
                        {getPanel(TRADING_PANEL_IDS.ORDERBOOK)}
                      </Box>
                    </TradingSplitLayout>
                  </Flex>
                </Flex>

                {/* Order-entry column (margin + assets + order form) */}
                <Flex
                  ref={orderEntryViewRef}
                  id="orderEntryView"
                  gapY={3}
                  direction="column"
                  className="oui-relative"
                  style={{
                    width: ORDER_ENTRY_MIN_WIDTH,
                    height: "max-content",
                  }}
                >
                  <Flex
                    gapY={2}
                    direction="column"
                    height="100%"
                    style={{
                      minWidth: ORDER_ENTRY_MIN_WIDTH,
                      maxWidth: horizontalDraggable
                        ? ORDER_ENTRY_MAX_WIDTH
                        : ORDER_ENTRY_MIN_WIDTH,
                      width: mainSplitSize,
                    }}
                  >
                    {sortableItems.map((id: string) => (
                      <TradingSortablePanel
                        key={id}
                        id={id}
                        showIndicator={showPositionIcon}
                        className={cn(
                          id === TRADING_PANEL_IDS.MARGIN &&
                            "oui-trading-riskRate-container",
                          id === TRADING_PANEL_IDS.ASSETS &&
                            "oui-trading-assetsView-container oui-border oui-border-line-12",
                          id === TRADING_PANEL_IDS.ORDER_ENTRY &&
                            "oui-trading-orderEntry-container",
                          id === "margin" && "oui-trading-riskRate-container",
                          id === "assets" &&
                            "oui-trading-assetsView-container oui-border oui-border-line-12",
                          id === "orderEntry" &&
                            "oui-trading-orderEntry-container",
                        )}
                      >
                        {getPanel(id)}
                      </TradingSortablePanel>
                    ))}
                  </Flex>
                  <Box height={extraHeight} />
                </Flex>
              </Flex>

              {/* Data list (positions/orders) */}
              <Box
                intensity={900}
                r="2xl"
                p={2}
                style={{
                  height: dataListSplitHeightSM,
                  minHeight: Math.max(dataListMinHeight, 0),
                  maxHeight: DATA_LIST_MAX_HEIGHT,
                }}
                className="oui-overflow-hidden"
              >
                {getPanel(TRADING_PANEL_IDS.DATA_LIST)}
              </Box>

              {marketLayout === "bottom" && stickyHorizontalMarketsView}
            </TradingSplitLayout>
          </Box>
        </SortableContext>
        <DragOverlay dropAnimation={dropAnimationConfig}>
          {dragOverlayContent}
        </DragOverlay>
      </DndContext>
    );
  }

  // ─────────────────────────────────────────────────────────────────────────
  // ≥1280px — full desktop layout
  // Matches the non-max2XL branch in trading.ui.desktop.tsx DesktopLayout
  // ─────────────────────────────────────────────────────────────────────────
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
            minWidth: 1440 - SCROLLBAR_WIDTH,
          }}
          className={cn(
            className,
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
          {/* Horizontal markets bar on top */}
          {marketLayout === "top" && horizontalMarketsView}

          {/* Main content row */}
          <Flex
            className={cn(
              "oui-flex-1 oui-overflow-hidden",
              layout === "left" && "oui-flex-row-reverse",
            )}
            gap={2}
          >
            {!max4XL && marketLayout === "left" && marketsView}

            {/* Horizontal split: order-entry | main content */}
            <TradingSplitLayout
              className="oui-flex oui-flex-1 oui-overflow-hidden"
              onSizeChange={onMainSplitSizeChange}
              disable={!horizontalDraggable}
            >
              {layout === "left" && orderEntryView}
              {mainView}
              {layout === "right" && orderEntryView}
            </TradingSplitLayout>
          </Flex>

          {marketLayout === "bottom" && stickyHorizontalMarketsView}
        </Flex>
      </SortableContext>

      <DragOverlay dropAnimation={dropAnimationConfig}>
        {dragOverlayContent}
      </DragOverlay>
    </DndContext>
  );
}
