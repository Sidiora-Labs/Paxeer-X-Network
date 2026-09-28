import {
  createClient,
  type AuthChangeEvent,
  type Session,
  type SupabaseClient,
  type User,
} from '@supabase/supabase-js';
import {
  PaxeerWalletError,
  type ChainInfo,
  type FundedProvisionResponse,
  type FundedSelfResponse,
  type FundedTier,
    type PaxeerEmbeddedConfig,
  type PublicWallet,
  type SendTxResponse,
  type SignMessageResponse,
  type SignTxResponse,
  type TxRequest,
} from './types';

export * from './types';

/**
 * `PaxeerWallet` — drop-in client for any Paxeer-network app.
 *
 * Lifecycle:
 *   1. `signInWith{Email,OAuth}()` runs the Supabase auth flow
 *   2. After the user returns with a valid session, the SDK auto-provisions a
 *      wallet on first call to `getWallet()` / `sendTransaction()` if needed
 *   3. All signing happens on the API server; the SDK never sees key material
 */
export class PaxeerWallet {
  readonly supabase: SupabaseClient;
  private readonly apiUrl: string;
  private readonly fetchImpl: typeof fetch;

    constructor(config: PaxeerEmbeddedConfig) {
    if (!config.apiUrl) throw new Error('PaxeerWallet: apiUrl required');
    if (!config.supabaseUrl) throw new Error('PaxeerWallet: supabaseUrl required');
    if (!config.supabaseAnonKey) throw new Error('PaxeerWallet: supabaseAnonKey required');

    this.apiUrl = config.apiUrl.replace(/\/$/, '');
    this.fetchImpl = config.fetch ?? globalThis.fetch.bind(globalThis);
    this.supabase = createClient(config.supabaseUrl, config.supabaseAnonKey, {
      auth: {
        persistSession: true,
        autoRefreshToken: true,
        detectSessionInUrl: true,
      },
    });
  }

  // ---------------------------------------------------------------------
  // Auth
  // ---------------------------------------------------------------------

  /** Send a magic-link email to start passwordless sign-in. */
  signInWithEmail(email: string, redirectTo?: string): Promise<{ error: Error | null }> {
    return this.supabase.auth
      .signInWithOtp({
        email,
        options: redirectTo ? { emailRedirectTo: redirectTo } : undefined,
      })
      .then((r) => ({ error: r.error }));
  }

  /** Begin OAuth sign-in. Browser will redirect to the provider. */
  async signInWithOAuth(
    provider: 'google' | 'apple' | 'twitter' | 'github' | 'discord',
    redirectTo?: string,
  ): Promise<void> {
    const { error } = await this.supabase.auth.signInWithOAuth({
      provider,
      options: redirectTo ? { redirectTo } : undefined,
    });
    if (error) throw new PaxeerWalletError(error.message, 'OAUTH_FAILED');
  }

  signOut(): Promise<void> {
    return this.supabase.auth.signOut().then(() => undefined);
  }

  async getSession(): Promise<Session | null> {
    const { data } = await this.supabase.auth.getSession();
    return data.session ?? null;
  }

  async getUser(): Promise<User | null> {
    const { data } = await this.supabase.auth.getUser();
    return data.user ?? null;
  }

  /**
   * Subscribe to auth state changes. Returns an unsubscribe function.
   * Forward this to whatever framework reactivity layer you use.
   */
  onAuthStateChange(cb: (event: AuthChangeEvent, session: Session | null) => void): () => void {
    const { data } = this.supabase.auth.onAuthStateChange(cb);
    return () => data.subscription.unsubscribe();
  }

  // ---------------------------------------------------------------------
  // Wallet
  // ---------------------------------------------------------------------

  /**
   * Get the current user's wallet. Auto-provisions on first call.
   * Throws if the user is not signed in.
   *
   * NOTE: this composes the two primitives below. Prefer calling
   * `getStandardSelf()` + `provisionStandardWallet()` directly when you want
   * the user to make an explicit choice (e.g. picking between a standard and
   * a funded account at sign-in time).
   */
  async getWallet(): Promise<{ wallet: PublicWallet; chain: ChainInfo }> {
    const me = await this.getStandardSelf();
    if (me) return me;

    // No wallet yet — provision then fetch.
    await this.provisionStandardWallet();
    const after = await this.getStandardSelf();
    if (!after) throw new PaxeerWalletError('provision_lost', 'PROVISION_LOST', 500);
    return after;
  }

  /**
   * Read the current user's standard wallet without auto-provisioning.
   * Returns `null` if the user is signed in but hasn't created a wallet yet.
   *
   * Use this when the calling UI must NOT silently create a wallet — e.g.
   * a chooser screen that lets the user explicitly pick "create a standard
   * wallet" vs "create a funded account".
   */
  async getStandardSelf(): Promise<{ wallet: PublicWallet; chain: ChainInfo } | null> {
    try {
      return await this.callJson<{ wallet: PublicWallet; chain: ChainInfo }>(
        'GET',
        '/v1/wallet/me',
      );
    } catch (err) {
      if (err instanceof PaxeerWalletError && err.status === 404) return null;
      throw err;
    }
  }

  /**
   * Explicitly provision a standard self-custody wallet for the authenticated
   * user. Idempotent on the server — calling twice returns the same wallet.
   */
  provisionStandardWallet(): Promise<{ wallet: PublicWallet }> {
    return this.callJson<{ wallet: PublicWallet }>('POST', '/v1/wallet/provision');
  }

  /** Sign a transaction without broadcasting. Returns serialized signed tx hex. */
  signTransaction(tx: TxRequest): Promise<SignTxResponse> {
    return this.callJson<SignTxResponse>('POST', '/v1/wallet/sign', { tx: serialize(tx) });
  }

