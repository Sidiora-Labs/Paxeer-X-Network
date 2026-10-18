import type { PropsWithChildren } from "react";
import { Adapter, WalletAdapterNetwork, WalletError } from "@solana/wallet-adapter-base";
import { Optional } from "@paxeer-network/types";
import type { InitOptions } from "@web3-onboard/core";
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
export type SolanaInitialProps = PropsWithChildren<{
  network?: WalletAdapterNetwork;
  mainnetRpc?: string;
  devnetRpc?: string;
  wallets?: Adapter[];
  // wallet not ready error handle
  onError?: (error: WalletError, adapter?: Adapter)=> void;
}>;

export type EvmInitialProps = PropsWithChildren<{
  apiKey?: string;
  options?: ConnectorInitOptions;
  // skip board configuration if already initialized
  skipInit?: boolean;
}>;
