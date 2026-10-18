import { useCallback, useContext, useMemo, useRef } from "react";
import { prop } from "ramda";
import { SWRConfiguration } from "swr";
import {
  NetworkId,
  type API,
  Chain as FlatChain,
  SOLANA_TESTNET_CHAINID,
  ARBITRUM_TESTNET_CHAINID,
  BSC_TESTNET_CHAINID,
  isNativeTokenChecker,
  XLAYER_TESTNET_CHAINID,
} from "@paxeer-network/types";
import { nativeTokenAddress } from "@paxeer-network/types";
import { PaxeerNetworkContext } from "../paxeerNetworkContext";
import {
  mainnetChainFallback,
  testnetChainFallback,
  useMainnetChainsStore,
  useTestnetChainsStore,
} from "../provider/store";
import { useMainTokenStore } from "../provider/store/mainTokenStore";
import { useTestTokenStore } from "../provider/store/testTokenStore";

// testnet white list
export const TESTNET_WHITE_LIST = [
  ARBITRUM_TESTNET_CHAINID,
  SOLANA_TESTNET_CHAINID,
  BSC_TESTNET_CHAINID,
];

export const TESTNET_WHITE_CHAINS = TESTNET_WHITE_LIST.map((id) => ({
  id,
})) satisfies Pick<FlatChain, "id">[];

export type Chain = API.Chain & {
  nativeToken?: API.TokenInfo;
  isTestnet?: boolean;
};

export type Chains<
  T extends NetworkId | undefined = undefined,
  K extends keyof API.Chain | undefined = undefined,
> = T extends NetworkId
  ? K extends keyof API.Chain
    ? API.Chain[K][]
    : API.Chain[]
  : K extends keyof API.Chain
    ? {
        testnet: API.Chain[K][];
        mainnet: API.Chain[K][];
      }
    : {
        testnet: API.Chain[];
        mainnet: API.Chain[];
      };

export type UseChainsOptions = {
  filter?: (item: API.Chain) => boolean;
  pick?: "dexs" | "network_infos" | "token_infos";
  // Whether to force the use of data returned by the API, ignoring the customChains.
  forceAPI?: boolean;
} & SWRConfiguration;

export type UseChainsReturnObject = {
  findByChainId: (chainId: number, field?: string) => Chain | undefined;
  isTestnetChain: (chainId?: number | string) => boolean;
  checkChainSupport: (
    chainId: number | string,
    networkId: NetworkId,
  ) => boolean;
  error: any;
};

// const testnetTokenFallback = TesnetTokenFallback([
//   ArbitrumSepoliaTokenInfo,
//   SolanaDevnetTokenInfo,
// ]);

export function useChains(
  networkId?: undefined,
  options?: undefined,
): [Chains<undefined, undefined>, UseChainsReturnObject];

export function useChains<
  T extends NetworkId | undefined,
  K extends UseChainsOptions | undefined,
>(
  networkId?: T,
  options?: K,
): [
  Chains<
    T,
    K extends UseChainsOptions
      ? K["pick"] extends keyof API.Chain
        ? K["pick"]
        : undefined
      : undefined
  >,
  UseChainsReturnObject,
];

/**
 *
 * @example
 * // { testnet: API.Chain[];  mainnet: API.Chain[]; }
 * const [chains1] = useChains();
 *
 * // { testnet: API.Chain[];  mainnet: API.Chain[]; }
 * const [chains2] = useChains(undefined);
 *
 * // { testnet: API.NetworkInfos[];  mainnet: API.NetworkInfos[]; }
 * const [chains3] = useChains(undefined, { pick: "network_infos" });
 *
 * // API.Chain[]
 * const [chains4] = useChains("testnet");
 *
 * // API.Chain[]
 * const [chains5] = useChains("mainnet");
 *
 * // API.NetworkInfos[]
 * const [chains6] = useChains("testnet", { pick: "network_infos" }); *
 *
 */
