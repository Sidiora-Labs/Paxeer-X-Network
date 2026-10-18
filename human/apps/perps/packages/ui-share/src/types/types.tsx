import { MarginMode } from "@paxeer-network/types";

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

export type SharePnLConfig = SharePnLOptions &
  Partial<Omit<SharePnLParams, "refCode">>;

export type SharePnLParams = {
  entity: ShareEntity;
  refCode?: string;
  refSlogan?: string;
  refLink?: string;
  brokerName?: string;
};

export type SharePnLOptions = {
  /**
   * default is Manrope
   */
  fontFamily?: string;
  /**
   * can not empty
   */
  backgroundImages?: string[];
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

export type ReferralType = {
  code?: string;
  link?: string;
  slogan?: string;
};

export type PnLDisplayFormat = "roi_pnl" | "roi" | "pnl";
export type ShareOptions =
  | "openPrice"
  | "closePrice"
  | "openTime"
  | "closeTime"
  | "markPrice"
  | "quantity"
  | "leverage";

export type ShareEntity = {
  symbol: string;
  side: "LONG" | "SHORT";
  marginMode?: MarginMode;
  pnl?: number;
  roi?: number;
  openPrice?: number;
  closePrice?: number;
  openTime?: number;
  closeTime?: number;
  markPrice?: number;
  quantity?: number;
  leverage?: number;
};
