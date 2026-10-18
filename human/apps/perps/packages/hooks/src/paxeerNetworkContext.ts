/* eslint-disable @typescript-eslint/no-explicit-any */
import { createContext, useContext } from "react";
import type {
  ConfigStore,
  PaxeerNetworkKeyStore,
  WalletAdapter,
} from "@paxeer-network/core";
import type { API, NetworkId, PaxeerNetworkOrder } from "@paxeer-network/types";
import type { Chains } from "./paxeer-network/useChains";

export type FilteredChains = {
  mainnet?: { id: number }[];
  testnet?: { id: number }[];
};

export interface PaxeerNetworkConfigContextState {
  /** @deprecated will be removed in next minor version */
  fetcher?: (url: string, init: RequestInit) => Promise<any>;
  configStore: ConfigStore;
  keyStore: PaxeerNetworkKeyStore;
  walletAdapters: WalletAdapter[];

  networkId: NetworkId;

  filteredChains?: FilteredChains;
  /** custom chains, please include all chain information, otherwise there will be problems */
  customChains?: Chains<undefined, undefined>;
  /** enable swap deposit, default is false */
  enableSwapDeposit?: boolean;
  /**
   * Custom orderbook default tick sizes.
   */
  defaultOrderbookTickSizes?: Record<PropertyKey, string>;

  /**
   * Custom orderbook default symbol depths.
   */
  defaultOrderbookSymbolDepths?: Record<PropertyKey, number[]>;

  /** when use this, please keep the reference stable, otherwise it will cause unnecessary renders */
  dataAdapter?: {
    /**
     * custom useChains return list data
     */
    chainsList?: (chains: API.Chain[]) => API.Chain[];
    /**
     * Custom `/v1/public/futures` response data.
     */
    symbolList?: (
      data: API.MarketInfoExt[],
      context: {
        rwaSymbolsInfo: Record<string, API.RwaSymbol> | undefined;
      },
    ) => any[];
    /**
     * custom `/v2/public/announcement` response data
     */
    announcementList?: (data: any[] | ReadonlyArray<any>) => any[];
  };
  notification?: {
    orderFilled?: {
      /**
       * Sound to play when an order is successful.
       * If `soundOptions` is provided, this field is treated as the legacy
       * single-sound configuration and only used when `soundOptions` is
       * absent.
       * @default undefined
       */
      media?: string;
      /**
       * Whether to open the notification by default.
       * For multi-sound mode this controls whether the initial selection
       * should be sound-on or muted when there is no stored preference.
       * @default false
       */
      defaultOpen?: boolean;
      /**
       * Whether to display the notification in the order entry.
       * @default true
       */
      displayInOrderEntry?: boolean;
      /**
       * Multiple sound options for order filled notification.
       * When provided, the UI should render a single-choice selector
       * (e.g. radio group) instead of a simple on/off toggle. One of the
       * options should represent the muted/off state.
       */
      soundOptions?: Array<{
        label: string;
        value: string;
        media: string;
      }>;
      /**
       * Default selected sound option value when there is no stored
       * preference. If omitted, the first item in `soundOptions` is used.
       */
      defaultSoundValue?: string;
    };
  };

  amplitudeConfig?: {
    amplitudeId: string;
    serverZone?: "EU" | "US";
  };
  orderMetadata?: OrderMetadataConfig;
}

export type OrderMetadata = {
  order_tag?: string;
  client_order_id?: string;
};

export type OrderMetadataConfig =
  | OrderMetadata
  | ((order: Partial<PaxeerNetworkOrder>) => OrderMetadata);

export const PaxeerNetworkContext = createContext<PaxeerNetworkConfigContextState>({
  // configStore: new MemoryConfigStore(),
} as PaxeerNetworkConfigContextState);

export const usePaxeerNetworkContext = () => {
  return useContext(PaxeerNetworkContext);
};

export const PaxeerNetworkProvider = PaxeerNetworkContext.Provider;
