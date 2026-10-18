import {
  WalletAdapterNetwork,
  type SignerWalletAdapterProps,
  type WalletAdapterProps,
} from "@solana/wallet-adapter-base";
import { Connection, PublicKey } from "@solana/web3.js";

export interface SolanaAdapterOption {
  provider: SolanaWalletProvider;
  address: string;
  chain: { id: number };
}

export interface SolanaWalletProvider {
  connection?: Connection;
  rpcUrl?: string;
  publicKey?: PublicKey;
  network: WalletAdapterNetwork;
  signMessage: (message: Uint8Array) => Promise<Uint8Array>;
  signTransaction: SignerWalletAdapterProps["signTransaction"];
  sendTransaction: WalletAdapterProps["sendTransaction"];
}
