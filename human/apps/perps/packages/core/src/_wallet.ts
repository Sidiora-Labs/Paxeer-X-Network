export interface WalletClient {
  // new (address: string): WalletClient;
  get address(): string;
  getBalance: () => Promise<any>;
  deposit: () => Promise<any>;

  connect: () => Promise<any>;
}

export abstract class BaseWalletClient implements WalletClient {
  // address: string;
  constructor(private readonly _address: string) {}
  abstract getBalance(): Promise<any>;
  abstract deposit(): Promise<any>;
  abstract connect(): Promise<any>;
  get address(): string {
    return this._address;
  }
}

export class SimpleWallet extends BaseWalletClient {
  getBalance(): Promise<any> {
    throw new Error("Method not implemented.");
  }
  deposit(): Promise<any> {
    throw new Error("Method not implemented.");
  }
  connect(): Promise<any> {
    throw new Error("Method not implemented.");
  }
}
