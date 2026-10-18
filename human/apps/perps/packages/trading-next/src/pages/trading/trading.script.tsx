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
import {
  OrderEntrySortKeys,
  TradingviewFullscreenKey,
} from "@paxeer-network/types";
import { AccountStatusEnum } from "@paxeer-network/types";
import { modal } from "@paxeer-network/ui";
import { PortfolioSheetWidget } from "../../components/mobile/portfolioSheet";
import { useTradingLocalStorage } from "../../hooks/";
import { useTradingPageContext } from "../../provider/tradingPageContext";
import { TradingPageState } from "../../types/types";
import { useFirstTimeDeposit } from "./hooks/useFirstTimeDeposit";
import {
  scrollBarWidth,
  topBarHeight,
  bottomBarHeight,
  space,
  symbolInfoBarHeight,
  orderEntryMinWidth,
  orderEntryMaxWidth,
  orderbookMinWidth,
  orderbookMaxWidth,
  orderbookMinHeight,
  orderbookMaxHeight,
  tradindviewMinHeight,
  tradingViewMinWidth,
  dataListMaxHeight,
  dataListInitialHeight,
} from "./trading.constants";

// Re-export constants and types so existing importers continue to work without changes.
export type { MarketLayoutPosition } from "./trading.constants";
export {
  scrollBarWidth,
  topBarHeight,
  bottomBarHeight,
  space,
  symbolInfoBarHeight,
  orderEntryMinWidth,
  orderEntryMaxWidth,
  orderbookMinWidth,
  orderbookMaxWidth,
  orderbookMinHeight,
  orderbookMaxHeight,
  tradindviewMinHeight,
  tradingViewMinWidth,
  dataListMaxHeight,
  dataListInitialHeight,
};

export type TradingState = ReturnType<typeof useTradingScript>;

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

  /** Order-entry sortable keys (margin, assets, orderEntry); used by desktop layout and split chrome */
  const [sortableItems, setSortableItems] = useLocalStorage<string[]>(
    OrderEntrySortKeys,
    ["margin", "assets", "orderEntry"],
  );

  const [tradingViewFullScreen] = useLocalStorage(
    TradingviewFullscreenKey,
    false,
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

  const observerState = useObserverOrderEntry({ max2XL });

  const tradindviewMaxHeight = max2XL ? 1200 : 600;

  const dataListMinHeight = canTrade ? 379 : 277;

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
    max2XL,
    min3XL,
    max4XL,
    canTrade,
    openMarketsSheet,
    onOpenMarketsSheetChange: setOpenMarketsSheet,
    horizontalDraggable,
    ...positionsState,
    ...observerState,
    restrictedInfo,
    ...tradingViewHeightState,
    tradindviewMaxHeight,
    dataListMinHeight,
    total,
    hideAssets,
    setHideAssets,
    onShowPortfolioSheet,
    navigateToPortfolio,
    isFirstTimeDeposit,
    symbolInfoBarHeight,
    sortableItems,
    setSortableItems,
    tradingViewFullScreen,
  };

  return { ...props, ...map } as TradingPageState & typeof map;
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
