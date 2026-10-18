import { ColorConfigInterface } from "../tradingviewAdapter/type";

const font = "regular 11px Manrope";

/** default dark theme color config */
export const defaultColorConfig: Record<
  "dark" | "light",
  ColorConfigInterface
> = {
  dark: {
    chartBG: "#131519",
    upColor: "#008676",
    downColor: "#D92D6B",
    pnlUpColor: "#00B49E",
    pnlDownColor: "#FF447C",
    pnlZeroColor: "#333948",
    pnlZoreColor: "#333948",
    textColor: "#FFFFFF",
    qtyTextColor: "#F4F7F9",
    font,
    volumeUpColor: "#0C3E3A",
    volumeDownColor: "#5A1E36",
    closeIconColor: "rgba(255, 255, 255, 0.8)",
    closeIcon: "rgba(255, 255, 255, 0.8)",
  },
  light: {
    chartBG: "#ffffff",
    upColor: "#2ebd85",
    downColor: "#F6465D",
    pnlUpColor: "#2ebd85",
    pnlDownColor: "#F6465D",
    pnlZeroColor: "#333948",
    pnlZoreColor: "#333948",
    textColor: "#000000",
    qtyTextColor: "#000000",
    font,
    volumeUpColor: "#2ebd85",
    volumeDownColor: "#F6465D",
    closeIconColor: "rgba(0, 0, 0, 0.8)",
    closeIcon: "rgba(0, 0, 0, 0.8)",
  },
};

export const getColorConfig = ({
  theme,
  cssVariables,
  customerColorConfig,
  themeColorConfig,
}: {
  theme: "dark" | "light";
  cssVariables: Record<string, string>;
  customerColorConfig?: ColorConfigInterface;
  themeColorConfig?: ColorConfigInterface;
}) => {
  const resolvedColorConfig = {
    ...customerColorConfig,
    ...themeColorConfig,
  };
  const defaultCconfig = defaultColorConfig[theme] || defaultColorConfig.dark;
  const chartBG =
    resolvedColorConfig.chartBG ||
    cssVariables.chartBG ||
    defaultCconfig.chartBG;
  const pnlZeroColor =
    resolvedColorConfig.pnlZeroColor ??
    resolvedColorConfig.pnlZoreColor ??
    defaultCconfig.pnlZeroColor ??
    defaultCconfig.pnlZoreColor;
  const closeIconColor =
    resolvedColorConfig.closeIconColor ??
    resolvedColorConfig.closeIcon ??
    defaultCconfig.closeIconColor ??
    defaultCconfig.closeIcon;

  // dark mode incompatible
  // const upColor =
  //   customerColorConfig?.upColor ||
  //   cssVariables.upColor ||
  //   defaultCconfig.upColor;

  // dark mode incompatible
  // const downColor =
  //   customerColorConfig?.downColor ||
  //   cssVariables.downColor ||
  //   defaultCconfig.downColor;

  /** Liquidation line uses same color as Position list Liq. Price (--oui-color-warning-light). */
  const liqLineColor =
    resolvedColorConfig.liqLineColor ?? cssVariables.warningLight;

  return {
    ...defaultCconfig,
    ...resolvedColorConfig,
    chartBG,
    pnlZeroColor,
    pnlZoreColor: pnlZeroColor,
    closeIconColor,
    // Keep the legacy field populated for consumers that still read it.
    closeIcon: closeIconColor,
    liqLineColor,
    // upColor,
    // downColor,
  };
};

export const defaultOverrides = {
  dark: {
    "paneProperties.separatorColor": "#2B2833",
    "paneProperties.vertGridProperties.color": "#26232F",
    "paneProperties.horzGridProperties.color": "#26232F",
    "scalesProperties.textColor": "#97969B",
  },
  light: {
    // "paneProperties.separatorColor": "#E5E5E5",
    // "paneProperties.vertGridProperties.color": "#E5E5E5",
    // "paneProperties.horzGridProperties.color": "#E5E5E5",
    // "scalesProperties.textColor": "#97969B",
    "scalesProperties.crosshairLabelBgColorLight": "#E5E5E5",
  },
};

export const getOveriides = ({
  theme,
  colorConfig,
  isMobile,
}: {
  theme: "dark" | "light";
  colorConfig: ColorConfigInterface;
  isMobile?: boolean;
}) => {
  const overrides = {
    "paneProperties.background": colorConfig.chartBG,
    // "mainSeriesProperties.style": 1,
    "paneProperties.backgroundType": "solid",
    "mainSeriesProperties.candleStyle.upColor": colorConfig.upColor,
    "mainSeriesProperties.candleStyle.downColor": colorConfig.downColor,
    "mainSeriesProperties.candleStyle.borderColor": colorConfig.upColor,
    "mainSeriesProperties.candleStyle.borderUpColor": colorConfig.upColor,
    "mainSeriesProperties.candleStyle.borderDownColor": colorConfig.downColor,
    "mainSeriesProperties.candleStyle.wickUpColor": colorConfig.upColor,
    "mainSeriesProperties.candleStyle.wickDownColor": colorConfig.downColor,
    "scalesProperties.fontSize": isMobile ? 8 : 12,
    "paneProperties.legendProperties.showSeriesTitle": isMobile ? false : true,
    "mainSeriesProperties.statusViewStyle.symbolTextSource": "ticker",
    ...defaultOverrides[theme],
  };
  const studiesOverrides = {
    "volume.volume.color.0": colorConfig.volumeDownColor,
    "volume.volume.color.1": colorConfig.volumeUpColor,
  };

  return {
    overrides,
    studiesOverrides,
  } as const;
};

export const EXCHANGE = "paxeer-network";
export const withoutExchangePrefix = (symbol: string) =>
  symbol.includes(":") ? symbol.split(":")[1] : symbol;

export const withExchangePrefix = (symbol: string) =>
  symbol.startsWith(`${EXCHANGE}:`) ? symbol : `${EXCHANGE}:${symbol}`;

/** Deterministic JSON stringify with sorted keys (no third-party deps). */
function stringifyWithSortedKeys(obj: object): string {
  const keys = Object.keys(obj).sort();
  const sorted: Record<string, any> = {};
  for (const k of keys) {
    sorted[k] = (obj as Record<string, any>)[k];
  }
  return JSON.stringify(sorted);
}

/** Simple deterministic hash (djb2-style, no third-party deps). Returns short alphanumeric string. */
function simpleHash(str: string): string {
  let h = 5381;
  for (let i = 0; i < str.length; i++) {
    h = (h << 5) + h + str.charCodeAt(i);
    h = h >>> 0;
  }
  return Math.abs(h).toString(36);
}

/**
 * Stable hash from overrides and studiesOverrides so chartKey changes when config changes.
 * Uses only built-in JSON + hand-written hash (no third-party libs).
 */
export function getOverridesConfigHash(
  theme: "dark" | "light",
  overrides: object,
  studiesOverrides: object,
): string {
  const payload = `${theme}${stringifyWithSortedKeys(overrides)}${stringifyWithSortedKeys(studiesOverrides)}`;
  return simpleHash(payload);
}
