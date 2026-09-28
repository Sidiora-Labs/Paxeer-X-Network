import type { AuthChangeEvent, Session, User } from '@supabase/supabase-js';
import type { IWallet, WalletKind } from '../ports/IWallet';
import type { IEventBus } from '../ports/IEventBus';
import type { TransactionData } from '../types';
import { WalletEvents } from '../types';
import { SimpleEventBus } from '../adapters/SimpleEventBus';
import { PaxeerWallet } from './sdk';
import type {
  ChainInfo,
  FundedSelfResponse,
  FundedTier,
  FundedProvisionResponse,
  FundedWhitelistEntry,
  OAuthProvider,
  PaxeerEmbeddedConfig,
  PublicWallet,
} from './sdk/types';

/**
 * High-level facade for the **Funded Account** custody model.
 *
 * Sibling to `EmbeddedWallet` — both wrap the same `PaxeerWalletClient`,
 * share the same Supabase auth surface, and implement `IWallet` so the
 * PWA's `WalletProvider` can drive either polymorphically. The two differ
 * in *which* server endpoints they hit:
 *
 *   - `EmbeddedWallet` → `/v1/wallet/{send,sign,sign-message}` (standard).
 *   - `FundedWallet`   → `/v1/funded/{send,sign,sign-message}` (policy-gated).
 *
 * Custody model:
 *   - User authenticates with Supabase exactly like the embedded flow.
 *   - The funded API auto-provisions a separate EVM EOA on the user's
 *     first call to `provisionFunded()`, disburses the tier's initial
 *     USDL + PAX from the treasury, and locks the wallet behind a
 *     drawdown-aware policy engine.
 *   - **Every signing call** is gated server-side:
 *       - Withdrawals → blocked (`WITHDRAWAL_BLOCKED`).
 *       - Tx target must be in the tier whitelist (`CONTRACT_NOT_WHITELISTED`).
 *       - `approve(spender, …)` spender must be whitelisted.
 *       - Native value gated per-entry (`NATIVE_VALUE_NOT_ALLOWED`).
 *       - Drawdown / breach caps enforced live.
 *   - Denials surface as `PaxeerWalletError` with a stable `code`. The
 *     PWA renders a friendly deny modal off `error.detail`.
 *
 * UI consequences in the PWA (enforced in the action layer + UI gates):
 *   - **Send + Receive + Ramp screens are hidden** in funded mode.
 *   - Swap output tokens are filtered to the whitelist.
 *   - DApp tiles in Discover are filtered to whitelisted contracts.
 *
 * Lifecycle:
 *
 *   ```ts
 *   const wallet = new FundedWallet({ apiUrl, supabaseUrl, supabaseAnonKey });
 *
 *   // Onboarding — same auth as embedded
 *   await wallet.signInWithOAuth('google', `${origin}/auth/callback`);
 *
 *   // After session lands the user picks a tier
 *   const tiers = await wallet.listTiers();
 *   await wallet.provisionFunded(tiers[0].tier_id);
 *
 *   // Trading is gated by the tier whitelist
 *   const address = await wallet.getReceiveAddress();   // internal use only
 *   await wallet.send({ to: '0xWHITELISTED…', value: '0' });
 *   ```
 *
 * Mounted into the PWA via `EmbeddedWalletProvider` (extended to also
 * expose funded state) — see `./react.ts`.
 */
export class FundedWallet implements IWallet {
  readonly kind: WalletKind = 'funded';
  readonly client: PaxeerWallet;
  readonly events: IEventBus;

  private cachedSelf: FundedSelfResponse | null = null;
  private cachedChain: ChainInfo | null = null;
  private rpcUrlOverride: string | undefined;
  private chainIdOverride: number | undefined;

