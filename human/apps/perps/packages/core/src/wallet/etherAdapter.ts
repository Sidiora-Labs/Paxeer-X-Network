import { BrowserProvider, ethers } from "ethers";
import { DecodedError, ErrorDecoder } from "ethers-decode-error";
import { API } from "@paxeer-network/types";
import { IWalletAdapter, WalletAdapterOptions } from "./adapter";

const errorDecoder = ErrorDecoder.create();

export interface EtherAdapterOptions {
  provider: any;
  wallet: {
    name?: string;
  };
  // getAddresses?: (address: string) => string;
  chain: { id: number };
}

/** @deprecated will be remove this in next minor version */
export class EtherAdapter implements IWalletAdapter {
  private provider?: BrowserProvider;
  private _chainId: number;
  private _address: string;
  constructor(options: WalletAdapterOptions) {
    // super();

    // this._chainId = parseInt(options.chain.id, 16);
    this._chainId = options.chain.id;
    this.provider = new BrowserProvider(options.provider, "any");
    this._address = options.address;
  }

  parseUnits(amount: string, decimals: number) {
    return ethers.parseUnits(amount, decimals).toString();
  }
  formatUnits(amount: string, decimals: number) {
    return ethers.formatUnits(amount, decimals);
  }
  getBalance(userAddress: string): Promise<any> {
    // return contract.balanceOf(userAddress);
    return this.provider!.getBalance(userAddress).then(
      (res) => {
        return res;
      },
      (error) => {},
    );
  }

  deposit(from: string, to: string, amount: string): Promise<any> {
    throw new Error("Method not implemented.");
  }

  async call(
    address: string,
    method: string,
    params: any[],
    options: {
      abi: any;
    },
  ): Promise<any> {
    const singer = await this.provider?.getSigner();
    const contract = new ethers.Contract(address, options.abi, singer);

    return contract[method].apply(null, params).catch(async (error) => {
      const parsedEthersError = await parseError(error);
      throw parsedEthersError;
    });
  }

  async callOnChain(
    chain: API.NetworkInfos,
    address: string,
    method: string,
    params: any[],
    options: {
      abi: any;
    },
  ): Promise<any> {
    // const singer = await this.provider?.getSigner();
    // const contract = new ethers.Contract(address, options.abi, singer);

    const provider = new ethers.JsonRpcProvider(chain.public_rpc_url);

    const contract = new ethers.Contract(address, options.abi, provider);

    return contract[method].apply(null, params).catch(async (error) => {
      const parsedEthersError = await parseError(error);
      throw parsedEthersError;
    });
  }

  get chainId(): number {
    return this._chainId;
  }

  set chainId(chainId: number) {
    this._chainId = chainId;
  }

  get addresses(): string {
    return this._address;
  }

  async send(
    method: string,
    params: Array<any> | Record<string, any>,
  ): Promise<any> {
    return await this.provider?.send(method, params);
  }

  async sendTransaction(
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
  ): Promise<ethers.TransactionResponse> {
    const singer = await this.provider?.getSigner();
    if (!singer) {
      throw new Error("singer is not exist");
    }

    const contract = new ethers.Contract(
      contractAddress,
      options.abi,
      this.provider,
    );

    // contract.interface.getAbiCoder().encode(tx.data);
    const encodeFunctionData = contract.interface.encodeFunctionData(
      method,
      payload.data,
    );

    const tx: ethers.TransactionRequest = {
      from: payload.from,
      to: payload.to,
      data: encodeFunctionData,
      value: payload.value,
    };

    // const gas = await this.estimateGas(tx);

    // tx.gasLimit = BigInt(Math.ceil(gas * 1.2));

    try {
      const result = await singer.sendTransaction(tx);

      return result;
    } catch (error) {
      const parsedEthersError = await parseError(error);
      throw parsedEthersError;
    }
  }

  async getTransactionRecipect(txHash: string) {
    await this.provider!.getTransactionReceipt(txHash);
  }

  async pollTransactionReceiptWithBackoff(
    txHash: string,
    baseInterval = 1000,
    maxInterval = 6000,
    maxRetries = 30,
  ) {
    let interval = baseInterval;
    let retries = 0;

    while (retries < maxRetries) {
      try {
        const receipt = await this.provider!.getTransactionReceipt(txHash);
        if (receipt) {
          // completed, get receipt
          return receipt;
        }
      } catch (error) {
        // waiting
      }

      await new Promise((resolve) => setTimeout(resolve, interval));

      interval = Math.min(interval * 2, maxInterval);
      retries++;
    }

    throw new Error("Transaction did not complete after maximum retries.");
  }

  private async estimateGas(tx: ethers.TransactionRequest): Promise<number> {
    const gas = await this.provider!.estimateGas(tx);

    return ethers.toNumber(gas);
  }

  async signTypedData(address: string, data: any) {
    return await this.provider?.send("eth_signTypedData_v4", [address, data]);
  }

  async verify(
    data: { domain: any; message: any; types: any },
    signature: string,
  ) {
    const { domain, types, message } = data;

    const recovered = ethers.verifyTypedData(domain, types, message, signature);
  }

  on(eventName: any, listener: any): void {
    this.provider?.on(eventName, listener);
  }

  off(eventName: any, listener: any): void {
    this.provider?.off(eventName, listener);
  }

  getContract(address: string, abi: any): ethers.Contract {
    const contract = new ethers.Contract(address, abi, this.provider);
    return contract;
  }
}

async function parseError(rawError: any): Promise<DecodedError> {
  const error: DecodedError = await errorDecoder.decode(rawError);
  return error;
}