export function useChains(
  networkId?: NetworkId,
  options: UseChainsOptions = {},
): [any, any] {
  const { pick: pickField } = options;
  const {
    filteredChains: allowedChains,
    customChains,
    dataAdapter,
  } = useContext(PaxeerNetworkContext);

  const filterFun = useRef(options?.filter);
  filterFun.current = options?.filter;

  const chainsMap = useRef(new Map<number, Chain>());

  // const brokerId = configStore.get("brokerId");
  // const env = configStore.get("env");
  // const brokerIdQuery = brokerId !== "paxeer-network" ? `?broker_id=${brokerId}` : "";

  // const urlPrefix = env === "prod" ? "https://testnet-api.paxeer-network.org" : "";

  const needFetchFromAPI = options.forceAPI || !customChains;

  // const commonSwrOpts = {
  //   revalidateIfStale: false,
  //   revalidateOnFocus: false,
  //   revalidateOnReconnect: false,
  //   // If false, undefined data gets cached against the key.
  //   revalidateOnMount: true,
  //   // don't duplicate a request with the same key for 1hr
  //   dedupingInterval: 3_600_000,
  //   ...swrOptions,
  // };

  const { data: tokenChainsRes, error: tokenError } = useMainTokenStore();

  // only prod env return mainnet chains info
  // const { data: tokenChainsRes, error: tokenError } = useQuery<API.Chain[]>(
  //   "https://api.paxeer-network.org/v1/public/token",
  //   { ...commonSwrOpts },
  // );

  const testTokenChainsRes = useTestTokenStore((state) => state.data);

  // testnet token info
  // const { data: testTokenChainsRes } = useQuery<API.Chain[]>(
  //   `${urlPrefix}/v1/public/token`,
  //   {
  //     ...commonSwrOpts,
  //     fallbackData: testnetTokenFallback,
  //   },
  // );

  const mainnetChainInfos = useMainnetChainsStore((state) => state.data);
  const mainnetChainInfoOrigin = useMainnetChainsStore(
    (state) => state.dataOrigin,
  );
  // Only broker-authorized chain info feeds chain selection (deposit /
  // withdraw UIs). Wallet connectors (initEvm / privy provider) intentionally
  // read the store's raw data as a superset: onboard / privy cannot add
  // chains after initialization, and a missing chain breaks wallet switching
  // even for chains the broker filter excludes.
  const chainInfos =
    mainnetChainInfoOrigin === "broker"
      ? mainnetChainInfos
      : mainnetChainFallback;
  // only prod env return mainnet chains info
  // const { data: chainInfos, error: chainInfoErr } = useQuery(
  //   needFetchFromAPI
  //     ? `https://api.paxeer-network.org/v1/public/chain_info${brokerIdQuery}`
  //     : null,
  //   { ...commonSwrOpts },
  // );

  const testnetChainInfos = useTestnetChainsStore((state) => state.data);
  const testnetChainInfoOrigin = useTestnetChainsStore(
    (state) => state.dataOrigin,
  );
  const testChainInfos =
    testnetChainInfoOrigin === "broker"
      ? testnetChainInfos
      : testnetChainFallback;
  // testnet chains info
  // const { data: testChainInfos, error: testChainInfoError } = useQuery(
  //   needFetchFromAPI
  //     ? `${urlPrefix}/v1/public/chain_info${brokerIdQuery}`
  //     : null,
  //   {
  //     ...commonSwrOpts,
  //     fallbackData: testnetChainFallback,
  //     onError: (error) => {
  //       console.error("Failed to fetch testnet chain info:", error);
  //     },
  //   },
  // );

  const chains = useMemo(() => {
    if (
      !tokenChainsRes ||
      !testTokenChainsRes ||
      !chainInfos ||
      !testChainInfos
    ) {
      chainsMap.current = new Map();
      return networkId ? [] : { mainnet: [], testnet: [] };
    }
    const mainnetChains = formatChains({
      tokenChains: tokenChainsRes,
      chainInfos,
      filter: filterFun.current,
      mainnet: true,
      resolveChains: dataAdapter?.chainsList,
    });

    const testnetChains = formatChains({
      tokenChains: testTokenChainsRes,
      chainInfos: testChainInfos,
      mainnet: false,
      resolveChains: dataAdapter?.chainsList,
    });

    let mainnetArr = needFetchFromAPI ? mainnetChains : customChains?.mainnet;
    let testnetArr = needFetchFromAPI ? testnetChains : customChains?.testnet;

    const nextChainsMap = new Map<number, Chain>();

    testnetArr?.forEach((chain) => {
      nextChainsMap.set(chain.network_infos.chain_id, chain);
    });

    mainnetArr?.forEach((chain) => {
      nextChainsMap.set(chain.network_infos.chain_id, chain);
    });

    chainsMap.current = nextChainsMap;

    mainnetArr = filterByAllowedChains(mainnetArr, allowedChains?.mainnet);
    testnetArr = filterByAllowedChains(
      testnetArr,
      allowedChains?.testnet ?? TESTNET_WHITE_CHAINS,
    );

    if (!!pickField) {
      //@ts-ignore
      testnetArr = testnetArr.map((item) => item[pickField]);
      //@ts-ignore
      mainnetArr = mainnetArr.map((item) => item[pickField]);
    }

    if (networkId === "mainnet") {
      return mainnetArr;
    }

    if (networkId === "testnet") {
      return testnetArr;
    }

    return {
      testnet: testnetArr,
      mainnet: mainnetArr,
    };
  }, [
    networkId,
    tokenChainsRes,
    chainInfos,
    testChainInfos,
    testTokenChainsRes,
    customChains,
    pickField,
    allowedChains,
    dataAdapter,
  ]);

  const findByChainId = useCallback(
    (chainId: number, field?: string) => {
      const chain = chainsMap.current.get(chainId);

      if (chain) {
        chain.nativeToken =
          chain.token_infos?.find((item) =>
            isNativeTokenChecker(item.address),
          ) ||
          ({
            symbol: chain.network_infos?.currency_symbol,
          } as any);
      }

      if (typeof field === "string") {
        return prop(field as never, chain);
      }

      return chain;
    },
    [chains, chainsMap],
  );

  const checkChainSupport = useCallback(
    (chainId: number | string, networkId: NetworkId) => {
      const _chains = Array.isArray(chains) ? chains : chains[networkId];
      return _checkChainSupport(chainId, _chains);
    },
    [chains],
  );

  const isTestnetChain = useCallback(
    (chainId?: number | string) => {
      if (typeof chainId === "undefined") {
        return false;
      }

      const normalizedChainId =
        typeof chainId === "number"
          ? chainId
          : chainId.trim() === ""
            ? NaN
            : Number(chainId);

      if (!Number.isFinite(normalizedChainId)) {
        return false;
      }

      return (
        chainsMap.current.get(normalizedChainId)?.network_infos?.mainnet ===
        false
      );
    },
    [chains],
  );

  return [
    chains,
    {
      findByChainId,
      isTestnetChain,
      checkChainSupport,
      error: tokenError,
    },
  ];
}

