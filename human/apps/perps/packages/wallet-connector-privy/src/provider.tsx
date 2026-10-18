import React, {
  type PropsWithChildren,
  useEffect,
  useRef,
  useState,
  createContext,
  useContext,
  useMemo,
} from "react";
import { PrivyClientConfig } from "@privy-io/react-auth";
import { WalletAdapterNetwork } from "@solana/wallet-adapter-base";
import { type Chain, defineChain } from "viem";
import { mainnet } from "viem/chains";
import {
  Chains,
  testnetChainFallback,
  useMainnetChainsStore,
  useTestnetChainsStore,
} from "@paxeer-network/hooks";
import { AbstractChains, API, SolanaChains } from "@paxeer-network/types";
import { TooltipProvider } from "@paxeer-network/ui";
import { Main } from "./main";
import { AbstractWallet } from "./providers/abstractWallet";
import { PrivyWallet } from "./providers/privy";
import { SolanaWallet } from "./providers/solana";
import { WagmiWallet } from "./providers/wagmi";
import {
  InitPrivy,
  InitWagmi,
  InitSolana,
  Network,
  WalletChainType,
  WalletChainTypeEnum,
  ConnectorWalletType,
  InitAbstract,
  WalletChainTypeConfig,
  WalletType,
} from "./types";

const processChainInfo = (chainInfo: any) =>
  chainInfo.map((row: any) =>
    defineChain({
      id: Number(row.chain_id),
      name: row.name,
      nativeCurrency: {
        decimals: row.currency_decimal,
        name: "Ether",
        symbol: row.currency_symbol,
      },
      rpcUrls: {
        default: {
          http: [row.public_rpc_url],
        },
      },
      blockExplorers: {
        default: { name: "Explorer", url: row.explorer_base_url },
      },
    }),
  ) || [];

interface WalletConnectorPrivyContextType {
  initChains: Chain[];
  mainnetChains: Chain[];
  testnetChains: Chain[];
  getChainsByNetwork: (network: "mainnet" | "testnet") => Chain[];
  openConnectDrawer: boolean;
  setOpenConnectDrawer: (open: boolean) => void;
  targetWalletType: WalletType | undefined;
  setTargetWalletType: (walletType: WalletType | undefined) => void;
  network: Network;
  setNetwork: (network: Network) => void;
  solanaInfo: {
    rpcUrl: string | null;
    network: WalletAdapterNetwork | null;
  } | null;
  setSolanaInfo: (
    solanaInfo: {
      rpcUrl: string | null;
      network: WalletAdapterNetwork | null;
    } | null,
  ) => void;
  termsOfUse: string;
  // TODO deprecated
  walletChainType: WalletChainType;
  // TODO new chaintype config
  walletChainTypeConfig: WalletChainTypeConfig;
  connectorWalletType: ConnectorWalletType;
  privyConfig: {
    loginMethods?: PrivyClientConfig["loginMethods"];
  };
}

const WalletConnectorPrivyContext =
  createContext<WalletConnectorPrivyContextType>({
    initChains: [mainnet],
    mainnetChains: [],
    testnetChains: [],
    getChainsByNetwork: () => [],
    openConnectDrawer: false,
    setOpenConnectDrawer: () => {},
    targetWalletType: undefined,
    setTargetWalletType: () => {},
    network: Network.mainnet,
    setNetwork: () => {},
    solanaInfo: null,
    setSolanaInfo: () => {},
    termsOfUse: "",
    walletChainType: WalletChainTypeEnum.EVM_SOL,
    walletChainTypeConfig: {
      hasEvm: true,
      hasSol: true,
      hasAbstract: false,
    },
    connectorWalletType: {
      disableWagmi: false,
      disablePrivy: false,
      disableSolana: false,
      disableAGW: false,
    },
    privyConfig: {
      loginMethods: [],
    },
  });

export const useWalletConnectorPrivy = () =>
  useContext(WalletConnectorPrivyContext);

interface WalletConnectorPrivyProps extends PropsWithChildren {
  privyConfig?: InitPrivy;
  wagmiConfig?: InitWagmi;
  solanaConfig?: InitSolana;
  abstractConfig?: InitAbstract;
  network: Network;
  customChains?: Chains;
  termsOfUse?: string;
  headerProps?: {
    mobile: React.ReactNode;
  };
}

