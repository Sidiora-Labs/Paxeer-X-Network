import { UnauthorizedError, InvalidParamsError, ChainDisconnectedError } from './errors.js';
import type { Eip6963ProviderDetail, Eip6963ProviderInfo } from './eip6963.js';
import { PaxeerProvider, PAXEER_CHAIN_ID } from './provider.js';
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
  private generation = 0;
  private disposed = false;
  private readonly invalidate = () => { this.generation += 1; };

  constructor(
    readonly provider: Eip1193Provider,
    readonly info?: Eip6963ProviderInfo,
    readonly admission?: { readonly chainId: number; readonly account: Hex; readonly current: () => boolean },
  ) {
    if (admission) {
      if (!Number.isSafeInteger(admission.chainId) || admission.chainId <= 0 || !/^0x[0-9a-fA-F]{40}$/.test(admission.account)) {
        throw new InvalidParamsError('admission', 'the admitted account and chain are malformed');
      }
      this.admission = Object.freeze({ ...admission });
    }
    this.mode = provider instanceof PaxeerProvider ? 'embedded' : 'injected';
    if (this.mode === 'injected') {
      for (const event of ['accountsChanged', 'chainChanged', 'disconnect'] as const) provider.on(event, this.invalidate);
    }
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
    const chain = Number(BigInt(result));
    if (!Number.isSafeInteger(chain) || chain <= 0) throw new InvalidParamsError('chainId', 'the provider returned an out-of-range chain id');
    return chain;
  }

  async sendTransaction(tx: WalletTransaction): Promise<Hex> {
    const generation = this.generation;
    const from = await this.writeAccount(generation);
    const expected = this.admission?.chainId ?? (this.mode === 'injected' ? PAXEER_CHAIN_ID : await this.chainId());
    if (tx.chainId !== undefined && tx.chainId !== expected) throw new ChainDisconnectedError(expected, tx.chainId);
    const params: TransactionParams = { from, chainId: quantity(BigInt(expected)) };
    if (tx.to !== undefined) params.to = tx.to;
    if (tx.data !== undefined) params.data = tx.data;
    if (tx.value !== undefined) params.value = quantity(tx.value);
    if (tx.gas !== undefined) params.gas = quantity(tx.gas);
    if (tx.maxFeePerGas !== undefined) params.maxFeePerGas = quantity(tx.maxFeePerGas);
    if (tx.maxPriorityFeePerGas !== undefined) params.maxPriorityFeePerGas = quantity(tx.maxPriorityFeePerGas);
    if (tx.nonce !== undefined) params.nonce = quantity(BigInt(tx.nonce));
    await this.checkWrite(from, expected, generation);
    this.assertCurrent(generation);
    return hex(await this.provider.request({ method: 'eth_sendTransaction', params: [params] }), 'transaction hash');
  }

  async signTypedData(typedData: TypedDataPayload): Promise<Hex> {
    const generation = this.generation;
    const from = await this.writeAccount(generation);
    const expected = this.admission?.chainId ?? (this.mode === 'injected' ? PAXEER_CHAIN_ID : await this.chainId());
    if (typedData.domain?.chainId !== undefined) {
      const supplied = typedData.domain.chainId;
      if (!['string', 'number', 'bigint'].includes(typeof supplied)) throw new InvalidParamsError('domain.chainId', 'the typed data chain is malformed');
      let chain: bigint;
      try { chain = BigInt(supplied as string | number | bigint); } catch { throw new InvalidParamsError('domain.chainId', 'the typed data chain is malformed'); }
      if (chain !== BigInt(expected)) throw new ChainDisconnectedError(expected, Number(chain));
    }
    const payload = JSON.stringify(typedData, (_key, value: unknown) =>
      typeof value === 'bigint' ? value.toString() : value,
    );
    await this.checkWrite(from, expected, generation);
    this.assertCurrent(generation);
    const result = await this.provider.request({ method: 'eth_signTypedData_v4', params: [from, payload] });
    this.assertCurrent(generation);
    return hex(result, 'signature');
  }

  async signMessage(message: string): Promise<Hex> {
    const generation = this.generation;
    const from = await this.writeAccount(generation);
    const expected = this.admission?.chainId ?? (this.mode === 'injected' ? PAXEER_CHAIN_ID : await this.chainId());
    const bytes = new TextEncoder().encode(message);
    const encoded = `0x${Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')}` as Hex;
    await this.checkWrite(from, expected, generation);
    this.assertCurrent(generation);
    const result = await this.provider.request({ method: 'personal_sign', params: [encoded, from] });
    this.assertCurrent(generation);
    return hex(result, 'signature');
  }

  async signCustody(custody: Hex): Promise<Hex> {
    if (this.mode !== 'embedded') {
      throw new UnauthorizedError('custody_not_supported', 'custody hand-off signing requires the embedded wallet');
    }
    const generation = this.generation;
    const from = await this.writeAccount(generation);
    const expected = this.admission?.chainId ?? await this.chainId();
    await this.checkWrite(from, expected, generation);
    this.assertCurrent(generation);
    const result = await this.provider.request({ method: 'paxeer_signCustody', params: [{ custody }] });
    this.assertCurrent(generation);
    return hex(result, 'signature');
  }

  on(event: ProviderEvent, listener: ProviderListener): this {
    this.provider.on(event, listener);
    return this;
  }

  off(event: ProviderEvent, listener: ProviderListener): this {
    this.provider.removeListener(event, listener);
    return this;
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    this.generation += 1;
    if (this.mode === 'injected') {
      for (const event of ['accountsChanged', 'chainChanged', 'disconnect'] as const) this.provider.removeListener(event, this.invalidate);
    }
  }

  private assertCurrent(generation: number): void {
    if (this.disposed || generation !== this.generation || (this.admission && !this.admission.current())) {
      throw new UnauthorizedError('session_changed', 'the wallet account or chain changed; reconnect before signing');
    }
  }

  private async writeAccount(generation: number): Promise<Hex> {
    this.assertCurrent(generation);
    const from = await this.account();
    this.assertCurrent(generation);
    if (this.admission && from.toLowerCase() !== this.admission.account.toLowerCase()) {
      throw new UnauthorizedError('account_mismatch', 'the provider account no longer matches the admitted account');
    }
    return from;
  }

  private async checkWrite(from: Hex, expected: number, generation: number): Promise<void> {
    const accounts = await this.provider.request({ method: 'eth_accounts' });
    if (!Array.isArray(accounts) || typeof accounts[0] !== 'string' || !/^0x[0-9a-fA-F]{40}$/.test(accounts[0])) {
      throw new UnauthorizedError('not_connected', 'the provider exposed no authorized account');
    }
    if (accounts[0].toLowerCase() !== from.toLowerCase()) throw new UnauthorizedError('account_mismatch', 'the provider account changed');
    const chain = await this.chainId();
    this.assertCurrent(generation);
    if (chain !== expected) throw new ChainDisconnectedError(expected, chain);
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
