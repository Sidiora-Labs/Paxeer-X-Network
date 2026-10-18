import { LocaleCode } from "@paxeer-network/i18n";
import {
  LanguageCode,
  LoadingScreenOptions,
  StudyOverrides,
  Overrides,
  type ChartingLibraryWidgetOptions,
  type IChartingLibraryWidget,
} from "./tradingviewAdapter/charting_library";
import { ChartMode, ColorConfigInterface } from "./tradingviewAdapter/type";

export type TradingviewLocaleCode = LanguageCode;
export type TradingviewDirection = "ltr" | "rtl";

export interface TradingviewWidgetPropsInterface {
  symbol?: string;
  mode?: ChartMode;
  scriptSRC?: string;
  library_path?: string;
  overrides?: Overrides;
  studiesOverrides?: StudyOverrides;
  customCssUrl?: string;
  colorConfig?: ColorConfigInterface;
  libraryPath?: string;
  fullscreen?: boolean;
  theme?: "dark" | "light";
  loadingScreen?: LoadingScreenOptions;
  /**
   * The locale of the tradingview.
   * If locale does not match in TradingviewLocaleCode, it will default to "en".
   * If locale is a function, you can use the current locale to return the TradingviewLocaleCode.
   */
  locale?:
    | TradingviewLocaleCode
    | ((localeCode: LocaleCode) => TradingviewLocaleCode);
  onFullScreenChange?: (isFullScreen: boolean) => void;
  classNames?: {
    root?: string;
    content?: string;
  };
  /**
   * Additional enabled features to merge with default enabled features
   */
  enabled_features?: string[];
  /**
   * Additional disabled features to merge with default disabled features
   */
  disabled_features?: string[];
  /**
   * Returns a Promise of custom indicators. PineJS is passed as the first argument for use inside indicators.
   * @see https://www.tradingview.com/charting-library-docs/latest/custom_studies/custom_studies.md
   */
  customIndicatorsGetter?: ChartingLibraryWidgetOptions["custom_indicators_getter"];
}

export interface TradingviewUIPropsInterface {
  tradingViewScriptSrc?: string;
  chartRef: React.Ref<HTMLDivElement>;
  interval?: string;
  changeDisplaySetting: (setting: DisplayControlSettingInterface) => void;
  displayControlState: DisplayControlSettingInterface;
  changeInterval: (newInterval: string) => void;
  lineType: string;
  changeLineType: (newLineType: string) => void;
  openChartSetting: () => void;
  openChartIndicators: () => void;
  symbol?: string;
  onFullScreenChange: () => void;
  classNames?: {
    root?: string;
    content?: string;
  };
  direction?: TradingviewDirection;
  fullscreen?: boolean;
  /** Live charting widget, available once the chart is ready. */
  readyWidget?: IChartingLibraryWidget | null;
}

export interface DisplayControlSettingInterface {
  position: boolean;
  buySell: boolean;
  limitOrders: boolean;
  stopOrders: boolean;
  tpsl: boolean;
  positionTpsl: boolean;
  trailingStop: boolean;
  liquidationPrice: boolean;
}
