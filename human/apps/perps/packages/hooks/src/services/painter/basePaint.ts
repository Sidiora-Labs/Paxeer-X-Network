import { MarginMode } from "@paxeer-network/types";
import { PosterPainter } from "./painter";

export type posterDataSource = {
  /**
   * slogan of the poster
   */
  message?: string;
  position: {
    symbol: string;
    brokerName?: string;
    // side: OrderSide;
    side: "LONG" | "SHORT";
    marginMode?: MarginMode;
    /**
     * The leverage of the position
     */
    leverage: number;
    /**
     * The unrealized PnL of the position
     */
    pnl: number;
    /**
     * The return on investment of the position
     */
    ROI: number;
    /**
     * The informations of the position, such as open price, opened at, mark price, quantity and custom message.
     */
    informations: { title: string; value: string }[];
    /**
     * The quote currency of the position
     */
    currency: string;
  };
  /**
   * The domain of the application
   */
  domain: string;

  /**
   * The update time of the position
   */
  updateTime: string;

  referral?: {
    code: string;
    slogan: string;
    link: string;
  } | null;
};

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

export type DrawOptions = {
  direction?: "ltr" | "rtl";
  /**
   * Color of common text
   */
  color?: string;
  fontFamily?: string;
  /**
   * Lose color
   */
  lossColor?: string;
  /**
   * Profit color
   */
  profitColor?: string;
  /**
   * The brand color of the application
   */
  brandColor?: string;
  backgroundColor?: string;
  backgroundImg?: string;
  data?: posterDataSource;
  layout?: PosterLayoutConfig;
};

export abstract class BasePaint {
  constructor(
    protected ctx: CanvasRenderingContext2D,
    protected painter: PosterPainter,
  ) {}
  abstract draw(options: DrawOptions): Promise<void>;
}
