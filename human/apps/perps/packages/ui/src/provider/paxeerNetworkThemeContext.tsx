import { createContext, useContext } from "react";
import type { ThemeCssVars } from "../tailwind";

export type ComponentOverrides = {
  tabs: {
    variant: "text" | "contained";
  };
  chainSelector: {
    /**
     * show testnet chains list
     * @default true
     * */
    showTestnet: boolean;
  };
};

export type TradingViewColorConfig = {
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
  volumeUpColor?: string;
  volumeDownColor?: string;
  /** Liquidation line color; should match Position list Liq. Price (e.g. from --oui-color-warning-light). */
  liqLineColor?: string;
};

export type ThemeConfig = {
  /**
   * Unique theme identifier.
   * Will also be used as the value of `data-oui-theme`.
   */
  id: string;
  /**
   * Display name for this theme. Used in UI (e.g. settings page).
   */
  displayName: string;
  /**
   * Theme mode.
   */
  mode: "dark" | "light";
  /**
   * Optional CSS variables overrides for this theme.
   * Keys should be full CSS variable names, e.g. "--oui-color-primary".
   * These are applied at runtime via document.documentElement.style.setProperty.
   */
  cssVars?: Partial<ThemeCssVars>;
  /** Optional TradingView color overrides for this theme. */
  tradingViewColorConfig?: TradingViewColorConfig;
  /** Whether this theme should be used when no valid stored theme exists. */
  isDefault?: boolean;
};

type GetComponentTheme = <T extends keyof ComponentOverrides>(
  component: T,
  defaultValue?: ComponentOverrides[T],
) => ComponentOverrides[T];

export type PaxeerNetworkThemeContextState = {
  // overrides?: Partial<ComponentOverrides>;
  getComponentTheme: GetComponentTheme;
  themes: ThemeConfig[];
  currentThemeId?: string;
  currentTheme?: ThemeConfig;
  setCurrentThemeId?: (id: string) => void;
};

export const PaxeerNetworkThemeContext = createContext(
  {} as PaxeerNetworkThemeContextState,
);

export const usePaxeerNetworkTheme = () => {
  return useContext(PaxeerNetworkThemeContext);
};
