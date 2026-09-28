import type { AuthChangeEvent, Session, User } from '@supabase/supabase-js';
import type { IWallet, WalletKind } from '../ports/IWallet';
import type { IEventBus } from '../ports/IEventBus';
import type { TransactionData } from '../types';
import { WalletEvents } from '../types';
import { SimpleEventBus } from '../adapters/SimpleEventBus';
import { PaxeerWallet } from './sdk';
import type {
  ChainInfo,
  OAuthProvider,
  PaxeerEmbeddedConfig,
  PublicWallet,
} from './sdk/types';

/**
 * High-level facade for the **Paxeer Embedded Wallet** custody model.
 *
 * This is the embedded counterpart to `/paxport/wallet/PaxeerWallet.ts`
 * (self-custody). The two share a small `IWallet` surface so PWA UI code
 * can drive either implementation polymorphically while still exposing the
 * embedded-specific auth surface (email / OAuth sign-in, session listeners)
 * that PWA onboarding screens need.
 *
 * Custody model — important for the PWA onboarding screen copy:
 *   - User authenticates with Supabase (email magic link or OAuth).
 *   - The Paxeer wallet API auto-provisions an EVM EOA on first call to
 *     `getReceiveAddress()` / `send()`. The private key is encrypted with
 *     the wallet master key and stored in the Paxeer-managed Postgres at
 *     `connect.paxportwallet.com`. **Paxeer holds custody.**
 *   - The user gets the same wallet address on every Paxeer app they sign
 *     into with the same identity provider. No popups, no signature prompts,
 *     no seed phrase to back up.
 *
 * Lifecycle for the PWA:
 *
 *   ```ts
 *   const wallet = new EmbeddedWallet({
 *     apiUrl: 'https://connect.paxportwallet.com',
 *     supabaseUrl: '...',
 *     supabaseAnonKey: '...',
 *     rpcUrl: 'https://public-mainnet.rpcpaxeer.online/evm',
 *   });
 *
 *   // Onboarding
 *   await wallet.signInWithOAuth('google', `${origin}/auth/callback`);
 *   // …user redirects through Google, lands on /auth/callback…
 *
 *   // After session lands the wallet is ready
 *   if (await wallet.isReady()) {
 *     const address = await wallet.getReceiveAddress();
 *     await wallet.send({ to: '0x…', value: '0.01' });
 *   }
 *   ```
 *
 * Mounted into the PWA via `EmbeddedWalletProvider` + `useEmbeddedWallet`
 * from `./react.ts`, but the class works without React for non-UI callers
 * (background tasks, tests, scripts).
 */
export class EmbeddedWallet implements IWallet {
  readonly kind: WalletKind = 'embedded';
  readonly client: PaxeerWallet;
  readonly events: IEventBus;

  private cached: { wallet: PublicWallet; chain: ChainInfo } | null = null;
  private rpcUrlOverride: string | undefined;
  private chainIdOverride: number | undefined;

  constructor(config: PaxeerEmbeddedConfig & { rpcUrl?: string; chainId?: number }, deps: { events?: IEventBus } = {}) {
    this.client = new PaxeerWallet({
      apiUrl: config.apiUrl,
      supabaseUrl: config.supabaseUrl,
      supabaseAnonKey: config.supabaseAnonKey,
      fetch: config.fetch,
    });
    this.rpcUrlOverride = config.rpcUrl;
    this.chainIdOverride = config.chainId;
    this.events = deps.events ?? new SimpleEventBus();

    // Forward Supabase auth state changes onto our domain event bus so the
    // rest of the wallet library (and the PWA UI) can listen using the same
    // `WalletEvents.*` channels they already use for self-custody.
    this.client.onAuthStateChange((event, session) => {
      if (event === 'SIGNED_IN' || event === 'INITIAL_SESSION') {
        if (session) this.events.emit(WalletEvents.SESSION_CREATED);
      } else if (event === 'SIGNED_OUT') {
        this.cached = null;
        this.events.emit(WalletEvents.SESSION_EXPIRED);
        this.events.emit(WalletEvents.WALLET_CLEARED);
      } else if (event === 'TOKEN_REFRESHED') {
        // No-op for the UI; SDK refreshes silently.
      }
    });
  }

  // ── IWallet ─────────────────────────────────────────────────────────

  /** True if a session exists AND a wallet has been provisioned. */
  async isReady(): Promise<boolean> {
    const session = await this.client.getSession();
    if (!session) return false;
    try {
      await this.ensureCached();
      return true;
    } catch {
      return false;
    }
  }

  /** True if a Supabase session is present. (Wallet auto-provisions on first use.) */
  async hasWallet(): Promise<boolean> {
    const session = await this.client.getSession();
    return !!session;
  }

  async getReceiveAddress(): Promise<string> {
    const { wallet } = await this.ensureCached();
    return wallet.address;
  }

  async send(tx: TransactionData): Promise<string> {
    const { wallet } = await this.ensureCached();

    // Translate /paxport/wallet's TransactionData into the SDK's TxRequest.
    // - Native transfer: `value` is decimal-eth, convert to wei string.
    // - ERC-20: SDK doesn't speak token transfer directly; we encode the
    //   `transfer(address,uint256)` call here using viem-style hex assembly.
    if (tx.tokenAddress) {
      const data = encodeErc20Transfer(tx.to, tx.value, tx.decimals ?? 18);
      const r = await this.client.sendTransaction({
        to: tx.tokenAddress as `0x${string}`,
        data,
        value: '0',
        gas: tx.gasLimit ? BigInt(tx.gasLimit) : undefined,
        chainId: this.chainIdOverride,
      });
      return r.tx_hash;
    }

    const r = await this.client.sendTransaction({
      to: tx.to as `0x${string}`,
      value: parseEtherToWei(tx.value),
      gas: tx.gasLimit ? BigInt(tx.gasLimit) : undefined,
      chainId: this.chainIdOverride,
    });
    return r.tx_hash;
  }

