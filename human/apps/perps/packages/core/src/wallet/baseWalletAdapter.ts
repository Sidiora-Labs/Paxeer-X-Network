import * as ed from "@noble/ed25519";
import { encode as bs58encode } from "bs58";
import { ethers } from "ethers";
import type { BigNumberish } from "ethers";
import { API, SDKError, ChainNamespace } from "@paxeer-network/types";
import { Account } from "../account";
import { SimpleDI } from "../di/simpleDI";
import { getTimestamp, SignatureDomain } from "../utils";
import {
  Message,
  RegisterAccountInputs,
  WithdrawInputs,
  WalletAdapter,
  SettleInputs,
  AddPaxeerNetworkKeyInputs,
  DexRequestInputs,
  InternalTransferInputs,
} from "./walletAdapter";

abstract class BaseWalletAdapter<Config> implements WalletAdapter<Config> {
  // private readonly contractManager: ContractManager;

  abstract generateRegisterAccountMessage(
    inputs: RegisterAccountInputs,
  ): Promise<Message>;

  abstract generateWithdrawMessage(
    inputs: WithdrawInputs,
  ): Promise<Message & { domain: SignatureDomain }>;

  abstract generateInternalTransferMessage(
    inputs: InternalTransferInputs,
  ): Promise<Message & { domain: SignatureDomain }>;

  abstract generateSettleMessage(
    inputs: SettleInputs,
  ): Promise<Message & { domain: SignatureDomain }>;

  abstract generateAddPaxeerNetworkKeyMessage(
    inputs: AddPaxeerNetworkKeyInputs,
  ): Promise<Message>;

  abstract generateDexRequestMessage(
    inputs: DexRequestInputs,
  ): Promise<Message & { domain: SignatureDomain }>;

  abstract getBalance(): Promise<bigint>;
  abstract getBalances(addresses: string[]): Promise<any>;

  abstract chainNamespace: ChainNamespace;

  async signMessageByPaxeerNetworkKey(payload: any) {
    // signature
    const account = SimpleDI.get<Account>("account");
    const signer = account.signer;
    const signature = await signer.sign(payload, getTimestamp());
    signature["paxeer-network-account-id"] = account.accountId;
    return signature;
  }

  abstract call(
    address: string,
    method: string,
    params: any[],
    options?: {
      abi: any;
    },
  ): Promise<any>;

  abstract sendTransaction(
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

  abstract callOnChain(
    chain: API.NetworkInfos,
    address: string,
    method: string,
    params: any[],
    options: {
      abi: any;
    },
  ): Promise<any>;

  abstract estimateGasFee(
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

  get address(): string {
    throw new SDKError("Method not implemented.");
  }

  get chainId(): number {
    throw new SDKError("Method not implemented.");
  }

  active(config: Config): void {
    throw new SDKError("Method not implemented.");
  }

  abstract deactivate(): void;

  update(config: Config): void {
    throw new SDKError("Method not implemented.");
  }

  generateSecretKey(): string {
    let privateKey, secretKey;
    do {
      privateKey = ed.utils.randomPrivateKey();
      secretKey = bs58encode(privateKey);
    } while (secretKey.length !== 44);

    return secretKey;
  }

  parseUnits(amount: string, decimals: number) {
    return ethers.parseUnits(amount, decimals).toString();
  }

  formatUnits(amount: BigNumberish, decimals: number) {
    return ethers.formatUnits(amount, decimals);
  }

  on(eventName: string, listener: (...args: any[]) => void): void {
    throw new SDKError("Method not implemented.");
  }

  off(eventName: string, listener: (...args: any[]) => void): void {
    throw new SDKError("Method not implemented.");
  }

  abstract pollTransactionReceiptWithBackoff(
    txHash: string,
    baseInterval?: number,
    maxInterval?: number,
    maxRetries?: number,
  ): Promise<any>;
}

export { BaseWalletAdapter };
