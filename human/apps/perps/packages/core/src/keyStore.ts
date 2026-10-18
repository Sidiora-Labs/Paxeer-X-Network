import { BasePaxeerNetworkKeyPair, PaxeerNetworkKeyPair } from "./keyPair";

export interface PaxeerNetworkKeyStore {
  getPaxeerNetworkKey: (address?: string) => PaxeerNetworkKeyPair | null;
  getAccountId: (address: string) => string | undefined | null;
  setAccountId: (address: string, accountId: string) => void;
  getAddress: () => string | undefined | null;
  setAddress: (address: string) => void;
  removeAddress: () => void;
  generateKey: () => PaxeerNetworkKeyPair;
  cleanKey: (address: string, key: string) => void;
  cleanAllKey: (address: string) => void;
  setKey: (paxeerNetworkKey: string, secretKey: PaxeerNetworkKeyPair) => void;
}

export abstract class BaseKeyStore implements PaxeerNetworkKeyStore {
  constructor(
    private readonly networkId: string = "testnet" // private readonly accountId: string
  ) {}
  abstract getPaxeerNetworkKey(address?: string): PaxeerNetworkKeyPair | null;
  abstract getAccountId(address: string): string | undefined | null;
  abstract setAccountId(address: string, accountId: string): void;
  abstract getAddress(): string | undefined | null;
  abstract setAddress(address: string): void;
  abstract removeAddress(): void;
  abstract generateKey(): PaxeerNetworkKeyPair;
  abstract setKey(paxeerNetworkKey: string, secretKey: PaxeerNetworkKeyPair): void;
  abstract cleanAllKey(address: string): void;
  abstract cleanKey(address: string, key: string): void;

  protected get keyPrefix() {
    return `paxeerNetwork_${this.networkId}_`;
  }
}

export class LocalStorageStore extends BaseKeyStore {
  getPaxeerNetworkKey(address?: string): PaxeerNetworkKeyPair | null {
    //TODO: get secretKey from localStorage, and return a PaxeerNetworkKeyPair
    //
    let paxeerNetworkKey;

    if (address) {
      paxeerNetworkKey = this.getItem(address, "paxeerNetworkKey");
    } else {
      //if address is undefined, get the default address
      const address = this.getAddress();
      if (!address) return null;
      paxeerNetworkKey = this.getItem(address, "paxeerNetworkKey");
    }

    if (!paxeerNetworkKey) {
      return null;
    }
    return new BasePaxeerNetworkKeyPair(paxeerNetworkKey);
  }

  getAccountId(address: string): string | undefined | null {
    return this.getItem(address, "accountId");
  }

  setAccountId(address: string, accountId: string) {
    // localStorage.setItem(`${this.keyPrefix}accountId`, accountId);
    this.setItem(address, { accountId });
  }

  getAddress(): string | undefined | null {
    return localStorage.getItem(`${this.keyPrefix}address`);
  }

  setAddress(address: string) {
    localStorage.setItem(`${this.keyPrefix}address`, address);
    // this.setItem(address, { address });
  }
  removeAddress() {
    localStorage.removeItem(`${this.keyPrefix}address`);
  }

  generateKey() {
    return BasePaxeerNetworkKeyPair.generateKey();
  }

  setKey(address: string, paxeerNetworkKey: PaxeerNetworkKeyPair) {
    // localStorage.setItem(`${this.keyPrefix}paxeerNetworkKey`, paxeerNetworkKey);
    // localStorage.setItem(`${this.keyPrefix}secretKey`, secretKey);
    this.setItem(address, { paxeerNetworkKey: paxeerNetworkKey.secretKey });
  }

  cleanAllKey(address: string): void {
    localStorage.removeItem(`${this.keyPrefix}${address}`);
    localStorage.removeItem(`${this.keyPrefix}address`);
  }
  cleanKey(address: string, key: string): void {
    const data = this.getItem(address);
    delete data[key];
    localStorage.setItem(`${this.keyPrefix}${address}`, JSON.stringify(data));
  }

  private setItem(address: string, value: Record<string, any>) {
    const key = `${this.keyPrefix}${address}`;
    let oldValue: any = localStorage.getItem(key);

    if (oldValue) {
      oldValue = JSON.parse(oldValue);
    } else {
      oldValue = {};
    }

    localStorage.setItem(
      key,
      JSON.stringify({
        ...oldValue,
        ...value,
      })
    );
  }

  private getItem(address: string, name?: string) {
    const key = `${this.keyPrefix}${address}`;
    let value: any = localStorage.getItem(key);

    if (value) {
      value = JSON.parse(value);
    } else {
      value = {};
    }

    if (typeof name === "undefined") {
      return value;
    }

    return value[name];
  }
}

// for readux manage state
export class MockKeyStore implements PaxeerNetworkKeyStore {
  constructor(private readonly secretKey: string) {}

  generateKey() {
    return new BasePaxeerNetworkKeyPair(this.secretKey);
  }

  getPaxeerNetworkKey() {
    return new BasePaxeerNetworkKeyPair(this.secretKey);
  }

  getAccountId() {
    return "";
  }

  setAccountId(accountId: string) {}

  getAddress() {
    return "";
  }

  setAddress(address: string) {}

  removeAddress() {}

  setKey(paxeerNetworkKey: string, secretKey: PaxeerNetworkKeyPair): void {}

  cleanAllKey(): void {}
  cleanKey(key: string): void {}
}