  /** Sign + broadcast in one round-trip. Returns the tx hash. */
  sendTransaction(tx: TxRequest): Promise<SendTxResponse> {
    return this.callJson<SendTxResponse>('POST', '/v1/wallet/send', { tx: serialize(tx) });
  }

  /** EIP-191 personal_sign over a message string. */
  signMessage(message: string): Promise<SignMessageResponse> {
    return this.callJson<SignMessageResponse>('POST', '/v1/wallet/sign-message', { message });
  }

  // ---------------------------------------------------------------------
  // Funded accounts (prop-firm tier)
  //
  // Parallel surface to the standard wallet methods above. Same auth model;
  // every signing call is gated by the funded policy engine on the server
  // (see HANDOFF.md §4). Denials surface as `PaxeerWalletError` with a stable
  // `code` (e.g. WITHDRAWAL_BLOCKED, CONTRACT_NOT_WHITELISTED) and the full
  // structured payload accessible via `error.detail`.
  // ---------------------------------------------------------------------

  /**
   * Public — list every active funded tier and its (contract, selector)
   * whitelist. Used by the UI to render the "Become a Funded Trader" panel
   * before the user has signed in. No auth needed.
   */
  listFundedTiers(): Promise<{ tiers: FundedTier[] }> {
    return this.callJson<{ tiers: FundedTier[] }>(
      'GET',
      '/v1/funded/tiers',
      undefined,
      { auth: false },
    );
  }

  /**
   * Get the current user's funded account state — live balances, status,
   * peak/daily-start equity, and the tier whitelist. Returns `null` if the
   * user has not yet provisioned a funded account.
   */
  async getFundedSelf(): Promise<FundedSelfResponse | null> {
    try {
      return await this.callJson<FundedSelfResponse>('GET', '/v1/funded/me');
    } catch (err) {
      if (err instanceof PaxeerWalletError && err.status === 404) return null;
      throw err;
    }
  }

  /**
   * Provision a funded account in the given tier (default: `starter_25k`).
   * Server flow: encrypt fresh EOA → insert funded_account row → treasury
   * disburses USDL + PAX → mark active. Idempotent: if the user already has
   * an account in this tier we return the existing one without re-funding.
   *
   * Holds the request open while the two on-chain transfers settle (typically
   * ~4-6s on chain 125), so the UI can show the funded balance immediately.
   */
  provisionFundedAccount(tier_id = 'starter_25k'): Promise<FundedProvisionResponse> {
    return this.callJson<FundedProvisionResponse>('POST', '/v1/funded/provision', { tier_id });
  }

  /** Sign a transaction with the funded wallet. Runs through the funded policy first. */
  signFundedTransaction(tx: TxRequest): Promise<SignTxResponse> {
    return this.callJson<SignTxResponse>('POST', '/v1/funded/sign', { tx: serialize(tx) });
  }

  /** Sign + broadcast through the funded wallet. Runs through the funded policy first. */
  sendFundedTransaction(tx: TxRequest): Promise<SendTxResponse> {
    return this.callJson<SendTxResponse>('POST', '/v1/funded/send', { tx: serialize(tx) });
  }

  /** EIP-191 personal_sign with the funded wallet (status check only — no whitelist gate). */
  signFundedMessage(message: string): Promise<SignMessageResponse> {
    return this.callJson<SignMessageResponse>('POST', '/v1/funded/sign-message', { message });
  }

  // ---------------------------------------------------------------------
  // Internal — auth-bearing fetch
  //
  // `auth: false` skips the bearer header so public endpoints (currently just
  // `/v1/funded/tiers`) work without an active session.
  // ---------------------------------------------------------------------

  private async callJson<T>(
    method: 'GET' | 'POST',
    path: string,
    body?: unknown,
    options: { auth?: boolean } = {},
  ): Promise<T> {
    const headers: Record<string, string> = {};
    if (options.auth !== false) {
      const session = await this.getSession();
      if (!session?.access_token) {
        throw new PaxeerWalletError('not_authenticated', 'NO_SESSION', 401);
      }
      headers.Authorization = `Bearer ${session.access_token}`;
    }
    if (body !== undefined) {
      headers['Content-Type'] = 'application/json';
    }

    const res = await this.fetchImpl(`${this.apiUrl}${path}`, {
      method,
      headers,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });

    let payload: unknown;
    try {
      payload = await res.json();
    } catch {
      payload = null;
    }

    if (!res.ok) {
      const code =
        typeof payload === 'object' && payload && 'error' in payload
          ? String((payload as { error: unknown }).error)
          : `HTTP_${res.status}`;
      const message =
        typeof payload === 'object' && payload && 'message' in payload
          ? String((payload as { message: unknown }).message)
          : `request failed: ${res.status}`;
      throw new PaxeerWalletError(message, code, res.status, payload);
    }
    return payload as T;
  }
}

function serialize(tx: TxRequest): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  if (tx.to !== undefined) out.to = tx.to;
  if (tx.value !== undefined) out.value = bigintToString(tx.value);
  if (tx.data !== undefined) out.data = tx.data;
  if (tx.gas !== undefined) out.gas = bigintToString(tx.gas);
  if (tx.maxFeePerGas !== undefined) out.maxFeePerGas = bigintToString(tx.maxFeePerGas);
  if (tx.maxPriorityFeePerGas !== undefined)
    out.maxPriorityFeePerGas = bigintToString(tx.maxPriorityFeePerGas);
  if (tx.nonce !== undefined) out.nonce = tx.nonce;
  if (tx.chainId !== undefined) out.chainId = tx.chainId;
  return out;
}

function bigintToString(v: string | bigint): string {
  return typeof v === 'bigint' ? v.toString() : v;
}
