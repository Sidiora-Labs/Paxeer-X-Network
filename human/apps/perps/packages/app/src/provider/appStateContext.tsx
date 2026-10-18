import React, { createContext, useContext } from "react";
import {
  RestrictedInfoReturns,
  type MarketCategoryConfig,
} from "@paxeer-network/hooks";
import type { WalletChainChangeState } from "@paxeer-network/types";
import { useWalletStateHandle } from "../hooks/useWalletStateHandle";

export type RouteOption = {
  href: "/portfolio" | "/portfolio/history" | "/perp";
  name: string;
};

export type WidgetConfigs = {
  scanQRCode?: {
    onSuccess?: (url: string) => void;
  };
  subAccount?: {
    /** @deprecated The number of custom sub-accounts needs to be configured in sync with the backend. If you’re not sure about it, please don’t set this value. */
    maxSubAccountCount: number;
  };
  withdraw?: {
    /**
     * Control the "withdraw to other wallet" feature.
     * - `true` / `undefined`: enable external wallet trigger & management.
     * - `false`: only show the connected wallet address without external
     *   wallet selector or add-wallet dialog.
     */
    enableWithdrawToExternalWallet?: boolean;
  };
  /**
   * Custom market tab configuration.
   * Function receives the default built-in tabs and context, returns the final tab sequence.
   */
  marketTabs?: MarketCategoryConfig;
};

export type AppContextState = {
  connectWallet: ReturnType<typeof useWalletStateHandle>["connectWallet"];
  /**
   * Whether the current network is not supported
   */
  wrongNetwork: boolean;
  disabledConnect: boolean;
  currentChainId: number | undefined;
  setCurrentChainId: (chainId: number | undefined) => void;
  onChainChanged?: (chainId: number, state: WalletChainChangeState) => void;
  // networkStatus: ReturnType<typeof useAppState>["networkStatus"];
  restrictedInfo: RestrictedInfoReturns;
  showAnnouncement: boolean;
  setShowAnnouncement: (show: boolean) => void;
  onRouteChange?: (option: RouteOption) => void;
  widgetConfigs?: WidgetConfigs;
  initialized: boolean;
};

export const AppStateContext = createContext<AppContextState>({
  setCurrentChainId: (chainId?: number) => {},
  restrictedInfo: {},
  setShowAnnouncement: (show: boolean) => {},
} as AppContextState);

export const useAppContext = () => {
  return useContext(AppStateContext);
};
