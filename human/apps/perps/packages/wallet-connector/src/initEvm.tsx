import React, { PropsWithChildren, useEffect, useRef, useState } from "react";
import type { InitOptions, OnboardAPI } from "@web3-onboard/core";
import { merge } from "lodash";
import {
  useMainnetChainsStore,
  useSimpleDI,
  useTestnetChainsStore,
} from "@paxeer-network/hooks";
import { Optional, SolanaChains } from "@paxeer-network/types";
import { initConfig } from "./config";

export type ConnectorInitOptions = Optional<
  InitOptions,
  | "apiKey"
  | "connect"
  | "wallets"
  | "chains"
  | "appMetadata"
  | "accountCenter"
  | "theme"
>;

export interface WalletConnectorProviderProps {
  apiKey?: string;
  options?: ConnectorInitOptions;
  // skip board configuration if already initialized
  skipInit?: boolean;
}

export function InitEvm(
  props: PropsWithChildren<WalletConnectorProviderProps>,
) {
  const [initialized, setInitialized] = useState(!!props.skipInit);
  const initializedRef = useRef(!!props.skipInit);
  const fetchRequestedRef = useRef(false);

  const { get, register } = useSimpleDI();
  const fetchMainChains = useMainnetChainsStore((state) => state.fetchData);
  const fetchTestChains = useTestnetChainsStore((state) => state.fetchData);
  const mainnetChainsHydrated = useMainnetChainsStore(
    (state) => state.hydrated,
  );
  const testnetChainsHydrated = useTestnetChainsStore(
    (state) => state.hydrated,
  );
  const mainnetChainInfos = useMainnetChainsStore((state) => state.data);
  const testnetChainInfos = useTestnetChainsStore((state) => state.data);

  useEffect(() => {
    document.body.style.setProperty("--onboard-modal-z-index", "88");
  }, []);

  useEffect(() => {
    if (props.skipInit || initializedRef.current) {
      return;
    }

    const registeredOnboardAPI = get("onboardAPI") as OnboardAPI;

    if (registeredOnboardAPI) {
      console.log("[paxeer-network SDK]:onboardAPI already initialized");
      initializedRef.current = true;
      setInitialized(true);
      return;
    }

    if (
      !mainnetChainsHydrated ||
      !testnetChainsHydrated ||
      !Array.isArray(mainnetChainInfos) ||
      !Array.isArray(testnetChainInfos)
    ) {
      return;
    }

    try {
      const testChains = processChainInfo(testnetChainInfos);
      const mainnetChains = processChainInfo(mainnetChainInfos);
      const options = merge(
        { chains: [...testChains, ...mainnetChains] },
        props.options || {},
      );

      const onboardAPI = initConfig(props.apiKey, options as InitOptions);
      register("onboardAPI", onboardAPI);
      initializedRef.current = true;
      setInitialized(true);
    } catch (error) {
      console.error("Error initializing Web3 Onboard:", error);
    }
  }, [
    get,
    mainnetChainInfos,
    mainnetChainsHydrated,
    props.apiKey,
    props.options,
    props.skipInit,
    register,
    testnetChainInfos,
    testnetChainsHydrated,
  ]);

  useEffect(() => {
    if (
      props.skipInit ||
      initializedRef.current ||
      fetchRequestedRef.current ||
      !mainnetChainsHydrated ||
      !testnetChainsHydrated
    ) {
      return;
    }

    if (Array.isArray(mainnetChainInfos) && testnetChainInfos) {
      return;
    }

    fetchRequestedRef.current = true;
    void fetchMainChains();
    void fetchTestChains();
  }, [
    fetchMainChains,
    fetchTestChains,
    mainnetChainInfos,
    mainnetChainsHydrated,
    props.skipInit,
    testnetChainInfos,
    testnetChainsHydrated,
  ]);

  if (!initialized) return null;

  return props.children;
}

const processChainInfo = (chainInfo: any) =>
  (Array.isArray(chainInfo) ? chainInfo : [])
    .filter((row: any) => !SolanaChains.has(Number(row.chain_id)))
    .map((row: any) => ({
      id: Number(row.chain_id),
      token: row.currency_symbol,
      label: row.name,
      rpcUrl: row.public_rpc_url,
      blockExplorerUrl: row.explorer_base_url,
    }));
