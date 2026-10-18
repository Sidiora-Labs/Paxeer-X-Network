import { AbstractChains, SolanaChains } from "@paxeer-network/types";
import { WalletType } from "./types";

export const PaxeerNetworkOSS = "/assets";
export const PrivyConnectorImagePath = `${PaxeerNetworkOSS}/static/sdk/privy`;
export const EVMWalletImage = `${PaxeerNetworkOSS}/static/sdk/evm_wallet`;

// evm wallets icon map
const WALLET_ICONS: Record<string, string> = {
  metamask: "/assets/static/sdk/evm_wallets/metamask.png",
  walletconnect:
    "/assets/static/sdk/evm_wallets/walletConnect.png",
  binance: "/assets/static/sdk/evm_wallets/binance.png",
  ledger: "/assets/static/sdk/evm_wallets/ledger.png",
  coinbase: "/assets/static/sdk/evm_wallets/coinbase.png",
  magic: "/assets/static/sdk/evm_wallets/magic.png",
  trezor: "/assets/static/sdk/evm_wallets/trezor.png",
  exodus: "/assets/static/sdk/evm_wallets/exodus.png",
  frame: "/assets/static/sdk/evm_wallets/frame.png",
  rabby: "/assets/static/sdk/evm_wallets/rabby.png",
};
export const getWalletIcon = (type: string): string | undefined => {
  return WALLET_ICONS[type.toLowerCase()];
};

export const getChainType = (chainId: number): WalletType => {
  if (AbstractChains.has(chainId)) {
    return WalletType.ABSTRACT;
  }
  if (SolanaChains.has(chainId)) {
    return WalletType.SOL;
  }
  return WalletType.EVM;
};
