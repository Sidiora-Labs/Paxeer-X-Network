import { IContract } from "@paxeer-network/core";
import {
  Eip1193Provider,
  Web3Provider,
} from "./provider/web3Provider.interface";

export interface EVMAdapterOptions {
  provider: Eip1193Provider;
  address: string;
  chain: { id: number };
  contractManager: IContract;
}

export type getWalletAdapterFunc = (options: EVMAdapterOptions) => Web3Provider;
