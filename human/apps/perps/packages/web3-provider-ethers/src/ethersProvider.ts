import { BrowserProvider, Eip1193Provider, ethers } from "ethers";
import { Web3Provider } from "@paxeer-network/default-evm-adapter";
import { API, isNativeTokenChecker } from "@paxeer-network/types";
import { parseError } from "./parseError";

function isContractCallFailure(error: unknown): boolean {
  if (!ethers.isError(error, "CALL_EXCEPTION")) {
    return false;
  }

  if (error.data !== null || error.reason !== null || error.revert !== null) {
    return true;
  }

  const rpcError = error.info?.error as { message?: unknown } | undefined;
  return (
    typeof rpcError?.message === "string" &&
    /execution reverted/iu.test(rpcError.message)
  );
}

class EthersProvider implements Web3Provider {
  private _provider!: BrowserProvider;
  private _originalProvider!: Eip1193Provider;

  parseUnits(amount: string, decimals?: number): string {
    return ethers.parseUnits(amount, decimals).toString();
  }

  formatUnits(amount: string, decimals?: number): string {
    return ethers.formatUnits(amount, decimals);
  }

  set provider(provider: Eip1193Provider) {
    this._provider = new BrowserProvider(provider, "any");
    this._originalProvider = provider;
  }

  get browserProvider(): BrowserProvider {
    if (!this._provider) {
      throw new Error("browserProvider is not initialized");
    }
    return this._provider;
  }

  async signTypedData(
    address: string,
    toSignatureMessage: any,
  ): Promise<string> {
    return await this.browserProvider?.send("eth_signTypedData_v4", [
      address,
      toSignatureMessage,
    ]);
  }

  async call(
    address: string,
    method: string,
    params: any[],
    options: { abi: any },
  ): Promise<any> {
    const writeMethod: string[] = ["approve"];
    // @ts-ignore
    if (this._originalProvider.agwWallet && writeMethod.includes(method)) {
      try {
        // @ts-ignore
        const transactionHash = await this._originalProvider.writeContract({
          abi: options.abi,
          address: address,
          functionName: method,
          args: params,
        });

        return {
          hash: transactionHash,
        };
      } catch (error) {
        // const parsedEthersError = getParsedEthersError(error as EthersError);
        // if ((error as any).message.includes("rejected")) {
        //   // @ts-ignore
        //   throw new Error({ content: "REJECTED_TRANSACTION" });
        // }
        const parsedEthersError = await parseError(error);
        throw parsedEthersError;
      }
    }
    const singer = await this.browserProvider.getSigner();
    const contract = new ethers.Contract(address, options.abi, singer);

    return contract[method].apply(null, params).catch(async (error) => {
      const parsedEthersError = await parseError(error);
      throw parsedEthersError;
    });
  }

  send(method: string, params: Array<any> | Record<string, any>): Promise<any> {
    return this.browserProvider.send(method, params);
  }

