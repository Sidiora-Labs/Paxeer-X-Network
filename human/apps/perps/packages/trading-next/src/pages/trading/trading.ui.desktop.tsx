/**
 * Minimal desktop layout: builds panel registry and renders LayoutHost.
 * Chrome (markets, DnD, Flex) is provided by layout plugins (e.g. layout-split).
 */
import React, { useEffect, useMemo } from "react";
import {
  useBadgeBySymbol,
  useGetRwaSymbolOpenStatus,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import type { LayoutModel, LayoutStrategy } from "@paxeer-network/layout-core";
import { LayoutHost } from "@paxeer-network/layout-core";
import { API } from "@paxeer-network/types";
import { Box, cn } from "@paxeer-network/ui";
import {
  createTradingPanelRegistry,
  type TradingPanelRegistryProps,
} from "../../components/desktop/layout/TradingPanelRegistry";
import type { LayoutPosition } from "../../components/desktop/layout/switchLayout";
import { showRwaOutsideMarketHoursNotify } from "../../components/desktop/notify/rwaNotification";
import { useShowRwaCountdown } from "../../hooks/useShowRwaCountdown";
import { useTradingPageContext } from "../../provider/tradingPageContext";
import type { DesktopLayoutInitialOptions } from "../../types/types";
import {
  dataListInitialHeight,
  scrollBarWidth,
  topBarHeight,
  bottomBarHeight,
  space,
  orderbookMinHeight,
  orderbookMaxHeight,
  tradindviewMinHeight,
  dataListMaxHeight,
} from "./trading.constants";
import type { TradingState } from "./trading.script";

/** Stable noops to avoid useMemo invalidation when layout props are undefined. */
const NOOP = () => {};
const NOOP_LAYOUT = (() => {}) as (v: LayoutPosition) => void;
const NOOP_MARKET_LAYOUT = (() => {}) as (
  v: "left" | "top" | "bottom" | "hide",
) => void;
/** Stable style object to avoid LayoutHost re-renders from inline object ref change. */
const LAYOUT_HOST_STYLE = { flex: 1, minHeight: 0 } as const;

export type DesktopLayoutProps = TradingState & {
  className?: string;
  /** Injected by layout plugin interceptor when not provided by host */
  layoutStrategy?: LayoutStrategy;
  getInitialLayout?: (
    options: DesktopLayoutInitialOptions,
  ) => LayoutModel | undefined;
  /** Optional storage key for layout persistence; when provided (e.g. by grid plugin) overrides default. */
  storageKey?: string;
  /**
   * When true, disables LayoutHost localStorage persistence.
   * Useful in development when layout should reset on every refresh.
   */
  disableLayoutPersistence?: boolean;
  /** Optional layout props when plugin provides rule-based layout; defaults used for SwitchLayout. */
  layout?: LayoutPosition;
  marketLayout?: "left" | "top" | "bottom" | "hide";
  onLayout?: (v: LayoutPosition) => void;
  onMarketLayout?: (v: "left" | "top" | "bottom" | "hide") => void;
  resizeable?: boolean;
  panelSize?: "small" | "middle" | "large";
  onPanelSizeChange?: (v: "small" | "middle" | "large") => void;
  animating?: boolean;
  setAnimating?: (v: boolean) => void;
};

/**
 * Desktop layout core: defines panels and renders LayoutHost.
 * Strategy and initial layout come from context (injected by host or layout plugin).
 */
export const DesktopLayout: React.FC<DesktopLayoutProps> = (props) => {
  const {
    max2XL,
    dataListMinHeight,
    tradingViewFullScreen,
    // dataListSplitHeightSM,
  } = props;
  /* Layout props optional when plugin provides rule-based layout; use defaults for SwitchLayout. */
  const layout = props.layout ?? "right";
  const marketLayout = props.marketLayout ?? "left";
  const onLayout = props.onLayout ?? NOOP_LAYOUT;
  const onMarketLayout = props.onMarketLayout ?? NOOP_MARKET_LAYOUT;
  const resizeable = props.resizeable ?? false;
  const panelSize = props.panelSize ?? "large";
  const onPanelSizeChange = props.onPanelSizeChange ?? NOOP;
  const animating = props.animating ?? false;
  const setAnimating = props.setAnimating ?? NOOP;

  const { brokerName } = useBadgeBySymbol(props.symbol);

  const { showCountdown, closeCountdown } = useShowRwaCountdown(props.symbol);
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

  const minScreenHeight = useMemo(
    () =>
      tradingViewFullScreen
        ? 0
        : symbolInfoBarHeight +
          orderbookMaxHeight +
          dataListInitialHeight +
          space * 4,
    [tradingViewFullScreen, symbolInfoBarHeight],
  );

  const minScreenHeightSM =
    topBarHeight +
    bottomBarHeight +
    symbolInfoBarHeight +
    tradindviewMinHeight +
    orderbookMinHeight +
    dataListMinHeight +
    space * 4;

  const disableLayoutPersistence = props.disableLayoutPersistence ?? false;

  const languageMap = useMemo(() => {
    return {
      "common.markets": t("common.markets"),
    };
  }, [t]);

  const dataListHeight = useMemo(() => {
    return {
      height: 0,
      minHeight: Math.max(dataListMinHeight, 0),
      maxHeight: dataListMaxHeight,
    };
  }, [dataListMinHeight, 0]);

  const registryProps = useMemo<TradingPanelRegistryProps>(
    () => ({
      symbol: props.symbol,
      onSymbolChange: (sym: unknown) =>
        props.onSymbolChange?.(sym as API.Symbol),
      tradingViewConfig: props.tradingViewConfig,
      disableFeatures: props.disableFeatures,
      navigateToPortfolio: props.navigateToPortfolio,
      sharePnLConfig: props.sharePnLConfig,
      isFirstTimeDeposit: props.isFirstTimeDeposit,
      resizeable,
      panelSize,
      onPanelSizeChange,
      layout,
      onLayout,
      marketLayout,
      onMarketLayout,
      symbolInfoBarHeight,
      showCountdown: showCountdown ?? false,
      closeCountdown,
      tradingViewFullScreen,
      animating,
      setAnimating,
      dataListHeight,
      languageMap,
    }),
    [
      props.symbol,
      props.onSymbolChange,
      props.tradingViewConfig,
      props.disableFeatures,
      props.navigateToPortfolio,
      props.sharePnLConfig,
      props.isFirstTimeDeposit,
      resizeable,
      panelSize,
      onPanelSizeChange,
      layout,
      onLayout,
      marketLayout,
      onMarketLayout,
      symbolInfoBarHeight,
      showCountdown,
      closeCountdown,
      tradingViewFullScreen,
      animating,
      setAnimating,
      dataListHeight,
      languageMap,
    ],
  );

  const panels = useMemo(
    () => createTradingPanelRegistry(registryProps, languageMap),
    [registryProps, languageMap],
  );

  const contextLayout = useTradingPageContext();
  /** Prefer props (injected by layout plugin interceptor) over context */
  const layoutStrategy = props.layoutStrategy ?? contextLayout.layoutStrategy;
  const getInitialLayout =
    props.getInitialLayout ?? contextLayout.getInitialLayout;
  const panelIds = useMemo(() => Array.from(panels.keys()), [panels]);

  const initialLayout = useMemo(() => {
    if (getInitialLayout) {
      return getInitialLayout({
        variant: max2XL ? "max2XL" : "default",
      });
    }
    if (layoutStrategy) {
      return layoutStrategy.defaultLayout(panelIds);
    }
    return undefined;
  }, [getInitialLayout, layoutStrategy, max2XL, panelIds]);

  const layoutStorageKey = disableLayoutPersistence
    ? undefined
    : (props.storageKey ??
      (max2XL
        ? "paxeerNetwork_trading_desktop_layout_sm"
        : "paxeerNetwork_trading_desktop_layout"));

  const defaultLayoutHostClassName = cn(
    "oui-flex oui-flex-1 oui-overflow-hidden",
    max2XL && "oui-size-full oui-min-w-[1018px] oui-px-3 oui-py-2",
  );
  const defaultLayoutHostStyle = max2XL
    ? {
        minHeight: minScreenHeightSM,
        minWidth: 1024 - scrollBarWidth,
      }
    : { flex: 1, minHeight: minScreenHeight };

  if (!layoutStrategy) {
    return (
      <div
        className={cn(defaultLayoutHostClassName, props.className)}
        style={defaultLayoutHostStyle}
      >
        <div className="oui-flex oui-flex-1 oui-items-center oui-justify-center oui-text-base-4">
          Desktop layout requires layoutStrategy (e.g. split or grid) from the
          consumer.
        </div>
      </div>
    );
  }

  return (
    <Box
      className={cn("oui-flex oui-flex-1 oui-overflow-hidden", props.className)}
      style={defaultLayoutHostStyle}
    >
      <LayoutHost
        strategy={layoutStrategy}
        panels={panels}
        initialLayout={initialLayout}
        storageKey={layoutStorageKey}
        className="oui-flex oui-flex-1 oui-overflow-hidden"
        style={LAYOUT_HOST_STYLE}
      />
    </Box>
  );
};
