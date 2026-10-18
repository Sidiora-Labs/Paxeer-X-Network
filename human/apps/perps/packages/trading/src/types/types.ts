import { ReactNode } from "react";
import { API } from "@paxeer-network/types";
import { SharePnLConfig } from "@paxeer-network/ui-share";
import { TradingviewWidgetPropsInterface } from "@paxeer-network/ui-tradingview";

export type layoutInfo = {
  width?: number;
  height?: number;
  // padding?: number;
  // margin?: number;
  fontSize?: number;

  color?: string;
  textAlign?: CanvasTextAlign;
  textBaseline?: CanvasTextBaseline;
  position: Partial<{
    left: number;
    right: number;
    top: number;
    bottom: number;
  }>;
};

export type PosterLayoutConfig = {
  message?: layoutInfo;

  domain?: layoutInfo;
  position?: layoutInfo;
  unrealizedPnl?: layoutInfo & {
    secondaryColor: string;
    secondaryFontSize: number;
  };

  informations?: layoutInfo & {
    labelColor?: string;
  };
  updateTime?: layoutInfo;
};

export type ShareOptions = {
  pnl: {
    /**
     * defualt is Manrope
     */
    fontFamily?: string;
    /**
     * can not empty
     */
    backgroundImages: string[];
    /**
     * posterLayoutConfig
     */
    layout?: PosterLayoutConfig;
    // normal text color
    /**
     * norma text color, default is  "rgba(255, 255, 255, 0.98)"
     */
    color?: string;
    /**
     * profit text color, default is "rgb(0,181,159)"
     */
    profitColor?: string;
    /**
     * loss text color, default is  "rgb(255,103,194)"
     */
    lossColor?: string;
    /**
     * brand color, default is "rgb(0,181,159)"
     */
    brandColor?: string;
  };
};

export enum TradingFeatures {
  Sider = "sider",
  TopNavBar = "topNavBar",
  Footer = "footer",
  Header = "header",
  Kline = "kline",
  OrderBook = "orderBook",
  TradeHistory = "tradeHistory",
  Positions = "positions",
  Orders = "orders",
  AssetAndMarginInfo = "asset_margin_info",
  SlippageSetting = "slippageSetting",
  FeesInfo = "feesInfo",
}

export type BasicSymbolInfo = {
  base_dp: number;
  quote_dp: number;
  base_tick: number;
  base: string;
  quote: string;
};

export interface TradingPageState extends TradingPageProps {
  symbolInfo: {
    base_dp: number;
    quote_dp: number;
    base_tick: number;
    base: string;
    quote: string;
    symbol: string;
  };
}

export interface TradingViewConfigInterface {
  scriptSRC?: string;
  library_path: string;
  overrides?: TradingviewWidgetPropsInterface["overrides"];
  studiesOverrides?: TradingviewWidgetPropsInterface["studiesOverrides"];
  customCssUrl?: string;
  colorConfig?: ColorConfigInterface;
  theme?: TradingviewWidgetPropsInterface["theme"];
  locale?: TradingviewWidgetPropsInterface["locale"];
  enabled_features?: string[];
  disabled_features?: string[];
}

export interface ColorConfigInterface {
  chartBG?: string;
  upColor?: string;
  downColor?: string;
  pnlUpColor?: string;
  pnlDownColor?: string;
  pnlZeroColor?: string;
  /** @deprecated Use pnlZeroColor instead. */
  pnlZoreColor?: string;
  textColor?: string;
  qtyTextColor?: string;
  font?: string;
  closeIconColor?: string;
  /** @deprecated Use closeIconColor instead. */
  closeIcon?: string;
}

export type ReferralProps = {
  saveRefCode?: boolean;
  onClickReferral?: () => void;
  onBoundRefCode?: (success: boolean, error: any) => void;
};

export type TradingRewardsProps = {
  onClickTradingRewards?: () => void;
};

type BaseTradingPageProps = {
  symbol: string;
  tradingViewConfig: TradingViewConfigInterface;
  onSymbolChange?: (symbol: API.Symbol) => void;
  // enableFeatures?: TradingFeatures[];
  // for trading page features, not for tradingView chart features
  disableFeatures?: TradingFeatures[];
  // for trading page features, not for tradingView chart features
  overrideFeatures?: Record<TradingFeatures, ReactNode>;
};

export type TradingPageProps = BaseTradingPageProps & {
  sharePnLConfig?: SharePnLConfig;
  referral?: ReferralProps;
  tradingRewards?: TradingRewardsProps;
  bottomSheetLeading?: React.ReactNode | string;
};
