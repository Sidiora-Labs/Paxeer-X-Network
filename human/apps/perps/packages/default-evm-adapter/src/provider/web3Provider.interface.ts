import { API } from "@paxeer-network/types";

export type Eip1193Provider = {
  request: (args: { method: string; params?: any[] }) => Promise<any>;
};

export interface Web3Provider {
  set provider(provider: Eip1193Provider);

  parseUnits(amount: string, decimals: number): string;

  formatUnits(amount: string, decimals: number): string;

  signTypedData(address: string, toSignatureMessage: any): Promise<string>;

  send(method: string, params: Array<any> | Record<string, any>): Promise<any>;

  call(
    address: string,
    method: string,
    params: any[],
    options?: {
      abi: any;
    },
  ): Promise<any>;

  /**
   * send transaction
   */
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
  ): Promise<any>;

  callOnChain(
    chain: API.NetworkInfos,
    address: string,
    method: string,
    params: any[],
    options: {
      abi: any;
    },
  ): Promise<any>;

  pollTransactionReceiptWithBackoff(
    txHash: string,
    baseInterval?: number,
    maxInterval?: number,
    maxRetries?: number,
  ): Promise<any>;

  getBalance(userAddress: string): Promise<bigint>;
  getBalances(addresses: string[]): Promise<any>;

  estimateGasFee(
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
  ): Promise<bigint>;
}
