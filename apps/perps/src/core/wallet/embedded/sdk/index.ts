/**
 * Vendored from `@paxeer/wallet` v0.1.0. See ./types.ts for vendoring policy.
 *
 * `PaxeerWallet` — drop-in client for any Paxeer-network app.
 *
 * Lifecycle:
 *   1. `signInWith{Email,OAuth}()` runs the Supabase auth flow.
 *   2. After the user returns with a valid session, the SDK auto-provisions a
 *      wallet on first call to `getWallet()` / `sendTransaction()` if needed.
 *   3. All signing happens on the API server; the SDK never sees key material.
 */

import {
  createClient,
  type AuthChangeEvent,
  type Session,
  type SupabaseClient,
  type User,
} from "@supabase/supabase-js";
import {
  PaxeerWalletError,
  type ChainInfo,
  type PaxeerWalletConfig,
  type PublicWallet,
  type SendTxResponse,
  type SignMessageResponse,
  type SignTxResponse,
  type TxRequest,
} from "./types";

export * from "./types";

export type OAuthProvider = "google" | "apple" | "twitter" | "github" | "discord";

export class PaxeerWallet {
  readonly supabase: SupabaseClient;
  private readonly apiUrl: string;
  private readonly fetchImpl: typeof fetch;

  constructor(config: PaxeerWalletConfig) {
    if (!config.apiUrl) throw new Error("PaxeerWallet: apiUrl required");
    if (!config.supabaseUrl) throw new Error("PaxeerWallet: supabaseUrl required");
    if (!config.supabaseAnonKey) throw new Error("PaxeerWallet: supabaseAnonKey required");

    this.apiUrl = config.apiUrl.replace(/\/$/, "");
    this.fetchImpl = config.fetch ?? globalThis.fetch.bind(globalThis);
    this.supabase = createClient(config.supabaseUrl, config.supabaseAnonKey, {
      auth: {
        persistSession: true,
        autoRefreshToken: true,
        detectSessionInUrl: false,
        flowType: "pkce",
      },
    });
  }

  // ───────────────────────────────────────────────────────────── Auth ─────

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
  async signInWithOAuth(provider: OAuthProvider, redirectTo?: string): Promise<void> {
    const { error } = await this.supabase.auth.signInWithOAuth({
      provider,
      options: redirectTo ? { redirectTo } : undefined,
    });
    if (error) throw new PaxeerWalletError(error.message, "OAUTH_FAILED");
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
   */
  onAuthStateChange(cb: (event: AuthChangeEvent, session: Session | null) => void): () => void {
    const { data } = this.supabase.auth.onAuthStateChange(cb);
    return () => data.subscription.unsubscribe();
  }

  // ─────────────────────────────────────────────────────────── Wallet ─────

  /**
   * Get the current user's wallet. Auto-provisions on first call.
   * Throws if the user is not signed in.
   */
  async getWallet(): Promise<{ wallet: PublicWallet; chain: ChainInfo }> {
    const me = await this.callJson<{ wallet: PublicWallet; chain: ChainInfo }>(
      "GET",
      "/v1/wallet/me",
    ).catch((err: PaxeerWalletError) => (err.status === 404 ? null : Promise.reject(err)));
    if (me) return me;

    // No wallet yet — provision then fetch.
    await this.callJson<{ wallet: PublicWallet }>("POST", "/v1/wallet/provision");
    return this.callJson<{ wallet: PublicWallet; chain: ChainInfo }>("GET", "/v1/wallet/me");
  }

  /** Sign a transaction without broadcasting. Returns serialized signed tx hex. */
  signTransaction(tx: TxRequest): Promise<SignTxResponse> {
    return this.callJson<SignTxResponse>("POST", "/v1/wallet/sign", { tx: serialize(tx) });
  }

  /** Sign + broadcast in one round-trip. Returns the tx hash. */
  sendTransaction(tx: TxRequest): Promise<SendTxResponse> {
    return this.callJson<SendTxResponse>("POST", "/v1/wallet/send", { tx: serialize(tx) });
  }

  /** EIP-191 personal_sign over a message string. */
  signMessage(message: string): Promise<SignMessageResponse> {
    return this.callJson<SignMessageResponse>("POST", "/v1/wallet/sign-message", { message });
  }

  // ─────────────────────────────────────── Internal — auth-bearing fetch ─

  private async callJson<T>(method: "GET" | "POST", path: string, body?: unknown): Promise<T> {
    const session = await this.getSession();
    if (!session?.access_token) {
      throw new PaxeerWalletError("not_authenticated", "NO_SESSION", 401);
    }
    const res = await this.fetchImpl(`${this.apiUrl}${path}`, {
      method,
      headers: {
        Authorization: `Bearer ${session.access_token}`,
        ...(body !== undefined ? { "Content-Type": "application/json" } : {}),
      },
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
        typeof payload === "object" && payload && "error" in payload
          ? String((payload as { error: unknown }).error)
          : `HTTP_${res.status}`;
      const message =
        typeof payload === "object" && payload && "message" in payload
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
  return typeof v === "bigint" ? v.toString() : v;
}