  async sendTransaction(
    contractAddress: string,
    method: string,
    payload: { from: string; to?: string; data: any[]; value?: bigint },
    options: { abi: any },
  ): Promise<any> {
    const singer = await this.browserProvider?.getSigner();
    if (!singer) {
      throw new Error("singer is not exist");
    }

    const contract = new ethers.Contract(
      contractAddress,
      options.abi,
      this.browserProvider,
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

    try {
      // for agw wallet
      if (method === "depositTo") {
        // @ts-ignore
        return await this._originalProvider.sendTransaction({
          to: contractAddress,
          data: encodeFunctionData,
          value: payload.value,
        });
      }
      return await singer.sendTransaction(tx);
    } catch (error) {
      const parsedEthersError = await parseError(error);
      throw parsedEthersError;
    }
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
        const receipt =
          await this.browserProvider!.getTransactionReceipt(txHash);
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

  callOnChain(
    chain: API.NetworkInfos,
    address: string,
    method: string,
    params: any[],
    options: { abi: any },
  ): Promise<any> {
    const provider = new ethers.JsonRpcProvider(chain.public_rpc_url);

    const contract = new ethers.Contract(address, options.abi, provider);

    return contract[method].apply(null, params).catch(async (error) => {
      const parsedEthersError = await parseError(error);
      throw parsedEthersError;
    });
  }

  getBalance(userAddress: string): Promise<bigint> {
    return this.browserProvider!.getBalance(userAddress).then((res) => {
      return res;
    });
  }

  async getBalances(addresses: string[]): Promise<string[]> {
    // if addresses is empty, return empty array
    if (!addresses || addresses.length === 0) {
      return [];
    }

    // Multicall3 contract address, support most EVM chains
    const MULTICALL3_ADDRESS = "0xcA11bde05977b3631167028862bE2a173976CA11";

    // extended ABI, add getEthBalance
    const MULTICALL_ABI = [
      "function tryAggregate(bool requireSuccess, tuple(address target, bytes callData)[] calls) public view returns (tuple(bool success, bytes returnData)[] returnData)",
      "function getEthBalance(address addr) view returns (uint256 balance)", // Multicall3 自带的方法
    ];

    // simplified ERC20 ABI
    const ERC20_ABI = [
      "function balanceOf(address account) view returns (uint256)",
    ];

    try {
      // get user address
      const signer = await this.browserProvider.getSigner();
      const userAddress = await signer.getAddress();
      const erc20Interface = new ethers.Interface(ERC20_ABI);
      const multicallInterface = new ethers.Interface(MULTICALL_ABI);

      const multicallCode =
        await this.browserProvider.getCode(MULTICALL3_ADDRESS);
      if (multicallCode === "0x") {
        return Promise.all(
          addresses.map(async (tokenAddr) => {
            if (isNativeTokenChecker(tokenAddr)) {
              const balance =
                await this.browserProvider.getBalance(userAddress);
              return balance.toString();
            }

            let returnData: string;
            try {
              returnData = await this.browserProvider.call({
                to: tokenAddr,
                data: erc20Interface.encodeFunctionData("balanceOf", [
                  userAddress,
                ]),
              });
            } catch (error) {
              if (isContractCallFailure(error)) {
                console.warn("Failed to fetch token balance:", error);
                return "0";
              }
              throw error;
            }

            if (returnData === "0x") {
              return "0";
            }

            try {
              const decoded = erc20Interface.decodeFunctionResult(
                "balanceOf",
                returnData,
              );
              return decoded?.[0]?.toString() || "0";
            } catch (error) {
              console.warn("Failed to decode balanceOf result:", error);
              return "0";
            }
          }),
        );
      }

      // create contract interface instance (for encoding and decoding data)
      const multicallContract = new ethers.Contract(
        MULTICALL3_ADDRESS,
        MULTICALL_ABI,
        this.browserProvider,
      );

      // build Calls array
      // we need to encode the call to balanceOf into a hex string
      const calls = addresses.map((tokenAddr) => {
        if (isNativeTokenChecker(tokenAddr)) {
          // CASE A: if it's a native token
          // target address: Multicall3 contract itself
          // call method: getEthBalance(user)
          return {
            target: MULTICALL3_ADDRESS,
            callData: multicallInterface.encodeFunctionData("getEthBalance", [
              userAddress,
            ]),
          };
        }
        return {
          target: tokenAddr,
          callData: erc20Interface.encodeFunctionData("balanceOf", [
            userAddress,
          ]),
        };
      });

      // call Multicall
      // the first parameter of tryAggregate(false, calls) is false, which means:
      // if a token call fails, don't revert the entire transaction, but return false status
      const results = await multicallContract.tryAggregate.staticCall(
        false,
        calls,
      );

      // decode results
      const balances = results.map(
        (result: [boolean, string], index: number) => {
          const [success, returnData] = result;
          if (success && returnData !== "0x") {
            const tokenAddr = addresses[index];
            const isNative = isNativeTokenChecker(tokenAddr);
            try {
              let decoded;
              if (isNative) {
                // Native Token returns directly as uint256, decode with multicallInterface
                decoded = multicallInterface.decodeFunctionResult(
                  "getEthBalance",
                  returnData,
                );
              } else {
                // ERC20 returns also uint256, decode with erc20Interface
                decoded = erc20Interface.decodeFunctionResult(
                  "balanceOf",
                  returnData,
                );
              }
              // decode returned bytes data
              // const decoded = erc20Interface.decodeFunctionResult(
              //   "balanceOf",
              //   returnData,
              // );
              // decoded[0] is BigInt, need to explicitly convert to string
              return decoded?.[0]?.toString() || "0";
            } catch (error) {
              // if decoding fails, return "0"
              console.warn("Failed to decode balanceOf result:", error);
              return "0";
            }
          } else {
            // if call fails (e.g. target is not a contract, or is an error address), return "0"
            return "0";
          }
        },
      );

      return balances;
    } catch (error) {
      console.error("getBalances error", error);
      const parsedEthersError = await parseError(error);
      throw parsedEthersError;
    }
  }

  async estimateGasFee(
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
  ): Promise<bigint> {
    const contract = new ethers.Contract(
      contractAddress,
      options.abi,
      this.browserProvider,
    );
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

    let gas = BigInt(0);
    const defaultGas = BigInt(800000);
    try {
      gas = await this.browserProvider?.estimateGas(tx);
    } catch (error) {
      gas = defaultGas;
      console.error("estimateGasFee error", error);
    }

    // current gas price
    const feeData = await this.browserProvider.getFeeData();

    const gasPrice = feeData.gasPrice ?? feeData.maxFeePerGas;

    if (!gasPrice) {
      throw new Error("gasPrice is not available");
    }

    // transaction cost in wei
    const costInWei = gas * gasPrice;

    return costInWei;
  }
}

export { EthersProvider };
