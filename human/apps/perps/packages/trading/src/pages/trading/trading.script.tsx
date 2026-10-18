import { RefObject, useEffect, useMemo, useRef, useState } from "react";
import Split from "@uiw/react-split";
import {
  useAccount,
  useCollateral,
  useLocalStorage,
  useMediaQuery,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { useAppContext, useDataTap } from "@paxeer-network/react-app";
import { AccountStatusEnum } from "@paxeer-network/types";
import { modal } from "@paxeer-network/ui";
import type { LayoutPosition } from "../../components/desktop/layout/switchLayout";
import { useSplitPersistent } from "../../components/desktop/layout/useSplitPersistent";
import { PortfolioSheetWidget } from "../../components/mobile/portfolioSheet";
import { useTradingLocalStorage } from "../../hooks/";
import { useTradingPageContext } from "../../provider/tradingPageContext";
import { TradingPageState } from "../../types/types";
import { useFirstTimeDeposit } from "./hooks/useFirstTimeDeposit";

export type TradingState = ReturnType<typeof useTradingScript>;

export const scrollBarWidth = 6;
export const topBarHeight = 48;
export const bottomBarHeight = 29;
export const space = 8;
export const symbolInfoBarHeight = 54;

export const orderEntryMinWidth = 280;
export const orderEntryMaxWidth = 360;

export const orderbookMinWidth = 280;
export const orderbookMaxWidth = 732;

export const orderbookMinHeight = 464;
export const orderbookMaxHeight = 728;

export const tradindviewMinHeight = 320;

export const tradingViewMinWidth = 540;

export const dataListMaxHeight = 800;
export const dataListInitialHeight = 350;

const PAXEER_NETWORK_ORDER_ENTRY_SIDE_MARKETS_LAYOUT =
  "paxeerNetwork_order_entry_side_markets_layout";

const PAXEER_NETWORK_SIDE_MARKETS_MODE_KEY = "paxeerNetwork_side_markets_mode";

const PAXEER_NETWORK_HORIZONTAL_MARKETS_LAYOUT = "paxeerNetwork_horizontal_markets_layout";

export type MarketLayoutPosition = "left" | "top" | "bottom" | "hide";

export const useTradingScript = () => {
  const [openMarketsSheet, setOpenMarketsSheet] = useState(false);
  const props = useTradingPageContext();
  const { state } = useAccount();
  const { t } = useTranslation();
  const { wrongNetwork, disabledConnect, restrictedInfo, onRouteChange } =
    useAppContext();
  const { hideAssets, setHideAssets } = useTradingLocalStorage();

  const isFirstTimeDeposit = useFirstTimeDeposit();

  const { totalValue } = useCollateral();

  const total = useDataTap(totalValue);

  /** max-width: 1279px */
  const max2XL = useMediaQuery("(max-width: 1279px)");

  /** min-width: 1440px */
  const min3XL = useMediaQuery("(min-width: 1440px)");

  /** max-width: 1680px */
  const max4XL = useMediaQuery("(max-width: 1680px)");

  // Order entry and side market list position, default Order entry in right
  const [layout, setLayout] = useLocalStorage<LayoutPosition>(
    PAXEER_NETWORK_ORDER_ENTRY_SIDE_MARKETS_LAYOUT,
    "right",
  );

  // Horizontal markets layout position, default left
  const [marketLayout, setMarketLayout] = useLocalStorage<MarketLayoutPosition>(
    PAXEER_NETWORK_HORIZONTAL_MARKETS_LAYOUT,
    "left",
  );

  const canTrade = useMemo<boolean>(() => {
    return (
      !wrongNetwork &&
      !disabledConnect &&
      (state.status >= AccountStatusEnum.EnableTrading ||
        state.status === AccountStatusEnum.EnableTradingWithoutConnected)
    );
  }, [state.status, wrongNetwork, disabledConnect]);

  const onShowPortfolioSheet = () => {
    if (canTrade) {
      modal.sheet({
        title: t("trading.asset&Margin"),
        leading: props.bottomSheetLeading,
        content: <PortfolioSheetWidget />,
      });
    }
  };

  const horizontalDraggable = useMemo(() => min3XL, [min3XL]);

  const positionsState = useOrderEntryPositions({
    canTrade,
    isFirstTimeDeposit,
  });

  const marketsCollapseState = useMarketsCollapse({ resizeable: min3XL });

  const observerState = useObserverOrderEntry({ max2XL });

  const marketsWidth = useMemo(() => {
    switch (marketsCollapseState.panelSize) {
      case "small":
        return 0;
      case "middle":
        return 70;
      case "large":
        return 360;
      default:
        return 0;
    }
  }, [marketsCollapseState.panelSize]);

  const tradindviewMaxHeight = max2XL ? 1200 : 600;

  const dataListMinHeight = canTrade ? 379 : 277;

  const splitSizeState = useSplitSize({ dep: layout });

  const tradingViewHeightState = useExtraHeight({
    orderEntryViewRef: observerState.orderEntryViewRef,
    tradindviewMaxHeight,
    dataListMinHeight,
  });

  const navigateToPortfolio =
    typeof onRouteChange === "function"
      ? () => onRouteChange({ href: "/portfolio", name: t("common.portfolio") })
      : undefined;

  const map = {
    layout,
    onLayout: setLayout,
    marketLayout,
    onMarketLayout: setMarketLayout,
    max2XL,
    min3XL,
    max4XL,
    canTrade,
    openMarketsSheet,
    onOpenMarketsSheetChange: setOpenMarketsSheet,
    horizontalDraggable,
    ...marketsCollapseState,
    ...positionsState,
    ...splitSizeState,
    ...observerState,
    restrictedInfo,
    ...tradingViewHeightState,
    marketsWidth,
    tradindviewMaxHeight,
    dataListMinHeight,
    total,
    hideAssets,
    setHideAssets,
    onShowPortfolioSheet,
    navigateToPortfolio,
    isFirstTimeDeposit,
    symbolInfoBarHeight,
  };

  return { ...props, ...map } as TradingPageState & typeof map;
};

const useMarketsCollapse = (options: { resizeable: boolean }) => {
  const { resizeable } = options;
  const [animating, setAnimating] = useState(false);

  const [panelSize, setPanelSize] = useLocalStorage<
    "small" | "middle" | "large"
  >(PAXEER_NETWORK_SIDE_MARKETS_MODE_KEY, "large");

  const onPanelSizeChange = (collapsed: "small" | "middle" | "large") => {
    setPanelSize(collapsed);
    setAnimating(true);
  };

  const memoizedPanelSize = useMemo<"small" | "middle" | "large">(() => {
    // Force only two states
    const normalized = panelSize === "large" ? "large" : "middle";
    // under 1440px markets force collapsed
    return resizeable ? normalized : "middle";
  }, [resizeable, panelSize]);

  return {
    resizeable: resizeable,
    panelSize: memoizedPanelSize,
    onPanelSizeChange: onPanelSizeChange,
    animating: animating,
    setAnimating: setAnimating,
  } as const;
};

const useOrderEntryPositions = (options: {
  canTrade: boolean;
  isFirstTimeDeposit: boolean;
}) => {
  const { canTrade, isFirstTimeDeposit } = options;

  const [positions, setPositions] = useLocalStorage(
    "paxeerNetwork_assets_orderEntry_margin_positions",
    [0, 1, 2],
  );

  const updatePositions = (currentIdx: number, targetIdx: number) => {
    const pos = [...positions];
    // [0,1,2] => [1,2,0]
    if (currentIdx === 0 && targetIdx === pos.length - 1) {
      pos[targetIdx] = positions[currentIdx];
      for (let i = 0; i < pos.length - 1; i++) {
        pos[i] = positions[i + 1];
      }

      // [0,1,2] => [2,0,1]
    } else if (currentIdx === pos.length - 1 && targetIdx === 0) {
      pos[targetIdx] = positions[currentIdx];
      for (let i = 1; i < pos.length; i++) {
        pos[i] = positions[i - 1];
      }
    } else {
      // [0,1,2] => [1,0,2], [0,1,2] => [0,2,1]
      [pos[currentIdx], pos[targetIdx]] = [pos[targetIdx], pos[currentIdx]];
    }
    setPositions(pos);
  };

  const showPositionIcon = useMemo(
    () => canTrade && !isFirstTimeDeposit,
    [canTrade, isFirstTimeDeposit],
  );

  const pos = useMemo(() => {
    return showPositionIcon ? (positions as number[]) : [0, 1, 2];
  }, [showPositionIcon, positions]);

  return {
    positions: pos,
    showPositionIcon,
    updatePositions,
  };
};

const useSplitSize = (options: { dep: any }) => {
  const { dep } = options;
  const [mainSplitSize, setMainSplitSize] = useSplitPersistent(
    "paxeerNetwork_main_split_size",
    `${orderEntryMinWidth}px`,
    dep,
  );
  const [dataListSplitSize, setDataListSplitSize] = useSplitPersistent(
    "paxeerNetwork_datalist_split_size",
    `${dataListInitialHeight}px`,
    // undefined,
  );
  const [orderBookSplitSize, setOrderbookSplitSize] = useSplitPersistent(
    "paxeerNetwork_orderbook_split_size",
    "280px",
    dep,
  );

  const [dataListSplitHeightSM, setDataListSplitHeightSM] = useSplitPersistent(
    "paxeerNetwork_datalist_split_height_sm",
    "350px",
  );

  const [orderBookSplitHeightSM, setOrderbookSplitHeightSM] =
    useSplitPersistent("paxeerNetwork_orderbook_split_height_sm", "280px");

  return {
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
  };
};

const useObserverOrderEntry = (options: { max2XL: boolean }) => {
  const { max2XL } = options;
  const [orderEntryHeight, setOrderEntryHeight] = useState(0);
  const orderEntryViewRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const element = orderEntryViewRef.current;

    if (!element || !max2XL) {
      return;
    }

    const resizeObserver = new ResizeObserver((entries) => {
      for (const entry of entries) {
        const height = entry.contentRect.height;
        if (height) {
          setOrderEntryHeight(height);
        }
      }
    });

    resizeObserver.observe(element);

    return () => {
      resizeObserver.unobserve(element);
    };
  }, [orderEntryViewRef, max2XL]);

  return {
    orderEntryViewRef,
    orderEntryHeight,
  };
};

