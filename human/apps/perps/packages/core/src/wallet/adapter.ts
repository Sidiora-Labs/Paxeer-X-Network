import { TransactionResponse } from "ethers";
import { API } from "@paxeer-network/types";

export interface IWalletAdapter {
  //   get address(): string;
  // new (options: WalletAdapterOptions): WalletAdapter;
  get chainId(): number;
  get addresses(): string;
  /**
   * Set the chain id
   */
  set chainId(chainId: number);
  parseUnits: (amount: string, decimals: number) => string;
  formatUnits: (amount: string, decimals: number) => string;
  // getBalance: (address: string) => Promise<any>;
  // deposit: (from: string, to: string, amount: string) => Promise<any>;
  send: (
    method: string,
    params: Array<any> | Record<string, any>,
  ) => Promise<any>;

  sendTransaction(
    contractAddress: string,
    method: string,
    payload: {
      from: string;
      to?: string;
      data: any[];
      value?: bigint;
    },
    options: {
      abi: any;
    },
  ): Promise<TransactionResponse>;

  getTransactionRecipect: (txHash: string) => Promise<any>;
  signTypedData: (address: string, data: any) => Promise<string>;

  pollTransactionReceiptWithBackoff: (
    txHash: string,
    baseInterval?: number,
    maxInterval?: number,
    maxRetries?: number,
  ) => Promise<any>;

  // Get the balance of address
  getBalance: (
    // address: string,
    userAddress: string,
  ) => Promise<any>;

  call(
    address: string,
    method: string,
    params: any,
    options: {
      abi: any;
    },
  ): Promise<any>;

  callOnChain(
    chain: API.NetworkInfos,
    address: string,
    method: string,
    params: any,
    options: {
      abi: any;
    },
  ): Promise<any>;

  on(eventName: any, listener: any): void;
  off(eventName: any, listener: any): void;
}

export type WalletAdapterOptions = {
  provider: any;
  address: string;
  chain: { id: number };
};

export type getWalletAdapterFunc = (
  options: WalletAdapterOptions,
) => IWalletAdapter;