const defaultPrivyLoginMethod = [
  "email",
  "google",
  "twitter",
] as PrivyClientConfig["loginMethods"];
export function WalletConnectorPrivyProvider(props: WalletConnectorPrivyProps) {
  const [walletChainType, setWalletChainType] = useState<WalletChainType>(
    WalletChainTypeEnum.EVM_SOL,
  );
  const [termsOfUse] = useState<string>(props.termsOfUse || "");
  const [network, setNetwork] = useState<Network>(props.network);
  const [initChains, setInitChains] = useState<Chain[]>([]);
  const [mainnetChains, setMainnetChains] = useState<Chain[]>([]);
  const [testnetChains, setTestnetChains] = useState<Chain[]>([]);
  const [mainnetChainInfos, setMainnetChainInfos] = useState<
    API.Chain[] | null
  >(null);
  const [testChainInfos, setTestChainInfos] = useState<API.Chain[] | null>(
    null,
  );

  const fetchMainChains = useMainnetChainsStore((state) => state.fetchData);
  const fetchTestChains = useTestnetChainsStore((state) => state.fetchData);

  const mainnetChainsHydrated = useMainnetChainsStore(
    (state) => state.hydrated,
  );
  const testChainsHydrated = useTestnetChainsStore((state) => state.hydrated);

  const mainnetChainInfosFromStore = useMainnetChainsStore(
    (state) => state.data,
  );
  const testChainInfosFromStore = useTestnetChainsStore((state) => state.data);

  const hasCustomChains =
    Array.isArray(props.customChains) && props.customChains.length > 0;

  const initRef = useRef(hasCustomChains);
  const [openConnectDrawer, setOpenConnectDrawer] = useState(false);
  const [targetWalletType, setTargetWalletType] = useState<
    WalletType | undefined
  >();
  const [privyConfig, setPrivyConfig] = useState<PrivyClientConfig>({
    loginMethods: props.privyConfig
      ? (props.privyConfig?.config?.loginMethods ?? defaultPrivyLoginMethod)
      : defaultPrivyLoginMethod,
  });
  const [solanaInfo, setSolanaInfo] = useState<{
    rpcUrl: string | null;
    network: WalletAdapterNetwork | null;
  } | null>(null);

  const connectorWalletType = useMemo(() => {
    const type: ConnectorWalletType = {
      disableWagmi: false,
      disablePrivy: false,
      disableSolana: false,
      disableAGW: false,
    };
    if (!props.privyConfig) {
      type.disablePrivy = true;
    }
    if (!props.wagmiConfig) {
      type.disableWagmi = true;
    }
    if (!props.solanaConfig) {
      type.disableSolana = true;
    }
    if (!props.abstractConfig) {
      type.disableAGW = true;
    }
    return type;
  }, [
    props.privyConfig,
    props.wagmiConfig,
    props.solanaConfig,
    props.abstractConfig,
  ]);

  const walletChainTypeConfig = useMemo(() => {
    const chainTypeObj: WalletChainTypeConfig = {
      hasEvm: false,
      hasSol: false,
      hasAbstract: false,
    };
    initChains.forEach((chain) => {
      if (SolanaChains.has(chain.id)) {
        chainTypeObj.hasSol = true;
      } else if (AbstractChains.has(chain.id)) {
        chainTypeObj.hasAbstract = true;
      } else {
        chainTypeObj.hasEvm = true;
      }
    });
    return chainTypeObj;
  }, [initChains]);

  useEffect(() => {
    if (!mainnetChainsHydrated || !testChainsHydrated) return;

    if (
      Array.isArray(useMainnetChainsStore.getState().data) &&
      useTestnetChainsStore.getState().data
    ) {
      return;
    }

    fetchMainChains().then((data) => {
      setMainnetChainInfos(data);
    });
    fetchTestChains().then((data) => {
      setTestChainInfos(data);
    });
  }, [mainnetChainsHydrated, testChainsHydrated]);

  const handleCustomerChains = () => {
    const testChains = processChainInfo(
      props.customChains!.testnet?.map((item) => item.network_infos),
    );
    const mainnetChains = processChainInfo(
      props.customChains!.mainnet?.map((item) => item.network_infos),
    );
    setTestnetChains(testChains);
    setMainnetChains(mainnetChains);
    setInitChains([...testChains, ...mainnetChains] as [Chain, ...Chain[]]);
    const chainTypeObj: {
      hasEvm: boolean;
      hasSol: boolean;
      hasAbstract: boolean;
    } = {
      hasEvm: false,
      hasSol: false,
      hasAbstract: false,
    };
    [...testChains, ...mainnetChains].forEach((chain) => {
      if (SolanaChains.has(chain.id)) {
        chainTypeObj.hasSol = true;
      } else if (AbstractChains.has(chain.id)) {
        chainTypeObj.hasAbstract = true;
      } else {
        chainTypeObj.hasEvm = true;
      }
    });
    initRef.current = true;
    if (chainTypeObj.hasEvm && chainTypeObj.hasSol) {
      setWalletChainType(WalletChainTypeEnum.EVM_SOL);
    } else if (chainTypeObj.hasEvm) {
      setWalletChainType(WalletChainTypeEnum.onlyEVM);
    } else if (chainTypeObj.hasSol) {
      setWalletChainType(WalletChainTypeEnum.onlySOL);
    }
  };
  if (
    connectorWalletType.disablePrivy &&
    connectorWalletType.disableWagmi &&
    connectorWalletType.disableSolana &&
    connectorWalletType.disableAGW
  ) {
    throw new Error(
      "Privy, Wagmi, Solana, Abstract are all disabled. Please enable at least one of them.",
    );
  }

  const getChainsByNetwork = (network: "mainnet" | "testnet"): Chain[] => {
    return network === "mainnet" ? mainnetChains : testnetChains;
  };

  const value = useMemo(
    () => ({
      initChains,
      mainnetChains,
      testnetChains,
      getChainsByNetwork,
      openConnectDrawer,
      setOpenConnectDrawer,
      targetWalletType,
      setTargetWalletType,
      network,
      setNetwork,
      solanaInfo,
      setSolanaInfo,
      termsOfUse,
      walletChainType,
      connectorWalletType,
      walletChainTypeConfig,
      privyConfig,
    }),
    [
      initChains,
      mainnetChains,
      testnetChains,
      getChainsByNetwork,
      openConnectDrawer,
      setOpenConnectDrawer,
      targetWalletType,
      setTargetWalletType,
      network,
      setNetwork,
      solanaInfo,
      setSolanaInfo,
      termsOfUse,
      walletChainType,
      connectorWalletType,
      walletChainTypeConfig,
      privyConfig,
    ],
  );

  useEffect(() => {
    if (initRef.current) return;
    if (hasCustomChains) {
      return;
    }

    // Check which data source is available first (store or API)
    const hasStoreData = mainnetChainInfosFromStore && testChainInfosFromStore;
    const hasApiData = mainnetChainInfos && testChainInfos;

    // If neither store nor API data is ready, wait
    if (!hasStoreData && !hasApiData) {
      return;
    }

    let testChainsList = [];
    let mainnetChainsList = [];
    try {
      // Use data in pairs: either both from store or both from API
      // Priority: if store data is available, use store; otherwise use API
      if (hasStoreData) {
        testChainsList = testChainInfosFromStore;
        mainnetChainsList = mainnetChainInfosFromStore;
      } else {
        testChainsList = testChainInfos || testnetChainFallback;
        mainnetChainsList = mainnetChainInfos || [];
      }

      const testChains = processChainInfo(testChainsList);
      const mainnetChains = processChainInfo(mainnetChainsList);

      const chains = [...testChains, ...mainnetChains];
      setTestnetChains(testChains);
      setMainnetChains(mainnetChains);

      setInitChains(chains);
    } catch (error) {
      console.error("Error initChains:", error);
      testChainsList = testnetChainFallback;
      mainnetChainsList = [];
    }

    initRef.current = true;
  }, [
    props.customChains,
    mainnetChainInfos,
    testChainInfos,
    mainnetChainInfosFromStore,
    testChainInfosFromStore,
  ]);

  useEffect(() => {
    if (props.customChains) {
      handleCustomerChains();
    }
  }, [props.customChains]);

  if (!initRef.current) {
    return null;
  }

  return (
    <WalletConnectorPrivyContext.Provider value={value}>
      <TooltipProvider delayDuration={300}>
        <PrivyWallet privyConfig={props.privyConfig} initChains={initChains}>
          <WagmiWallet wagmiConfig={props.wagmiConfig} initChains={initChains}>
            <SolanaWallet solanaConfig={props.solanaConfig}>
              <AbstractWallet abstractConfig={props.abstractConfig}>
                <Main headerProps={props.headerProps}>{props.children}</Main>
              </AbstractWallet>
            </SolanaWallet>
          </WagmiWallet>
        </PrivyWallet>
      </TooltipProvider>
    </WalletConnectorPrivyContext.Provider>
  );
}