export function getOffsetSizeNum(size: string | null) {
  if (size) {
    return `${100 - Math.min(Number(size), 100)}`;
  }
  return "";
}

function useExtraHeight(options: {
  orderEntryViewRef: RefObject<HTMLDivElement>;
  tradindviewMaxHeight: number;
  dataListMinHeight: number;
}) {
  const { tradindviewMaxHeight, dataListMinHeight } = options;
  const tradingviewAndOrderbookSplitRef = useRef<Split>(null);
  const max2XLSplitRef = useRef<Split>(null);

  const [extraHeight, setExtraHeight] = useLocalStorage(
    "paxeerNetwork_order_entry_extra_height",
    0,
  );

  const space = 10 + 12;

  const [dataListHeight, setDataListHeight] = useLocalStorage(
    "paxeerNetwork_trading_data_list_height",
    dataListMinHeight,
  );

  const onTradingviewAndOrderbookDragging = (
    preSize: number,
    nextSize: number,
  ) => {
    const boxHeight = tradingviewAndOrderbookSplitRef?.current?.boxHeight;
    if (!boxHeight) {
      return;
    }

    const splitTradingviewHeight = (boxHeight * preSize) / 100;
    const splitOrderbookHeight = (boxHeight * nextSize) / 100;

    const tradingviewHeight = Math.min(
      Math.max(splitTradingviewHeight, tradindviewMinHeight),
      tradindviewMaxHeight,
    );

    const orderbookHeight = Math.min(
      Math.max(splitOrderbookHeight, orderbookMinHeight),
      orderbookMaxHeight,
    );

    const orderEntryHeight =
      options.orderEntryViewRef.current?.clientHeight || 0;

    // console.log("tradingviewHeight", splitTradingviewHeight, tradingviewHeight);
    // console.log("orderbookHeight", splitOrderbookHeight, orderbookHeight);

    if (splitOrderbookHeight >= orderbookHeight) {
      const offset = splitOrderbookHeight - orderbookHeight;
      // console.log("offset ---", offset);
      setExtraHeight(Math.max(0, extraHeight - offset));
    } else if (
      tradingviewHeight + orderbookHeight <
      tradindviewMaxHeight + orderbookMaxHeight
    ) {
      const height =
        tradingviewHeight + orderbookHeight + space + symbolInfoBarHeight;

      const offset = Math.max(0, height - orderEntryHeight);
      // console.log("offset ++++", height, offset);
      setExtraHeight(extraHeight + offset);
    }
  };

  const onDataListSplitHeightDragging = (preSize: number, nextSize: number) => {
    const boxHeight = max2XLSplitRef?.current?.boxHeight;
    if (!boxHeight) {
      return;
    }

    // const splitTradingAndOrderbookHeight = (boxHeight * preSize) / 100;
    const splitDataListHeight = (boxHeight * nextSize) / 100;

    if (
      splitDataListHeight >= dataListMinHeight &&
      splitDataListHeight <= dataListMaxHeight
    ) {
      setDataListHeight(splitDataListHeight);
      const offset = splitDataListHeight - dataListHeight;
      if (offset > 0) {
        setExtraHeight(Math.max(0, extraHeight - offset));
      }
    }
  };

  return {
    max2XLSplitRef,
    tradingviewAndOrderbookSplitRef,
    onTradingviewAndOrderbookDragging,
    onDataListSplitHeightDragging,
    extraHeight,
    dataListHeight,
  };
}
