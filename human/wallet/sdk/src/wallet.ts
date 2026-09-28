import { UnauthorizedError, InvalidParamsError } from './errors.js';
import type { Eip6963ProviderDetail, Eip6963ProviderInfo } from './eip6963.js';
import { PaxeerProvider } from './provider.js';
import type {
  Eip1193Provider,
  Hex,
  ProviderEvent,
  ProviderListener,
  TransactionParams,
  TypedDataPayload,
} from './types.js';

export type CustodyMode = 'embedded' | 'injected';

export interface WalletTransaction {
  to?: Hex;
  value?: bigint;
  data?: Hex;
  gas?: bigint;
  maxFeePerGas?: bigint;
  maxPriorityFeePerGas?: bigint;
  nonce?: number;
  chainId?: number;
}

export class WalletInterface {
  readonly mode: CustodyMode;

  constructor(
    readonly provider: Eip1193Provider,
    readonly info?: Eip6963ProviderInfo,
  ) {
    this.mode = provider instanceof PaxeerProvider ? 'embedded' : 'injected';
  }

  async accounts(): Promise<Hex[]> {
    const result = await this.provider.request({ method: 'eth_requestAccounts' });
    if (!Array.isArray(result) || !result.every((a) => typeof a === 'string' && /^0x[0-9a-fA-F]{40}$/.test(a))) {
      throw new InvalidParamsError('accounts', 'the provider returned malformed accounts');
    }
    return result as Hex[];
  }

  async chainId(): Promise<number> {
    const result = await this.provider.request({ method: 'eth_chainId' });
    if (typeof result !== 'string' || !/^0x[0-9a-fA-F]+$/.test(result)) {
      throw new InvalidParamsError('chainId', 'the provider returned a malformed chain id');
    }
    return Number(BigInt(result));
  }

  async sendTransaction(tx: WalletTransaction): Promise<Hex> {
    const from = await this.account();
    const params: TransactionParams = { from };
    if (tx.to !== undefined) params.to = tx.to;
    if (tx.data !== undefined) params.data = tx.data;
    if (tx.value !== undefined) params.value = quantity(tx.value);
    if (tx.gas !== undefined) params.gas = quantity(tx.gas);
    if (tx.maxFeePerGas !== undefined) params.maxFeePerGas = quantity(tx.maxFeePerGas);
    if (tx.maxPriorityFeePerGas !== undefined) params.maxPriorityFeePerGas = quantity(tx.maxPriorityFeePerGas);
    if (tx.nonce !== undefined) params.nonce = quantity(BigInt(tx.nonce));
    if (tx.chainId !== undefined) params.chainId = quantity(BigInt(tx.chainId));
    return hex(await this.provider.request({ method: 'eth_sendTransaction', params: [params] }), 'transaction hash');
  }

  async signTypedData(typedData: TypedDataPayload): Promise<Hex> {
    const from = await this.account();
    const payload = JSON.stringify(typedData, (_key, value: unknown) =>
      typeof value === 'bigint' ? value.toString() : value,
    );
    return hex(await this.provider.request({ method: 'eth_signTypedData_v4', params: [from, payload] }), 'signature');
  }

  async signMessage(message: string): Promise<Hex> {
    const from = await this.account();
    const bytes = new TextEncoder().encode(message);
    const encoded = `0x${Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')}` as Hex;
    return hex(await this.provider.request({ method: 'personal_sign', params: [encoded, from] }), 'signature');
  }

  async signCustody(custody: Hex): Promise<Hex> {
    if (this.mode !== 'embedded') {
      throw new UnauthorizedError('custody_not_supported', 'custody hand-off signing requires the embedded wallet');
    }
    await this.account();
    return hex(await this.provider.request({ method: 'paxeer_signCustody', params: [{ custody }] }), 'signature');
  }

  on(event: ProviderEvent, listener: ProviderListener): this {
    this.provider.on(event, listener);
    return this;
  }

  off(event: ProviderEvent, listener: ProviderListener): this {
    this.provider.removeListener(event, listener);
    return this;
  }

  private async account(): Promise<Hex> {
    const [first] = await this.accounts();
    if (!first) throw new UnauthorizedError('not_connected', 'the provider exposed no account');
    return first;
  }
}

export function walletInterfaces(
  embedded: PaxeerProvider,
  embeddedInfo: Eip6963ProviderInfo | undefined,
  injected: readonly Eip6963ProviderDetail[],
): WalletInterface[] {
  const wallets = [new WalletInterface(embedded, embeddedInfo)];
  for (const detail of injected) {
    if (detail.provider === embedded) continue;
    wallets.push(new WalletInterface(detail.provider, detail.info));
  }
  return wallets;
}

function quantity(value: bigint): Hex {
  if (value < 0n) throw new InvalidParamsError('quantity', 'quantities must be non-negative');
  return `0x${value.toString(16)}`;
}

function hex(value: unknown, what: string): Hex {
  if (typeof value !== 'string' || !/^0x[0-9a-fA-F]*$/.test(value)) {
    throw new InvalidParamsError(what, `the provider returned a malformed ${what}`);
  }
  return value as Hex;
}
