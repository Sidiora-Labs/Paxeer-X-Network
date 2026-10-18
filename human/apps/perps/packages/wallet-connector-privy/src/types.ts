import type { PropsWithChildren } from "react";
import { PrivyClientConfig } from "@privy-io/react-auth";
import {
  Adapter,
  WalletAdapter,
  WalletAdapterNetwork,
  WalletError,
} from "@solana/wallet-adapter-base";
import { QueryClient } from "@tanstack/react-query";
import { Connector, CreateConnectorFn, Storage } from "wagmi";
import { WalletState } from "@paxeer-network/hooks";
import {
  ABSTRACT_MAINNET_CHAINID,
  ABSTRACT_TESTNET_CHAINID,
  ChainNamespace,
  SOLANA_MAINNET_CHAINID,
  SOLANA_TESTNET_CHAINID,
} from "@paxeer-network/types";

export type SolanaInitialProps = PropsWithChildren<{
  network?: WalletAdapterNetwork;
  endPoint?: string;
  mainnetRpc?: string;
  devnetRpc?: string;
  wallets?: Adapter[];
  // wallet not ready error handle
  onError?: (error: WalletError, adapter?: Adapter) => void;
}>;

export enum Network {
  mainnet = "mainnet",
  testnet = "testnet",
}

export enum WalletType {
  EVM = "EVM",
  SOL = "SOL",
  ABSTRACT = "Abstract",
}

export enum WalletConnectType {
  EVM = "EVM",
  SOL = "SOL",
  PRIVY = "privy",
  ABSTRACT = "Abstract",
}

export interface ConnectProps {
  walletType: WalletConnectType;
  extraType?: string;
  connector?: Connector;
  walletAdapter?: WalletAdapter;
}

type PrivyAppearanceConfig = NonNullable<PrivyClientConfig["appearance"]>;

/** Appearance subset for InitPrivy; accentColor widened for CSS vars / rgb(). */
export type InitPrivyAppearance = Omit<
  PrivyAppearanceConfig,
  "walletChainType" | "walletList" | "accentColor"
> & {
  accentColor?: string;
};

/** Pass-through Privy client config; chains are owned by the SDK. */
export type InitPrivyClientConfig = Partial<
  Omit<PrivyClientConfig, "supportedChains" | "defaultChain" | "appearance">
> & {
  appearance?: InitPrivyAppearance;
};

export interface InitPrivy {
  appid: string;
  config?: InitPrivyClientConfig;
}

export interface InitWagmi {
  connectors?: CreateConnectorFn[];
  storage?: Storage;
}

export interface InitSolana {
  mainnetRpc?: string;
  devnetRpc?: string;
  wallets: Adapter[];
  onError: (error: WalletError, adapter?: Adapter) => void;
}

export interface InitAbstract {
  queryClient?: QueryClient;
}

export const SolanaChains = new Map([
  [WalletAdapterNetwork.Devnet, SOLANA_TESTNET_CHAINID],
  [WalletAdapterNetwork.Mainnet, SOLANA_MAINNET_CHAINID],
]);

export const SolanaChainsMap = new Map<Network | WalletAdapterNetwork, number>([
  [WalletAdapterNetwork.Devnet, SOLANA_TESTNET_CHAINID],
  [Network.testnet, SOLANA_TESTNET_CHAINID],
  [Network.mainnet, SOLANA_MAINNET_CHAINID],
  [WalletAdapterNetwork.Mainnet, SOLANA_MAINNET_CHAINID],
]);

export const AbstractChainsMap = new Map<Network, number>([
  [Network.testnet, ABSTRACT_TESTNET_CHAINID],
  [Network.mainnet, ABSTRACT_MAINNET_CHAINID],
]);

export interface ConnectorWalletType {
  disableWagmi?: boolean;
  disablePrivy?: boolean;
  disableSolana?: boolean;
  disableAGW?: boolean;
}
export interface WalletChainTypeConfig {
  hasEvm: boolean;
  hasSol: boolean;
  hasAbstract: boolean;
}
export enum WalletChainTypeEnum {
  onlyEVM = "onlyEVM",
  onlySOL = "onlySOL",
  EVM_SOL = "EVM_SOL",
}
export type WalletChainType = WalletChainTypeEnum;

export type IWalletState = WalletState & {
  chain?: {
    namespace: ChainNamespace;
    id: number;
  };
};