function _checkChainSupport(chainId: number | string, chains: API.Chain[]) {
  if (typeof chainId === "string") {
    chainId = parseInt(chainId);
  }
  return chains.some((chain) => {
    return chain.network_infos.chain_id === chainId;
  });
}

export function filterByAllowedChains(
  chains: API.Chain[],
  allowedChains?: Partial<FlatChain>[],
) {
  if (!allowedChains) {
    return chains;
  }

  return allowedChains
    .map((allowedChain) => {
      return chains.find(
        (chain) =>
          parseInt(chain.network_infos?.chain_id as any) === allowedChain.id,
      );
    })
    .filter((chain) => !!chain);
}

export function formatChains(options: {
  chainInfos?: any;
  tokenChains?: API.Token[];
  filter?: (chain: any) => boolean;
  mainnet: boolean;
  resolveChains?: (chains: API.Chain[]) => API.Chain[];
}) {
  const {
    chainInfos = [],
    tokenChains = [],
    filter,
    mainnet,
    resolveChains,
  } = options;

  const chains: API.Chain[] = [];

  for (const chainInfo of chainInfos) {
    const chainId = Number(chainInfo.chain_id);

    const {
      name,
      public_rpc_url,
      currency_symbol,
      currency_decimal,
      explorer_base_url,
      vault_address,
    } = chainInfo;

    const network_infos = {
      name,
      chain_id: chainId,
      bridgeless: true,
      mainnet,

      public_rpc_url,
      currency_symbol,
      currency_decimal,
      bridge_enable: true,
      explorer_base_url,

      shortName: name,
      vault_address,
    };

    const tokenInfos = tokenChains
      .filter((item) =>
        item.chain_details.some((item) => Number(item.chain_id) === chainId),
      )
      .map((item) => {
        const chain = item.chain_details.find(
          (item) => Number(item.chain_id) === chainId,
        );

        return {
          symbol: item.token,
          // if contract_address is not exist, use nativeTokenAddress to place holder
          address: chain?.contract_address || nativeTokenAddress,
          /** chain decimals */
          decimals: chain?.decimals,
          /** token decimals */
          token_decimal: item?.decimals,
          precision: item.decimals,

          display_name: chain?.display_name,

          withdrawal_fee: chain?.withdrawal_fee,
          cross_chain_withdrawal_fee: chain?.cross_chain_withdrawal_fee,
          minimum_withdraw_amount: item.minimum_withdraw_amount,

          base_weight: item.base_weight,
          discount_factor: item.discount_factor,
          haircut: item.haircut,
          user_max_qty: item.user_max_qty,
          is_collateral: item.is_collateral,
        };
      });

    const _chain: any = {
      network_infos,
      token_infos: tokenInfos,
    };

    if (typeof filter === "function") {
      if (!filter(_chain)) continue;
    }

    chains.push(_chain);
  }

  if (typeof resolveChains === "function") {
    return resolveChains(chains);
  }

  return chains;
}