  async reset(): Promise<void> {
    this.cached = null;
    await this.client.signOut();
  }

  // ── Auth surface (embedded-specific; PWA onboarding uses these) ─────

  /** Send a magic-link email. The user clicks the link, lands on
   *  `redirectTo`, and the SDK automatically completes the session. */
  async signInWithEmail(
    email: string,
    redirectTo?: string,
  ): Promise<{ ok: boolean; error?: string }> {
    const { error } = await this.client.signInWithEmail(email, redirectTo);
    return error ? { ok: false, error: error.message } : { ok: true };
  }

  /** Begin OAuth sign-in. The browser will redirect to the provider. */
  signInWithOAuth(provider: OAuthProvider, redirectTo?: string): Promise<void> {
    return this.client.signInWithOAuth(provider, redirectTo);
  }

  signOut(): Promise<void> {
    return this.reset();
  }

  // ── Session helpers (mirrors useEmbeddedSession in ./react.ts) ─────

  getSession(): Promise<Session | null> {
    return this.client.getSession();
  }

  getUser(): Promise<User | null> {
    return this.client.getUser();
  }

  /** Subscribe to raw Supabase auth events. Returns an unsubscribe fn. */
  onAuthStateChange(
    cb: (event: AuthChangeEvent, session: Session | null) => void,
  ): () => void {
    return this.client.onAuthStateChange(cb);
  }

  /** EIP-191 personal_sign over an arbitrary message. */
  async signMessage(message: string): Promise<string> {
    await this.ensureCached();
    const r = await this.client.signMessage(message);
    return r.signature;
  }

  // ── Wallet metadata ─────────────────────────────────────────────────

  /** Returns the cached `{ wallet, chain }` record, fetching if needed. */
  async getWalletInfo(): Promise<{ wallet: PublicWallet; chain: ChainInfo }> {
    return this.ensureCached();
  }

  /** RPC URL the PWA should use for read calls (balance / etc). */
  getRpcUrl(): string | null {
    if (this.rpcUrlOverride) return this.rpcUrlOverride;
    return this.cached?.chain.rpc_url ?? null;
  }

  /** Force-refresh the cached wallet record (e.g. after a successful tx). */
  async refresh(): Promise<{ wallet: PublicWallet; chain: ChainInfo }> {
    this.cached = null;
    return this.ensureCached();
  }

  // ── Internal ────────────────────────────────────────────────────────

  private async ensureCached(): Promise<{ wallet: PublicWallet; chain: ChainInfo }> {
    if (this.cached) return this.cached;
    const r = await this.client.getWallet();
    this.cached = r;
    return r;
  }
}

// ── helpers ──────────────────────────────────────────────────────────

/**
 * Convert a decimal-eth string (`"0.01"`) to a wei string (`"10000000000000000"`).
 * We avoid pulling in `ethers` as a hard dep here because the SDK is meant
 * to be lightweight; the self-custody wallet core owns its ethers integration.
 */
function parseEtherToWei(value: string): string {
  const trimmed = value.trim();
  if (!trimmed) return '0';
  const negative = trimmed.startsWith('-');
  const abs = negative ? trimmed.slice(1) : trimmed;
  const [whole, frac = ''] = abs.split('.');
  if (!/^\d*$/.test(whole) || !/^\d*$/.test(frac)) {
    throw new Error(`EmbeddedWallet.send: invalid decimal value "${value}"`);
  }
  const fracPadded = (frac + '0'.repeat(18)).slice(0, 18);
  // BigInt strips leading zeros which is what we want.
  const wei = BigInt(whole || '0') * 10n ** 18n + BigInt(fracPadded || '0');
  return (negative ? -wei : wei).toString();
}

/**
 * ABI-encode `transfer(address,uint256)`. Selector + 32-byte address +
 * 32-byte value. We hand-roll this so the embedded module stays
 * dependency-light; the API server validates the calldata anyway.
 */
function encodeErc20Transfer(to: string, value: string, decimals: number): `0x${string}` {
  const selector = '0xa9059cbb'; // keccak256("transfer(address,uint256)") -> first 4 bytes
  const cleanTo = to.toLowerCase().replace(/^0x/, '');
  if (cleanTo.length !== 40 || !/^[0-9a-f]+$/.test(cleanTo)) {
    throw new Error(`EmbeddedWallet.send: invalid recipient address "${to}"`);
  }
  const paddedAddress = cleanTo.padStart(64, '0');
  const amount = parseUnits(value, decimals);
  const paddedAmount = amount.toString(16).padStart(64, '0');
  return `${selector}${paddedAddress}${paddedAmount}` as `0x${string}`;
}

function parseUnits(value: string, decimals: number): bigint {
  const trimmed = value.trim();
  const negative = trimmed.startsWith('-');
  const abs = negative ? trimmed.slice(1) : trimmed;
  const [whole, frac = ''] = abs.split('.');
  if (!/^\d*$/.test(whole) || !/^\d*$/.test(frac)) {
    throw new Error(`EmbeddedWallet.send: invalid decimal value "${value}"`);
  }
  const fracPadded = (frac + '0'.repeat(decimals)).slice(0, decimals);
  const out = BigInt(whole || '0') * 10n ** BigInt(decimals) + BigInt(fracPadded || '0');
  return negative ? -out : out;
}
