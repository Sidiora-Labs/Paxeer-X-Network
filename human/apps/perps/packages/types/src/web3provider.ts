import { API } from "./types/api";

export interface Web3Provider {
  signTypedData(
    address: string,
    toSignatureMessage: Record<string, any>
  ): Promise<string>;

  call(
    address: string,
    method: string,
    params: any[],
    options?: {
      abi: any;
    }
  ): Promise<any>;

  /**
   * send transaction
   */
  sendTransaction(
    address: string,
    method: string,
    params: any[],
    options?: {
      abi: any;
    }
  ): Promise<any>;

  callOnChain(
    chain: API.NetworkInfos,
    address: string,
    method: string,
    params: any[],
    options: {
      abi: any;
    }
  ): Promise<any>;

  getBalance(): Promise<string>;
}