  constructor(
    config: PaxeerEmbeddedConfig & { rpcUrl?: string; chainId?: number },
    deps: { events?: IEventBus; client?: PaxeerWallet } = {},
  ) {
    // Reuse an existing client when supplied (so funded + embedded share the
    // same Supabase session). Otherwise build one — but this path is mostly
    // for non-React callers; the PWA always passes its EmbeddedWallet.client.
    this.client =
      deps.client ??
      new PaxeerWallet({
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
    // `WalletEvents.*` channels they already use for self-custody / embedded.
    this.client.onAuthStateChange((event, session) => {
      if (event === 'SIGNED_IN' || event === 'INITIAL_SESSION') {
        if (session) this.events.emit(WalletEvents.SESSION_CREATED);
      } else if (event === 'SIGNED_OUT') {
        this.cachedSelf = null;
        this.cachedChain = null;
        this.events.emit(WalletEvents.SESSION_EXPIRED);
        this.events.emit(WalletEvents.WALLET_CLEARED);
      }
    });
  }

  // ── IWallet ─────────────────────────────────────────────────────────

  /** True if a session exists AND a funded account has been provisioned. */
  async isReady(): Promise<boolean> {
    const session = await this.client.getSession();
    if (!session) return false;
    try {
      const self = await this.client.getFundedSelf();
      return !!self;
    } catch {
      return false;
    }
  }

  /** True if a Supabase session is present. (Funded account requires explicit provision.) */
  async hasWallet(): Promise<boolean> {
    const session = await this.client.getSession();
    return !!session;
  }

  /**
   * Funded wallet address. Useful for support / debugging / settings —
   * the PWA hides the Receive UI in funded mode, so this is NOT exposed
   * via a "Receive" button. Throws if the user has no funded account.
   */
  async getReceiveAddress(): Promise<string> {
    const self = await this.ensureSelf();
    return self.wallet.address;
  }

  /**
   * Send a transaction through the funded policy engine. The server
   * validates the (contract, selector) pair against the tier whitelist
   * before signing. Denials throw `PaxeerWalletError` with `code` set to
   * one of the `FundedDenyCode` values.
   */
  async send(tx: TransactionData): Promise<string> {
    if (tx.tokenAddress) {
      const data = encodeErc20Transfer(tx.to, tx.value, tx.decimals ?? 18);
      const r = await this.client.sendFundedTransaction({
        to: tx.tokenAddress as `0x${string}`,
        data,
        value: '0',
        gas: tx.gasLimit ? BigInt(tx.gasLimit) : undefined,
        chainId: this.chainIdOverride,
      });
      return r.tx_hash;
    }

    const r = await this.client.sendFundedTransaction({
      to: tx.to as `0x${string}`,
      value: parseEtherToWei(tx.value),
      gas: tx.gasLimit ? BigInt(tx.gasLimit) : undefined,
      chainId: this.chainIdOverride,
    });
    return r.tx_hash;
  }

  /**
   * Sign the user out of Supabase. The funded account row stays on the
   * server — the user can sign back in to resume.
   */
  async reset(): Promise<void> {
    this.cachedSelf = null;
    this.cachedChain = null;
    await this.client.signOut();
  }

  // ── Auth surface (shared with embedded; mirrored here so callers
  //    can drive FundedWallet without holding an EmbeddedWallet too) ───

  async signInWithEmail(
    email: string,
    redirectTo?: string,
  ): Promise<{ ok: boolean; error?: string }> {
    const { error } = await this.client.signInWithEmail(email, redirectTo);
    return error ? { ok: false, error: error.message } : { ok: true };
  }

  signInWithOAuth(provider: OAuthProvider, redirectTo?: string): Promise<void> {
    return this.client.signInWithOAuth(provider, redirectTo);
  }

  signOut(): Promise<void> {
    return this.reset();
  }

  getSession(): Promise<Session | null> {
    return this.client.getSession();
  }

  getUser(): Promise<User | null> {
    return this.client.getUser();
  }

  onAuthStateChange(
    cb: (event: AuthChangeEvent, session: Session | null) => void,
  ): () => void {
    return this.client.onAuthStateChange(cb);
  }

  /** EIP-191 personal_sign — funded version skips the whitelist gate (sig only). */
  async signMessage(message: string): Promise<string> {
    await this.ensureSelf();
    const r = await this.client.signFundedMessage(message);
    return r.signature;
  }

  // ── Funded-specific surface ─────────────────────────────────────────

  /** Public list of active tiers + whitelists. No auth required. */
  async listTiers(): Promise<FundedTier[]> {
    const { tiers } = await this.client.listFundedTiers();
    return tiers;
  }

  /**
   * Read the current funded account state. Returns `null` if the user is
   * signed in but has not provisioned an account yet — the UI should
   * route them to the tier picker in that case.
   */
  async getFundedSelf(): Promise<FundedSelfResponse | null> {
    const self = await this.client.getFundedSelf();
    if (self) this.cachedSelf = self;
    return self;
  }

  /**
   * Provision a funded account in the given tier. Holds the request open
   * for ~4-6s while the treasury disbursement transactions settle, so
   * the UI can show the funded balance immediately on success.
   *
   * Idempotent: if the user already has an account in the tier, returns
   * the existing record without re-funding.
   */
  async provisionFunded(tier_id?: string): Promise<FundedProvisionResponse> {
    const r = await this.client.provisionFundedAccount(tier_id);
    // Invalidate the cache so the next `getFundedSelf` re-fetches the
    // freshly funded balances.
    this.cachedSelf = null;
    return r;
  }

  /** Whitelist for the current tier. Throws if no funded account yet. */
  async getWhitelist(): Promise<FundedWhitelistEntry[]> {
    const self = await this.ensureSelf();
    return self.whitelist;
  }

  /** RPC URL for read calls (balances, allowances, quotes). */
  getRpcUrl(): string | null {
    if (this.rpcUrlOverride) return this.rpcUrlOverride;
    return this.cachedChain?.rpc_url ?? null;
  }

  /** Force a re-fetch of the funded record (e.g. after a successful tx). */
  async refresh(): Promise<FundedSelfResponse | null> {
    this.cachedSelf = null;
    return this.getFundedSelf();
  }

  // ── Internal ────────────────────────────────────────────────────────

  private async ensureSelf(): Promise<FundedSelfResponse> {
    if (this.cachedSelf) return this.cachedSelf;
    const self = await this.client.getFundedSelf();
    if (!self) {
      throw new Error(
        'FundedWallet: no funded account provisioned for this user. ' +
          'Call provisionFunded(tier_id) first.',
      );
    }
    this.cachedSelf = self;
    return self;
  }
}

// ── helpers (duplicated from EmbeddedWallet to keep the funded module
//    dependency-light and decoupled — same encoding rules either way) ──

function parseEtherToWei(value: string): string {
  const trimmed = value.trim();
  if (!trimmed) return '0';
  const negative = trimmed.startsWith('-');
  const abs = negative ? trimmed.slice(1) : trimmed;
  const [whole, frac = ''] = abs.split('.');
  if (!/^\d*$/.test(whole) || !/^\d*$/.test(frac)) {
    throw new Error(`FundedWallet.send: invalid decimal value "${value}"`);
  }
  const fracPadded = (frac + '0'.repeat(18)).slice(0, 18);
  const wei = BigInt(whole || '0') * 10n ** 18n + BigInt(fracPadded || '0');
  return (negative ? -wei : wei).toString();
}

function encodeErc20Transfer(to: string, value: string, decimals: number): `0x${string}` {
  const selector = '0xa9059cbb';
  const cleanTo = to.toLowerCase().replace(/^0x/, '');
  if (cleanTo.length !== 40 || !/^[0-9a-f]+$/.test(cleanTo)) {
    throw new Error(`FundedWallet.send: invalid recipient address "${to}"`);
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
    throw new Error(`FundedWallet.send: invalid decimal value "${value}"`);
  }
  const fracPadded = (frac + '0'.repeat(decimals)).slice(0, decimals);
  const out = BigInt(whole || '0') * 10n ** BigInt(decimals) + BigInt(fracPadded || '0');
  return negative ? -out : out;
}
