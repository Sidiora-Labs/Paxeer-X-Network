import { measureWalletOperation } from './measurement';
import { getPaxeerRpcUrls } from '@/core/rpc';
import { createPublicClient, createWalletClient, custom, defineChain, getAddress, http, type Address, type EIP1193Provider, type Hex } from 'viem';
export const PAXEER_CHAIN_ID = 125;

export const paxeerWalletChain = defineChain({
  id: PAXEER_CHAIN_ID,
  name: 'Paxeer X Network',
  nativeCurrency: { name: 'PAX', symbol: 'PAX', decimals: 18 },
  // Same RPC precedence as wagmi and the CSP connect-src (core/rpc.ts).
  rpcUrls: { default: { http: [getPaxeerRpcUrls()[0]] } },
});

/** Public RPC reads do not authorize signing or imply wallet connection. */
export function getPublicClient(rpcUrl: string = paxeerWalletChain.rpcUrls.default.http[0], signal?: AbortSignal) {
  return createPublicClient({ chain: paxeerWalletChain, transport: http(rpcUrl, signal ? { fetchOptions: { signal }, retryCount: 0 } : undefined) });
}

export function getWalletClient(provider: EIP1193Provider) {
  return createWalletClient({ chain: paxeerWalletChain, transport: custom(provider) });
}

export function getInjectedProvider(): EIP1193Provider | null {
  if (typeof window === 'undefined') return null;
  const candidate = (window as Window & { ethereum?: EIP1193Provider }).ethereum;
  return candidate && typeof candidate.request === 'function' ? candidate : null;
}

export class WrongPaxeerChainError extends Error {
  constructor(readonly actualChainId: number) {
    super(`Wallet is on chain ${actualChainId}; switch to Paxeer chain 125 before signing`);
    this.name = 'WrongPaxeerChainError';
  }
}

export class PaxeerWallet {
  private readonly client;
  constructor(readonly provider: EIP1193Provider) {
    this.client = getWalletClient(provider);
  }

  async getAccounts(): Promise<readonly Address[]> {
    return (await this.client.getAddresses()).map((account) => getAddress(account));
  }

  async proofKind(account: Address): Promise<'eoa_eip191' | 'erc1271'> {
    const authorized = await this.authorizedAccount(account);
    const code = await this.provider.request({ method: 'eth_getCode', params: [authorized, 'latest'] });
    await this.assertChain();
    if (typeof code !== 'string' || !/^0x(?:[0-9a-fA-F]{2})*$/.test(code)) throw new Error('Wallet code lookup failed');
    return code === '0x' ? 'eoa_eip191' : 'erc1271';
  }

  async assertChain(): Promise<void> {
    const chainId = await this.client.getChainId();
    if (chainId !== PAXEER_CHAIN_ID) throw new WrongPaxeerChainError(chainId);
  }

  async connect(): Promise<readonly Address[]> {
    const accounts = await measureWalletOperation('connector.connect', 'external-wallet', () => this.client.requestAddresses());
    await this.assertChain();
    if (!accounts.length) throw new Error('Wallet returned no authorized account');
    return accounts.map((account) => getAddress(account));
  }

  /** Network switching always follows an explicit user action. */
  async switchToPaxeer(): Promise<void> {
    await this.client.switchChain({ id: PAXEER_CHAIN_ID });
    await this.assertChain();
  }

  private async authorizedAccount(expectedAccount: Address): Promise<Address> {
    await this.assertChain();
    const account = getAddress(expectedAccount);
    const accounts = await this.getAccounts();
    if (accounts[0] !== account) throw new Error('Selected wallet account is no longer authorized');
    await this.assertChain();
    return account;
  }

  /** Returns the wallet's opaque EIP191 signature; server authentication must verify EOA or ERC1271 semantics. */
  async signMessage(account: Address, message: string): Promise<Hex> {
    if (!message.length) throw new Error('Signing message must not be empty');
    const authorized = await this.authorizedAccount(account);
    return measureWalletOperation('wallet.sign', 'external-wallet', () => this.client.signMessage({ account: authorized, message }));
  }

  async sendTransaction(account: Address, transaction: { to: Address; value?: bigint; data?: Hex; gas?: bigint; gasPrice?: bigint }): Promise<Hex> {
    if (transaction.value !== undefined && transaction.value < 0n) throw new Error('Transfer amount must not be negative');
    const from = await this.authorizedAccount(account);
    return measureWalletOperation('wallet.send', 'external-wallet', () => this.client.sendTransaction({ ...transaction, to: getAddress(transaction.to), account: from, chain: paxeerWalletChain }));
  }
}

export class InjectedPaxeerWallet extends PaxeerWallet {}

export function getInjectedWallet(): InjectedPaxeerWallet | null {
  const provider = getInjectedProvider();
  return provider ? new InjectedPaxeerWallet(provider) : null;
}

/**
 * Resolve a wagmi connector's EIP-1193 provider. While wagmi is still
 * reconnecting from persisted storage, `useAccount().connector` is the stored
 * stub `{ id, name, type, uid }` (no methods) even though `isConnected` is
 * already true, so this must never call `getProvider` unguarded.
 */
export async function connectorProvider(connector: unknown): Promise<EIP1193Provider> {
  const getProvider = typeof connector === 'object' && connector !== null && 'getProvider' in connector ? (connector as { getProvider?: unknown }).getProvider : undefined;
  if (typeof getProvider !== 'function') throw new Error('Wallet connector is still restoring its provider');
  const provider: unknown = await (getProvider as (this: unknown) => Promise<unknown>).call(connector);
  if (!isWalletProvider(provider)) throw new Error('Wallet provider unavailable');
  return provider;
}

export function isWalletProvider(value: unknown): value is EIP1193Provider {
  return typeof value === 'object' && value !== null && 'request' in value && typeof value.request === 'function' && 'on' in value && typeof value.on === 'function' && 'removeListener' in value && typeof value.removeListener === 'function';
}
