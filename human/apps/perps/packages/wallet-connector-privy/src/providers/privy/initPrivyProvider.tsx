import { type PropsWithChildren, useMemo } from "react";
import { PrivyClientConfig, PrivyProvider } from "@privy-io/react-auth";
import { Chain } from "viem/chains";
import {
  AbstractChains,
  SolanaChains,
  defaultMainnetChains,
  defaultTestnetChains,
} from "@paxeer-network/types";
import { useWalletConnectorPrivy } from "../../provider";
import { InitPrivy, Network } from "../../types";

interface IProps extends PropsWithChildren {
  privyConfig: InitPrivy;
  initChains: Chain[];
}

/**
 * Builds the Privy client config from the merged mainnet/testnet chain list.
 *
 * Privy throws on an empty `supportedChains` array ("`supportedChains` must
 * contain at least one chain"). A broker may legitimately expose Solana-only
 * chains, so when no EVM chain survives the filter both `supportedChains` and
 * `defaultChain` are omitted and Privy falls back to its own defaults — the
 * provider itself must stay mounted for login and Solana embedded wallets.
 */
export const buildPrivyClientConfig = (options: {
  initChains: Chain[];
  privyConfig: InitPrivy;
  network: Network;
}): PrivyClientConfig => {
  const { initChains, privyConfig, network } = options;
  const chains = initChains.filter((chain) => !SolanaChains.has(chain.id));
  const preferredDefaultChainIds = (
    network === "mainnet" ? defaultMainnetChains : defaultTestnetChains
  ).map((c) => c.id);

  const preferredDefaultChain = preferredDefaultChainIds
    .map((id) => chains.find((c) => c.id === id))
    .find((c) => !!c);

  const firstEvmChain = chains.find(
    (chain) => !SolanaChains.has(chain.id) && !AbstractChains.has(chain.id),
  );

  const defaultEvmChain = preferredDefaultChain ?? firstEvmChain ?? chains[0];

  const userConfig = (privyConfig.config ?? {}) as Partial<PrivyClientConfig>;
  const {
    supportedChains,
    defaultChain,
    loginMethods: userLoginMethods,
    appearance: userAppearance,
    embeddedWallets: userEmbedded,
    externalWallets: userExternal,
    ...rest
  } = userConfig;

  void supportedChains;
  void defaultChain;

  const defaultEmbedded: NonNullable<PrivyClientConfig["embeddedWallets"]> = {
    ethereum: {
      createOnLogin: "all-users",
    },
    solana: {
      createOnLogin: "all-users",
    },
  };

  const defaultExternal: NonNullable<PrivyClientConfig["externalWallets"]> = {
    walletConnect: {
      enabled: false,
    },
  };

  const embeddedWallets: PrivyClientConfig["embeddedWallets"] = {
    ...defaultEmbedded,
    ...userEmbedded,
    ethereum: {
      ...defaultEmbedded.ethereum,
      ...userEmbedded?.ethereum,
    },
    solana: {
      ...defaultEmbedded.solana,
      ...userEmbedded?.solana,
    },
  };

  const externalWallets: PrivyClientConfig["externalWallets"] = {
    ...defaultExternal,
    ...userExternal,
    walletConnect: {
      ...defaultExternal.walletConnect,
      ...userExternal?.walletConnect,
      enabled:
        userExternal?.walletConnect?.enabled ??
        defaultExternal.walletConnect?.enabled ??
        false,
    },
  };

  const hasEvmChains = chains.length > 0;

  return {
    ...rest,
    loginMethods: userLoginMethods || ["email", "google", "twitter"],
    appearance: {
      ...userAppearance,
      walletChainType: "ethereum-and-solana",
    },
    embeddedWallets,
    externalWallets,
    ...(hasEvmChains
      ? { defaultChain: defaultEvmChain, supportedChains: chains }
      : {}),
  };
};

export function InitPrivyProvider({
  privyConfig,
  initChains,
  children,
}: IProps) {
  const { network } = useWalletConnectorPrivy();

  const config = useMemo(
    () => buildPrivyClientConfig({ initChains, privyConfig, network }),
    [initChains, privyConfig, network],
  );

  if (!initChains.length) {
    console.warn("initChains is empty");
    return null;
  }

  return (
    <PrivyProvider appId={privyConfig.appid} config={config}>
      {children}
    </PrivyProvider>
  );
}
