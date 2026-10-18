import type { FC, PropsWithChildren } from "react";
import React, { useLayoutEffect, useMemo } from "react";
import {
  ConfigStore,
  // MemoryConfigStore,
  PaxeerNetworkKeyStore,
  LocalStorageStore,
  SimpleDI,
  Account,
  IContract,
  WalletAdapter,
} from "@paxeer-network/core";
import { DefaultEVMWalletAdapter } from "@paxeer-network/default-evm-adapter";
import { DefaultSolanaWalletAdapter } from "@paxeer-network/default-solana-adapter";
import { NetworkId } from "@paxeer-network/types";
import { SDKError } from "@paxeer-network/types";
import { EthersProvider } from "@paxeer-network/web3-provider-ethers";
import { DEFAULT_SYMBOL_DEPTHS, DEFAULT_TICK_SIZES } from "./constants";
import { ProxyConfigStore } from "./dev/proxyConfigStore";
import { ExtendedConfigStore } from "./extendedConfigStore";
import {
  FilteredChains,
  PaxeerNetworkConfigContextState,
  PaxeerNetworkProvider,
} from "./paxeerNetworkContext";
// import { usePreLoadData } from "./usePreloadData";
import { DataCenterProvider } from "./provider/dataCenter/dataCenterProvider";
import { StatusProvider } from "./provider/status/statusProvider";

// import { useParamsCheck } from "./useParamsCheck";

export type ChainFilterFunc = (config: ConfigStore) => FilteredChains;

export type ChainFilter = FilteredChains | ChainFilterFunc;

export type BaseConfigProviderProps = {
  keyStore?: PaxeerNetworkKeyStore;
  contracts?: IContract;
  walletAdapters?: WalletAdapter[];
  /** filter chains, only show chains in the filter */
  chainFilter?: ChainFilter;
  /**
   * Custom orderbook default tick sizes.
   */
  orderbookDefaultTickSizes?: Record<string, string>;
  /**
   * Custom orderbook default symbol depths.
   */
  orderbookDefaultSymbolDepths?: Record<PropertyKey, number[]>;
} & Pick<
  PaxeerNetworkConfigContextState,
  | "enableSwapDeposit"
  | "customChains"
  | "dataAdapter"
  | "notification"
  | "amplitudeConfig"
  | "orderMetadata"
>;

export type ExclusiveConfigProviderProps =
  | {
      brokerId: string;
      brokerName?: string;
      networkId: NetworkId;
      configStore?: never;
    }
  | {
      brokerId?: never;
      brokerName?: never;
      networkId?: never;
      configStore: ConfigStore;
    };

export type ConfigProviderProps = BaseConfigProviderProps &
  ExclusiveConfigProviderProps;

export const PaxeerNetworkConfigProvider: FC<
  PropsWithChildren<ConfigProviderProps>
> = (props) => {
  const [account, setAccount] = React.useState<Account | null>(null);
  const {
    configStore,
    keyStore,
    walletAdapters,
    brokerId,
    brokerName,
    networkId,
    contracts,
    chainFilter,
    customChains,
    enableSwapDeposit = false,
    dataAdapter,
    notification,
    amplitudeConfig,
    orderbookDefaultTickSizes,
    orderbookDefaultSymbolDepths,
    children,
    orderMetadata,
  } = props;

  if (!brokerId && typeof configStore === "undefined") {
    console.error("[PaxeerNetworkConfigProvider]: brokerId is required");
  }

  if (typeof configStore !== "undefined" && !configStore.get("brokerId")) {
    // console.error("[PaxeerNetworkConfigProvider]: brokerId is required");
    throw new SDKError(
      "if configStore is provided, brokerId is required in configStore",
    );
  }

  if (
    typeof brokerId !== "undefined" &&
    typeof configStore !== "undefined" &&
    brokerId !== (configStore as ConfigStore).get("brokerId")
  ) {
    throw new SDKError(
      "If you have provided a custom `configStore` and the `brokerId` is set in the `configStore`, please remove the `brokerId` prop.",
    );
  }

  const innerConfigStore = useMemo<ConfigStore>(() => {
    return new ProxyConfigStore(
      configStore ||
        new ExtendedConfigStore({
          brokerId,
          brokerName,
          networkId,
        }),
    );
  }, [configStore, brokerId, brokerName, networkId]);

  const innerKeyStore = useMemo<PaxeerNetworkKeyStore>(() => {
    if (keyStore) return keyStore;

    const networkId = innerConfigStore.get<NetworkId>("networkId");
    return new LocalStorageStore(networkId);
  }, [innerConfigStore, keyStore]);

  const innerWalletAdapters = useMemo<WalletAdapter[]>(() => {
    return (
      walletAdapters || [
        new DefaultEVMWalletAdapter(new EthersProvider()),
        new DefaultSolanaWalletAdapter(),
      ]
    );
  }, [walletAdapters]);

  const defaultOrderbookTickSizes = useMemo(() => {
    return orderbookDefaultTickSizes || DEFAULT_TICK_SIZES;
  }, [orderbookDefaultTickSizes]);

  const defaultOrderbookSymbolDepths = useMemo(() => {
    return orderbookDefaultSymbolDepths || DEFAULT_SYMBOL_DEPTHS;
  }, [orderbookDefaultSymbolDepths]);

  // check params, if has mismatch, throw warning message to console
  // useParamsCheck({ brokerId: innerConfigStore.get("brokerId") });

  useLayoutEffect(() => {
    let account = SimpleDI.get<Account>(Account.instanceName);

    if (!account) {
      account = new Account(
        innerConfigStore,
        innerKeyStore,
        // innerGetWalletAdapter,
        innerWalletAdapters,
        {
          contracts,
        },
      );

      SimpleDI.registerByName(Account.instanceName, account);
    }

    setAccount(account);
  }, []);

  const filteredChains = useMemo(() => {
    if (typeof chainFilter === "function") {
      return chainFilter(innerConfigStore);
    }
    return chainFilter;
  }, [chainFilter, innerConfigStore]);

  const memoizedValue = useMemo<PaxeerNetworkConfigContextState>(() => {
    return {
      configStore: innerConfigStore,
      keyStore: innerKeyStore,
      networkId: innerConfigStore.get("networkId") || networkId,
      filteredChains,
      walletAdapters: innerWalletAdapters,
      customChains,
      enableSwapDeposit,
      defaultOrderbookTickSizes,
      defaultOrderbookSymbolDepths,
      dataAdapter,
      notification: notification,
      amplitudeConfig,
      orderMetadata,
    };
  }, [
    innerConfigStore,
    innerKeyStore,
    networkId,
    filteredChains,
    innerWalletAdapters,
    customChains,
    enableSwapDeposit,
    defaultOrderbookTickSizes,
    defaultOrderbookSymbolDepths,
    dataAdapter,
    notification,
    amplitudeConfig,
    orderMetadata,
  ]);

  if (!account) {
    return null;
  }

  return (
    <PaxeerNetworkProvider value={memoizedValue}>
      <StatusProvider>
        <DataCenterProvider>{children}</DataCenterProvider>
      </StatusProvider>
    </PaxeerNetworkProvider>
  );
};
