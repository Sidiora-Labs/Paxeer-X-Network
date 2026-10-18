import * as ed from "@noble/ed25519";
import { encode as bs58encode } from "bs58";
import {
  AddPaxeerNetworkKeyInputs,
  BaseWalletAdapter,
  IContract,
  Message,
  RegisterAccountInputs,
  SettleInputs,
  SignatureDomain,
  InternalTransferInputs,
  WithdrawInputs,
  DexRequestInputs,
} from "@paxeer-network/core";
import { API, ChainNamespace } from "@paxeer-network/types";
import {
  addPaxeerNetworkKeyMessage,
  registerAccountMessage,
  settleMessage,
  internalTransferMessage,
  withdrawMessage,
  dexRequestMessage,
} from "./helper";
import type { Web3Provider } from "./provider/web3Provider.interface";
import { EVMAdapterOptions } from "./types";

class DefaultEVMWalletAdapter extends BaseWalletAdapter<EVMAdapterOptions> {
  chainNamespace: ChainNamespace = ChainNamespace.evm;
  private _address!: string;
  private _chainId!: number;
  private contractManager!: IContract;

  constructor(private readonly web3Provider: Web3Provider) {
    super();
  }

  get address(): string {
    return this._address;
  }

  get chainId(): number {
    return this._chainId;
  }

  set chainId(chainId: number) {
    this._chainId = chainId;
  }

  private setConfig(config: EVMAdapterOptions): void {
    this._address = config.address;
    this._chainId = config.chain.id;

    if (config.provider) {
      this.web3Provider.provider = config.provider;
    }

    if (config.contractManager) {
      this.contractManager = config.contractManager;
    } else {
      throw new Error("Please provide contract manager class");
    }
  }

  active(config: EVMAdapterOptions): void {
    this.setConfig(config);
    this.lifecycleName("active", config);
  }

  deactivate(): void {
    this.lifecycleName("deactivate", {});
  }

  update(config: EVMAdapterOptions): void {
    this.lifecycleName("update", config);
    this.setConfig(config);
  }

  generateSecretKey(): string {
    let privKey, secretKey;
    do {
      privKey = ed.utils.randomPrivateKey();
      secretKey = bs58encode(privKey);
    } while (secretKey.length !== 44);

    return secretKey;
  }

  private lifecycleName(name: string, data: any) {
    console.log("lifecycle", name, data);
  }

  private async signTypedData(toSignatureMessage: Record<string, any>) {
    console.log("toSignatureMessage", this.address);
    return await this.web3Provider.signTypedData(
      // address,
      this.address,
      // toSignatureMessage
      JSON.stringify(toSignatureMessage),
    );
  }

  async generateRegisterAccountMessage(
    inputs: RegisterAccountInputs,
  ): Promise<Message> {
    const [message, toSignatureMessage] = await registerAccountMessage({
      ...inputs,

      chainId: this.chainId,
      domain: this.getDomain(),
    });

    const signedMessage = await this.signTypedData(toSignatureMessage);

    return {
      message: {
        ...message,
        chainType: "EVM",
      },
      signatured: signedMessage,
    };
  }

  async generateAddPaxeerNetworkKeyMessage(
    inputs: AddPaxeerNetworkKeyInputs,
  ): Promise<Message> {
    const [message, toSignatureMessage] = await addPaxeerNetworkKeyMessage({
      ...inputs,
      chainId: this.chainId,
      domain: this.getDomain(),
    });

    const signedMessage = await this.signTypedData(toSignatureMessage);

    return {
      message: {
        ...message,
        chainType: "EVM",
      },
      signatured: signedMessage,
    };
  }

  async generateWithdrawMessage(inputs: WithdrawInputs): Promise<
    Message & {
      domain: SignatureDomain;
    }
  > {
    // const { chainId, receiver, token, amount, nonce, brokerId, timestamp } = inputs;
    const domain = this.getDomain(true);

    const [message, toSignatureMessage] = await withdrawMessage({
      ...inputs,
      chainId: this.chainId,
      domain,
    });

    const signedMessage = await this.signTypedData(toSignatureMessage);

    return {
      message: {
        ...message,
        chainType: "EVM",
      },
      signatured: signedMessage,
      domain,
    };
  }

  async generateInternalTransferMessage(
    inputs: InternalTransferInputs,
  ): Promise<
    Message & {
      domain: SignatureDomain;
    }
  > {
    const domain = this.getDomain(true);

    const [message, toSignatureMessage] = await internalTransferMessage({
      ...inputs,
      chainId: this.chainId,
      domain,
    });

    const signedMessage = await this.signTypedData(toSignatureMessage);

    return {
      message: {
        ...message,
        chainType: "EVM",
      },
      signatured: signedMessage,
      domain,
    };
  }

  async generateSettleMessage(inputs: SettleInputs): Promise<
    Message & {
      domain: SignatureDomain;
    }
  > {
    const domain = this.getDomain(true);
    const [message, toSignatureMessage] = await settleMessage({
      ...inputs,
      chainId: this.chainId,
      domain,
    });

    const signedMessage = await this.signTypedData(toSignatureMessage);

    return {
      message: {
        ...message,
        chainType: "EVM",
      },
      signatured: signedMessage,
      domain,
    };
  }

  async generateDexRequestMessage(inputs: DexRequestInputs): Promise<
    Message & {
      domain: SignatureDomain;
    }
  > {
    const [message, toSignatureMessage] = await dexRequestMessage({
      ...inputs,
      chainId: this.chainId,
    });

    const signedMessage = await this.signTypedData(toSignatureMessage);

    return {
      message: {
        ...message,
        chainType: "EVM",
      },
      signatured: signedMessage,
      domain: inputs.domain,
    };
  }

  call(
    address: string,
    method: string,
    params: any[],
    options?: { abi: any },
  ): Promise<any> {
    return this.web3Provider.call(address, method, params, options);
  }

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
  ): Promise<any> {
    return this.web3Provider.sendTransaction(
      contractAddress,
      method,
      payload,
      options,
    );
  }

  callOnChain(
    chain: API.NetworkInfos,
    address: string,
    method: string,
    params: any[],
    options: { abi: any },
  ): Promise<any> {
    return this.web3Provider.callOnChain(
      chain,
      address,
      method,
      params,
      options,
    );
  }

  getDomain(onChainDomain?: boolean): SignatureDomain {
    if (!this.web3Provider) {
      throw new Error("web3Provider is undefined");
    }
    const chainId = this.chainId;

    // const {verifyContractAddress} = this.contract.getContractInfoByEnv();
    return {
      name: "paxeer-network",
      version: "1",
      chainId,
      verifyingContract: onChainDomain
        ? this.contractManager.getContractInfoByEnv().verifyContractAddress
        : "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC",
    };
  }

  pollTransactionReceiptWithBackoff(
    txHash: string,
    baseInterval?: number,
    maxInterval?: number,
    maxRetries?: number,
  ): Promise<any> {
    return this.web3Provider.pollTransactionReceiptWithBackoff(
      txHash,
      baseInterval,
      maxInterval,
      maxRetries,
    );
  }

  getBalance(): Promise<bigint> {
    return this.web3Provider.getBalance(this.address);
  }

  getBalances(addresses: string[]): Promise<any> {
    return this.web3Provider.getBalances(addresses);
  }

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
  ): Promise<bigint> {
    return this.web3Provider.estimateGasFee(
      contractAddress,
      method,
      payload,
      options,
    );
  }
}

export { DefaultEVMWalletAdapter };
