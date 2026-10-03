import {
  createClient,
  type AuthChangeEvent,
  type Session,
  type SupabaseClient,
  type User,
} from '@supabase/supabase-js';
import { ed25519 } from '@noble/curves/ed25519.js';
import { sha256 } from '@noble/hashes/sha2.js';
import {
  PaxeerWalletError,
  type ChainInfo,
  type FundedProvisionResponse,
  type FundedSelfResponse,
  type FundedTier,
  type PaxeerWalletConfig,
  type PublicWallet,
  type SendTxResponse,
  type SignMessageResponse,
  type SignTxResponse,
  type TxRequest,
} from './types.js';

export * from './types.js';
export * from './rpc.js';
export * from './endpoint.js';
export * from './kernel.js';
export * from './human.js';
export * from './ladder.js';
export * from './modules/index.js';
export * from './errors.js';
export * from './provider.js';
export * from './eip6963.js';
export * from './wallet.js';
export * from './agent.js';

export interface ActivityDisclosureWire {
  readonly account: string;
  readonly module: string;
  readonly operation: number;
  readonly amounts: readonly { readonly asset: string; readonly amount: string }[];
  readonly destinations: readonly string[];
  readonly sequence: string;
  readonly not_before: string;
  readonly not_after: string;
}

export interface ActivityApprovalWire {
  readonly version: 1;
  readonly principal: string;
  readonly key_id: string;
  readonly network_id: number;
  readonly protocol_version: number;
  readonly session_id: string;
  readonly activity_digest: string;
  readonly expires_at: string;
}

export interface LxActivityReview {
  readonly id: string;
  readonly kind: 'lx_activity' | 'lx_send_authorization';
  readonly state: 'reviewed';
  readonly activity: string;
  readonly signing_preimage: string;
  readonly public_key: string;
  readonly key_id: string;
  readonly principal: string;
  readonly network_id: number;
  readonly protocol_version: number;
  readonly session_id: string;
  readonly disclosure: ActivityDisclosureWire;
  readonly expires_at: string;
  readonly did: string;
  readonly account_id: string;
  readonly epoch: number;
  readonly participants: readonly string[];
}

export type LxActivityApproved = Omit<LxActivityReview, 'state'> & {
  readonly state: 'approved';
  readonly approval: ActivityApprovalWire;
};
export type LxActivitySigningUnknown = Omit<LxActivityApproved, 'state'> & {
  readonly state: 'signing_unknown';
};
export type LxActivitySigned = Omit<LxActivityApproved, 'state'> & {
  readonly state: 'signed';
  readonly signature: string;
  readonly attestor_audit: readonly {
    readonly node_id: string;
    readonly audit_sequence: number;
  }[];
};
export type LxActivityArtifact =
  | LxActivityReview | LxActivityApproved | LxActivitySigningUnknown | LxActivitySigned;

export interface LxActivityStorage {
  getItem(key: string): string | null | Promise<string | null>;
  setItem(key: string, value: string): void | Promise<void>;
}

export interface LxActivityOptions {
  readonly storage: LxActivityStorage;
  readonly confirm: (review: LxActivityReview) => boolean | Promise<boolean>;
}

export type LxActivityWalletConfig = PaxeerWalletConfig & {
  readonly lxActivity?: LxActivityOptions;
};

interface LxActivityRecord {
  version: 1;
  review: LxActivityReview;
  confirmed: boolean;
  phase: 'reviewed' | 'approval_unknown' | 'approved' | 'signing_unknown' | 'signed';
  artifact: LxActivityArtifact;
}

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
  private readonly lxActivity?: LxActivityOptions;
  private readonly lxLocks = new Map<string, Promise<unknown>>();

  constructor(config: LxActivityWalletConfig) {
    if (!config.apiUrl) throw new Error('PaxeerWallet: apiUrl required');
    if (!config.supabaseUrl) throw new Error('PaxeerWallet: supabaseUrl required');
    if (!config.supabaseAnonKey) throw new Error('PaxeerWallet: supabaseAnonKey required');

    this.apiUrl = config.apiUrl.replace(/\/$/, '');
    this.fetchImpl = config.fetch ?? globalThis.fetch.bind(globalThis);
    if (config.lxActivity && (
      typeof config.lxActivity.confirm !== 'function' ||
      typeof config.lxActivity.storage?.getItem !== 'function' ||
      typeof config.lxActivity.storage?.setItem !== 'function'
    )) throw lxError('LX_ACTIVITY_CONFIGURATION', 'Explicit confirmation and durable storage are required');
    this.lxActivity = config.lxActivity
      ? { storage: config.lxActivity.storage, confirm: config.lxActivity.confirm }
      : undefined;
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

  async reviewLxActivity(activity: string): Promise<LxActivityReview> {
    return this.lxReview(activity, 'lx_activity');
  }

  async reviewLxSendAuthorization(activity: string): Promise<LxActivityReview> {
    return this.lxReview(activity, 'lx_send_authorization');
  }

  private async lxReview(activity: string, kind: LxActivityReview['kind']): Promise<LxActivityReview> {
    this.lxOptions();
    decodeLxActivity(activity, kind);
    const principal = await this.lxPrincipal();
    const artifact = validateLxArtifact(await this.lxRequest(
      principal, 'POST', '/v1/wallet/lx/review', { activity, kind },
    ), principal);
    if (artifact.state !== 'reviewed' || artifact.activity !== activity || artifact.kind !== kind) {
      throw lxError('LX_REVIEW_MISMATCH', 'The gateway did not return the requested original review');
    }
    requireLxUnexpired(artifact);
    return this.lxExclusive(artifact.id, async () => {
      const prior = await this.lxLoad(principal, artifact.id);
      if (prior) {
        sameLxReview(prior.review, artifact);
        return freezeLx(prior.review);
      }
      await this.lxSave(principal, {
        version: 1, review: artifact, confirmed: false, phase: 'reviewed', artifact,
      });
      return freezeLx(artifact);
    });
  }

  async approveLxActivity(reviewId: string): Promise<LxActivityApproved> {
    requireLxUUID(reviewId);
    return this.lxExclusive(reviewId, async () => {
      const principal = await this.lxPrincipal();
      let record = await this.lxRequiredRecord(principal, reviewId);
      if (record.phase === 'approval_unknown') {
        record = await this.lxRefresh(principal, record);
      }
      if (record.artifact.state !== 'reviewed') {
        if (!record.confirmed) throw lxError('LX_CONFIRMATION_REQUIRED', 'No retained explicit confirmation');
        if (record.artifact.state !== 'approved') {
          throw lxError('LX_SIGNING_ALREADY_REQUESTED', 'Read the existing approval status');
        }
        return freezeLx(record.artifact);
      }
      requireLxUnexpired(record.review);
      if (await this.lxOptions().confirm(freezeLx(record.review)) !== true) {
        throw lxError('LX_APPROVAL_DECLINED', 'The activity was not approved');
      }
      await this.lxAssertPrincipal(principal);
      requireLxUnexpired(record.review);
      record = { ...record, confirmed: true, phase: 'approval_unknown' };
      await this.lxSave(principal, record);
      const review = record.review;
      const artifact = validateLxArtifact(await this.lxRequest(
        principal, 'POST', '/v1/wallet/lx/approve', {
          review_id: review.id, activity: review.activity,
          signing_preimage: review.signing_preimage, disclosure: review.disclosure,
          expires_at: review.expires_at, decision: 'approve',
        },
      ), principal);
      sameLxReview(review, artifact);
      if (artifact.state !== 'approved') {
        throw lxError('LX_APPROVAL_MISMATCH', 'The gateway did not retain the exact approval');
      }
      await this.lxSave(principal, { ...record, phase: 'approved', artifact });
      return freezeLx(artifact);
    });
  }

  async signApprovedLxActivity(approvalId: string): Promise<LxActivityArtifact> {
    requireLxUUID(approvalId);
    return this.lxExclusive(approvalId, async () => {
      const principal = await this.lxPrincipal();
      let record = await this.lxRequiredRecord(principal, approvalId);
      if (!record.confirmed) throw lxError('LX_CONFIRMATION_REQUIRED', 'No retained explicit confirmation');
      if (record.phase === 'approval_unknown') record = await this.lxRefresh(principal, record);
      if (record.phase === 'signing_unknown' || record.phase === 'signed') {
        return freezeLx((await this.lxRefresh(principal, record)).artifact);
      }
      if (record.phase !== 'approved' || record.artifact.state !== 'approved') {
        throw lxError('LX_APPROVAL_REQUIRED', 'Signing requires a retained gateway approval');
      }
      requireLxUnexpired(record.review);
      record = { ...record, phase: 'signing_unknown' };
      await this.lxSave(principal, record);
      const artifact = validateLxArtifact(await this.lxRequest(
        principal, 'POST', '/v1/wallet/lx/sign', { approval_id: approvalId },
      ), principal);
      sameLxReview(record.review, artifact);
      if (artifact.state !== 'signed' && artifact.state !== 'signing_unknown') {
        throw lxError('LX_SIGNATURE_UNAVAILABLE', 'The signing outcome is unknown; query the retained approval');
      }
      await this.lxSave(principal, { ...record, phase: artifact.state, artifact });
      return freezeLx(artifact);
    });
  }

  async lxApprovalStatus(approvalId: string): Promise<LxActivityArtifact> {
    requireLxUUID(approvalId);
    return this.lxExclusive(approvalId, async () => {
      const principal = await this.lxPrincipal();
      const record = await this.lxLoad(principal, approvalId);
      if (record) return freezeLx((await this.lxRefresh(principal, record)).artifact);
      const artifact = validateLxArtifact(await this.lxRequest(
        principal, 'GET', `/v1/wallet/lx/approvals/${approvalId}`,
      ), principal);
      if (artifact.id !== approvalId) throw lxError('LX_REVIEW_MISMATCH', 'Approval ID changed');
      return freezeLx(artifact);
    });

  private lxOptions(): LxActivityOptions {
    if (!this.lxActivity) throw lxError('LX_ACTIVITY_CONFIGURATION', 'Configure explicit confirmation and durable storage');
    return this.lxActivity;
  }

  private async lxPrincipal(): Promise<string> {
    this.lxOptions();
    const session = await this.getSession();
    if (!session?.access_token) throw lxError('NO_SESSION', 'Authentication is required', 401);
    const { data, error } = await this.supabase.auth.getUser(session.access_token);
    if (error || !data.user || data.user.id !== session.user.id) {
      throw lxError('NO_SESSION', 'The original Supabase session could not be verified', 401);
    }
    requireLxUUID(data.user.id);
    return data.user.id;
  }

  private async lxAssertPrincipal(principal: string): Promise<Session> {
    const session = await this.getSession();
    if (!session?.access_token || session.user.id !== principal) {
      throw lxError('LX_PRINCIPAL_CHANGED', 'The authenticated principal changed', 401);
    }
    return session;
  }

  private async lxRequest(principal: string, method: 'GET' | 'POST', path: string, body?: unknown): Promise<unknown> {
    const session = await this.lxAssertPrincipal(principal);
    const response = await this.fetchImpl(`${this.apiUrl}${path}`, {
      method,
      headers: { Authorization: `Bearer ${session.access_token}`, 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
      redirect: 'error',
    });
    const payload: unknown = await response.json();
    await this.lxAssertPrincipal(principal);
    if (!response.ok) {
      const failure = lxObject(payload);
      throw new PaxeerWalletError(
        typeof failure.message === 'string' ? failure.message : `LX request failed: ${response.status}`,
        typeof failure.error === 'string' ? failure.error : `HTTP_${response.status}`,
        response.status, payload,
      );
    }
    return payload;
  }

  private lxStorageKey(principal: string, id: string): string {
    return `paxeer:lx-activity:v1:${encodeURIComponent(this.apiUrl)}:${principal}:${id}`;
  }

  private async lxLoad(principal: string, id: string): Promise<LxActivityRecord | null> {
    const raw = await this.lxOptions().storage.getItem(this.lxStorageKey(principal, id));
    if (raw === null) return null;
    const saved = lxObject(JSON.parse(raw));
    const review = validateLxArtifact(saved.review, principal);
    const artifact = validateLxArtifact(saved.artifact, principal);
    if (saved.version !== 1 || review.state !== 'reviewed' || review.id !== id ||
      typeof saved.confirmed !== 'boolean' || !['reviewed', 'approval_unknown', 'approved', 'signing_unknown', 'signed'].includes(String(saved.phase))) {
      throw lxError('LX_STORAGE_INVALID', 'The retained original review is invalid');
    }
    sameLxReview(review, artifact);
    const phase = saved.phase as LxActivityRecord['phase'];
    if ((phase === 'reviewed' && (saved.confirmed || artifact.state !== 'reviewed')) ||
      (phase !== 'reviewed' && !saved.confirmed) ||
      (phase === 'approval_unknown' && artifact.state !== 'reviewed') ||
      (phase === 'approved' && artifact.state !== 'approved') ||
      (phase === 'signing_unknown' && artifact.state !== 'approved' && artifact.state !== 'signing_unknown') ||
      (phase === 'signed' && artifact.state !== 'signed')) {
      throw lxError('LX_STORAGE_INVALID', 'The retained approval transition is invalid');
    }
    return { version: 1, review, artifact, phase, confirmed: saved.confirmed };
  }

  private async lxRequiredRecord(principal: string, id: string): Promise<LxActivityRecord> {
    const record = await this.lxLoad(principal, id);
    if (!record) throw lxError('LX_ORIGINAL_REVIEW_REQUIRED', 'The durable original review is missing');
    return record;
  }

  private async lxSave(principal: string, record: LxActivityRecord): Promise<void> {
    await this.lxAssertPrincipal(principal);
    await this.lxOptions().storage.setItem(this.lxStorageKey(principal, record.review.id), JSON.stringify(record));
  }

  private async lxRefresh(principal: string, record: LxActivityRecord): Promise<LxActivityRecord> {
    const artifact = validateLxArtifact(await this.lxRequest(
      principal, 'GET', `/v1/wallet/lx/approvals/${record.review.id}`,
    ), principal);
    sameLxReview(record.review, artifact);
    if ((record.artifact.state === 'signed' && artifact.state !== 'signed') ||
      (record.artifact.state === 'signing_unknown' && artifact.state === 'approved') ||
      (record.artifact.state !== 'reviewed' && artifact.state === 'reviewed')) {
      throw lxError('LX_STATE_REGRESSION', 'The gateway approval state regressed');
    }
    if (record.artifact.state === 'signed' && JSON.stringify(record.artifact) !== JSON.stringify(artifact)) {
      throw lxError('LX_STATE_REGRESSION', 'The retained signature evidence changed');
    }
    if (!record.confirmed && artifact.state !== 'reviewed') {
      throw lxError('LX_CONFIRMATION_REQUIRED', 'Remote approval cannot replace local explicit confirmation');
    }
    const phase = artifact.state;
    const next: LxActivityRecord = { ...record, artifact, phase };
    if (phase === 'reviewed') next.confirmed = false;
    await this.lxSave(principal, next);
    return next;
  }

  private async lxExclusive<T>(id: string, work: () => Promise<T>): Promise<T> {
    const prior = this.lxLocks.get(id) ?? Promise.resolve();
    const next = prior.catch(() => undefined).then(work);
    this.lxLocks.set(id, next);
    try { return await next; }
    finally { if (this.lxLocks.get(id) === next) this.lxLocks.delete(id); }
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

function lxError(code: string, message: string, status = 400): PaxeerWalletError {
  return new PaxeerWalletError(message, code, status);
}

function lxObject(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw lxError('LX_ARTIFACT_INVALID', 'Expected an LX object');
  }
  return value as Record<string, unknown>;
}

function lxExact(value: Record<string, unknown>, keys: readonly string[]): void {
  if (Object.keys(value).length !== keys.length || keys.some((key) => !Object.prototype.hasOwnProperty.call(value, key))) {
    throw lxError('LX_ARTIFACT_INVALID', 'Unexpected or missing LX fields');
  }
}

function lxText(value: unknown): string {
  if (typeof value !== 'string' || value.length === 0) throw lxError('LX_ARTIFACT_INVALID', 'Expected nonempty text');
  return value;
}

function requireLxUUID(value: unknown): asserts value is string {
  if (typeof value !== 'string' || !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value)) {
    throw lxError('LX_ARTIFACT_INVALID', 'Expected a canonical UUID');
  }
}

function lxInteger(value: unknown, maximum: number): number {
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0 || value > maximum) {
    throw lxError('LX_ARTIFACT_INVALID', 'Expected a bounded unsigned integer');
  }
  return value;
}

function lxDecimal(value: unknown, bits: number): string {
  if (typeof value !== 'string' || value.length > Math.ceil(bits * Math.LOG10E * Math.LN2) ||
    !/^(0|[1-9][0-9]*)$/.test(value) || BigInt(value) >= (1n << BigInt(bits))) {
    throw lxError('LX_ARTIFACT_INVALID', 'Expected canonical unsigned decimal');
  }
  return value;
}

function lxHex(value: unknown, bytes: number): string {
  if (typeof value !== 'string' || value.length !== bytes * 2 || !/^[0-9a-f]+$/.test(value)) {
    throw lxError('LX_ARTIFACT_INVALID', 'Expected canonical lowercase hexadecimal');
  }
  return value;
}

function lxBytes(hex: string): Uint8Array {
  const bytes = new Uint8Array(hex.length / 2);
  for (let i = 0; i < bytes.length; i++) bytes[i] = Number.parseInt(hex.slice(i * 2, i * 2 + 2), 16);
  return bytes;
}

function lxToHex(bytes: Uint8Array): string {
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('');
}

function lxConcat(...parts: Uint8Array[]): Uint8Array {
  const out = new Uint8Array(parts.reduce((size, part) => size + part.length, 0));
  let offset = 0;
  for (const part of parts) { out.set(part, offset); offset += part.length; }
  return out;
}

function lxHash(domain: string, bytes: Uint8Array): string {
  return lxToHex(sha256(lxConcat(new TextEncoder().encode(domain), bytes)));
}

interface LxDecodedActivity {
  protocol: number;
  network: number;
  module: string;
  operation: number;
  did: string;
  publicKey: string;
  account: string;
  sequence: string;
  notBefore: string;
  notAfter: string;
  preimage: string;
  sendEffect?: { asset: string; amount: string; destination: string };
}

function decodeLxActivity(activity: unknown, kind: LxActivityReview['kind']): LxDecodedActivity {
  if (typeof activity !== 'string' || activity.length === 0 || activity.length > 2_097_152 ||
    activity.length % 2 !== 0 || !/^[0-9a-f]+$/.test(activity)) {
    throw lxError('LX_ACTIVITY_INVALID', 'Expected bounded unsigned canonical LX activity bytes');
  }
  const bytes = lxBytes(activity);
  let offset = 0;
  const take = (count: number): Uint8Array => {
    if (offset + count > bytes.length) throw lxError('LX_ACTIVITY_INVALID', 'Truncated LX activity');
    const out = bytes.subarray(offset, offset + count);
    offset += count;
    return out;
  };
  const integer = (count: number): bigint => {
    let value = 0n;
    for (const byte of take(count)) value = (value << 8n) | BigInt(byte);
    return value;
  };
  const tag = (expected: number): void => {
    if (Number(integer(1)) !== expected) throw lxError('LX_ACTIVITY_INVALID', 'Noncanonical LX field order');
  };
  const sized = (maximum: number, exact?: number): Uint8Array => {
    const count = Number(integer(4));
    if (count > maximum || (exact !== undefined && count !== exact)) {
      throw lxError('LX_ACTIVITY_INVALID', 'Invalid LX field length');
    }
    return take(count);
  };
  const version = Number(integer(2));
  if (version < 1 || version > 3) throw lxError('LX_VERSION_UNSUPPORTED', 'The existing LX decoder supports versions 1 through 3');
  if (integer(2) !== 0x1001n || integer(1) !== 11n) {
    throw lxError('LX_ACTIVITY_INVALID', 'Expected the unsigned canonical Activity structure');
  }
  tag(1);
  const protocol = Number(integer(2));
  if (protocol !== version) throw lxError('LX_ACTIVITY_INVALID', 'Activity protocol versions differ');
  tag(2); const network = Number(integer(4));
  tag(3); const activityType = Number(integer(4));
  const modules = ['', 'asset', 'escrow', 'budget', 'stream', 'service', 'perps', 'governance', 'bridge', 'programs', 'spot', 'web'];
  const module = modules[Math.floor(activityType / 65536)];
  const operation = activityType % 65536;
  if (!module || operation === 0) throw lxError('LX_ACTIVITY_INVALID', 'Unknown LX activity module or operation');
  tag(4);
  const did = new TextDecoder('utf-8', { fatal: true }).decode(sized(255));
  if (!/^did:layerx:[0-9a-f]{64}$/.test(did)) throw lxError('LX_ACTIVITY_INVALID', 'Invalid original actor DID');
  const publicKey = did.slice('did:layerx:'.length);
  requireLxPoint(publicKey);
  tag(5);
  if (lxToHex(sized(524_288)) !== publicKey) {
    throw lxError('LX_OWNER_AUTHORITY_REQUIRED', 'The activity must use its original owner public key authority');
  }
  tag(6); const sequence = integer(8).toString();
  tag(7); const notBefore = integer(8).toString(); const notAfter = integer(8).toString();
  if (BigInt(notAfter) < BigInt(notBefore)) throw lxError('LX_ACTIVITY_INVALID', 'Invalid activity validity window');
  tag(8); const idempotency = lxToHex(sized(32, 32));
  tag(9); integer(16);
  tag(10); const payloadHash = lxToHex(sized(32, 32));
  tag(11); const payload = sized(524_288);
  if (offset !== bytes.length || payloadHash !== lxHash('LXP/v1/payload-hash\0', payload)) {
    throw lxError('LX_ACTIVITY_INVALID', 'Activity payload hash or canonical length mismatch');
  }
  const name = new TextEncoder().encode(`agent:${did}:main`);
  const length = new Uint8Array(4);
  new DataView(length.buffer).setUint32(0, name.length, false);
  const decoded: LxDecodedActivity = {
    protocol, network, module, operation, did, publicKey, sequence, notBefore, notAfter,
    account: lxHash('LX:ACCOUNT:v1', lxConcat(length, name)),
    preimage: lxHash('LXP/v1/signature-preimage\0', bytes),
  };
  if (kind === 'lx_send_authorization') {
    const send = decodeLxSendAuthorization(payload, decoded, idempotency);
    decoded.preimage = send.digest;
    decoded.sendEffect = { asset: send.asset, amount: send.amount, destination: send.destination };
  }
  return decoded;
}

function requireLxPoint(hex: string): void {
  try {
    const point = ed25519.ExtendedPoint.fromHex(lxBytes(hex));
    if (point.isSmallOrder() || lxToHex(point.toRawBytes()) !== hex) throw new Error('noncanonical point');
  } catch {
    throw lxError('LX_PUBLIC_KEY_INVALID', 'Expected a canonical non-small-order Ed25519 public key');
  }
}

function decodeLxSendAuthorization(
  payload: Uint8Array, activity: LxDecodedActivity, idempotency: string,
): { digest: string; asset: string; amount: string; destination: string } {
  if (payload.length > 512 || activity.module !== 'asset' || activity.operation !== 5) {
    throw lxError('LX_SEND_AUTHORIZATION_INVALID', 'Expected the existing bounded asset-send authorization payload');
  }
  let offset = 0;
  const take = (size: number): Uint8Array => {
    if (offset + size > payload.length) throw lxError('LX_SEND_AUTHORIZATION_INVALID', 'Truncated asset send');
    const value = payload.subarray(offset, offset + size);
    offset += size;
    return value;
  };
  const integer = (size: number): bigint => {
    let value = 0n;
    for (const byte of take(size)) value = (value << 8n) | BigInt(byte);
    return value;
  };
  if (integer(2) !== 0x5301n || integer(2) !== 10n) {
    throw lxError('LX_SEND_AUTHORIZATION_INVALID', 'Invalid asset-send structure');
  }
  const from = lxToHex(take(32));
  const destination = lxToHex(take(32));
  const asset = lxToHex(take(32));
  const amount = integer(16).toString();
  integer(8);
  const sendIdempotency = lxToHex(take(32));
  integer(8);
  const context = lxToHex(take(32));
  const conditions = Number(integer(1));
  if (conditions > 8) throw lxError('LX_SEND_AUTHORIZATION_INVALID', 'Too many send conditions');
  for (let i = 0; i < conditions; i++) {
    const condition = integer(1);
    if (condition !== 1n && condition !== 2n) throw lxError('LX_SEND_AUTHORIZATION_INVALID', 'Unknown send condition');
    integer(8);
  }
  const commonEnd = offset;
  const authorizationKind = integer(1);
  const controller = lxToHex(take(32));
  const publicKey = lxToHex(take(32));
  const signature = take(64);
  const tailStart = offset;
  const signedContext = lxToHex(take(32));
  const network = Number(integer(4));
  const protocol = Number(integer(2));
  if (offset !== payload.length || from !== activity.account || from === destination || amount === '0' ||
    authorizationKind !== 1n || controller !== from || publicKey !== activity.publicKey ||
    signature.some((byte) => byte !== 0) || context !== signedContext || network === 0 ||
    network !== activity.network || protocol !== activity.protocol || sendIdempotency !== idempotency) {
    throw lxError('LX_SEND_AUTHORIZATION_INVALID', 'Asset-send owner, context, placeholder, or envelope binding mismatch');
  }
  const message = lxConcat(
    payload.subarray(0, 2), payload.subarray(4, commonEnd),
    payload.subarray(commonEnd, commonEnd + 33), payload.subarray(tailStart),
  );
  return { digest: lxHash('LXP/v1/signature-preimage\0', message), asset, amount, destination };
}

function validateLxDisclosure(value: unknown, decoded: LxDecodedActivity): ActivityDisclosureWire {
  const wire = lxObject(value);
  lxExact(wire, ['account', 'module', 'operation', 'amounts', 'destinations', 'sequence', 'not_before', 'not_after']);
  if (!Array.isArray(wire.amounts) || !Array.isArray(wire.destinations)) {
    throw lxError('LX_ARTIFACT_INVALID', 'Invalid activity disclosure effects');
  }
  const disclosure: ActivityDisclosureWire = {
    account: lxHex(wire.account, 32), module: lxText(wire.module),
    operation: lxInteger(wire.operation, 65535),
    amounts: wire.amounts.map((entry: unknown) => {
      const amount = lxObject(entry);
      lxExact(amount, ['asset', 'amount']);
      return { asset: lxHex(amount.asset, 32), amount: lxDecimal(amount.amount, 128) };
    }),
    destinations: wire.destinations.map((entry: unknown) => lxHex(entry, 32)),
    sequence: lxDecimal(wire.sequence, 64), not_before: lxDecimal(wire.not_before, 64),
    not_after: lxDecimal(wire.not_after, 64),
  };
  if (disclosure.account !== decoded.account || disclosure.module !== decoded.module ||
    disclosure.operation !== decoded.operation || disclosure.sequence !== decoded.sequence ||
    disclosure.not_before !== decoded.notBefore || disclosure.not_after !== decoded.notAfter) {
    throw lxError('LX_DISCLOSURE_MISMATCH', 'Disclosure does not match the canonical activity');
  }
  if (decoded.sendEffect && (disclosure.amounts.length !== 1 || disclosure.destinations.length !== 1 ||
    disclosure.amounts[0]?.asset !== decoded.sendEffect.asset || disclosure.amounts[0]?.amount !== decoded.sendEffect.amount ||
    disclosure.destinations[0] !== decoded.sendEffect.destination)) {
    throw lxError('LX_DISCLOSURE_MISMATCH', 'Disclosure changed the original send authorization effects');
  }
  return disclosure;
}

function validateLxArtifact(value: unknown, principal: string): LxActivityArtifact {
  const wire = lxObject(value);
  const keys = ['id', 'kind', 'state', 'activity', 'signing_preimage', 'public_key', 'key_id', 'principal',
    'network_id', 'protocol_version', 'session_id', 'disclosure', 'expires_at', 'did', 'account_id', 'epoch', 'participants'];
  if (wire.state !== 'reviewed' && wire.state !== 'approved' && wire.state !== 'signing_unknown' && wire.state !== 'signed') {
    throw lxError('LX_ARTIFACT_INVALID', 'Unknown approval state');
  }
  if (wire.state !== 'reviewed') keys.push('approval');
  if (wire.state === 'signed') keys.push('signature', 'attestor_audit');
  lxExact(wire, keys);
  requireLxUUID(wire.id); requireLxUUID(wire.session_id); requireLxUUID(wire.principal);
  if (wire.principal !== principal) throw lxError('LX_PRINCIPAL_MISMATCH', 'Approval belongs to another principal', 403);
  if (wire.kind !== 'lx_activity' && wire.kind !== 'lx_send_authorization') {
    throw lxError('LX_ARTIFACT_INVALID', 'Unknown LX approval kind');
  }
  const decoded = decodeLxActivity(wire.activity, wire.kind);
  if (!Array.isArray(wire.participants) || wire.participants.length !== 3) {
    throw lxError('LX_PARTICIPANTS_INVALID', 'An exact three-node quorum is required');
  }
  const participants = wire.participants.map((entry: unknown) => lxText(entry));
  if (new Set(participants).size !== 3) throw lxError('LX_PARTICIPANTS_INVALID', 'Duplicate quorum participants');
  const review: LxActivityReview = {
    id: wire.id, kind: wire.kind, state: 'reviewed', activity: lxText(wire.activity),
    signing_preimage: lxHex(wire.signing_preimage, 32), public_key: lxHex(wire.public_key, 32),
    key_id: lxText(wire.key_id), principal: wire.principal,
    network_id: lxInteger(wire.network_id, 0xffffffff), protocol_version: lxInteger(wire.protocol_version, 3),
    session_id: wire.session_id, disclosure: validateLxDisclosure(wire.disclosure, decoded),
    expires_at: lxDecimal(wire.expires_at, 64), did: lxText(wire.did), account_id: lxHex(wire.account_id, 32),
    epoch: lxInteger(wire.epoch, Number.MAX_SAFE_INTEGER), participants,
  };
  if (review.signing_preimage !== decoded.preimage || review.public_key !== decoded.publicKey ||
    review.did !== decoded.did || review.account_id !== decoded.account ||
    review.network_id !== decoded.network || review.protocol_version !== decoded.protocol ||
    BigInt(review.expires_at) > BigInt(decoded.notAfter) || BigInt(review.expires_at) < BigInt(decoded.notBefore)) {
    throw lxError('LX_REVIEW_MISMATCH', 'Review changed the activity or original identity binding');
  }
  if (wire.state === 'reviewed') return review;
  const approvalWire = lxObject(wire.approval);
  lxExact(approvalWire, ['version', 'principal', 'key_id', 'network_id', 'protocol_version', 'session_id', 'activity_digest', 'expires_at']);
  const approval: ActivityApprovalWire = {
    version: 1, principal: review.principal, key_id: review.key_id, network_id: review.network_id,
    protocol_version: review.protocol_version, session_id: review.session_id,
    activity_digest: review.signing_preimage, expires_at: review.expires_at,
  };
  for (const [key, expected] of Object.entries(approval)) {
    if (approvalWire[key] !== expected) throw lxError('LX_APPROVAL_MISMATCH', 'Approval scope differs from the original review');
  }
  if (wire.state !== 'signed') return { ...review, state: wire.state, approval };
  const signature = lxHex(wire.signature, 64);
  requireLxPoint(signature.slice(0, 64));
  if (!ed25519.verify(lxBytes(signature), lxBytes(review.signing_preimage), lxBytes(review.public_key), { zip215: false })) {
    throw lxError('LX_SIGNATURE_INVALID', 'The signature does not verify against the original activity and public key');
  }
  if (!Array.isArray(wire.attestor_audit) || wire.attestor_audit.length !== 3) {
    throw lxError('LX_AUDIT_INVALID', 'Expected three attestor audit receipts');
  }
  const audit = wire.attestor_audit.map((entry: unknown) => {
    const receipt = lxObject(entry);
    lxExact(receipt, ['node_id', 'audit_sequence']);
    const node_id = lxText(receipt.node_id);
    const audit_sequence = lxInteger(receipt.audit_sequence, Number.MAX_SAFE_INTEGER);
    if (!participants.includes(node_id) || audit_sequence === 0) throw lxError('LX_AUDIT_INVALID', 'Invalid attestor audit binding');
    return { node_id, audit_sequence };
  });
  if (new Set(audit.map((entry) => entry.node_id)).size !== 3) throw lxError('LX_AUDIT_INVALID', 'Duplicate attestor audit receipts');
  return { ...review, state: 'signed', approval, signature, attestor_audit: audit };
}

function sameLxReview(review: LxActivityReview, artifact: LxActivityArtifact): void {
  const restored = validateLxArtifact({
    id: artifact.id, kind: artifact.kind, state: 'reviewed', activity: artifact.activity,
    signing_preimage: artifact.signing_preimage, public_key: artifact.public_key,
    key_id: artifact.key_id, principal: artifact.principal, network_id: artifact.network_id,
    protocol_version: artifact.protocol_version, session_id: artifact.session_id,
    disclosure: artifact.disclosure, expires_at: artifact.expires_at, did: artifact.did,
    account_id: artifact.account_id, epoch: artifact.epoch, participants: artifact.participants,
  }, review.principal);
  if (JSON.stringify(restored) !== JSON.stringify(review)) {
    throw lxError('LX_REVIEW_MISMATCH', 'The durable original review changed');
  }
}

function requireLxUnexpired(review: LxActivityReview): void {
  if (BigInt(Math.floor(Date.now() / 1000)) > BigInt(review.expires_at)) {
    throw lxError('LX_APPROVAL_EXPIRED', 'The original approval expired; obtain and explicitly review a new activity');
  }
}

function freezeLx<T>(value: T): T {
  const copy: T = JSON.parse(JSON.stringify(value));
  const freeze = (entry: unknown): void => {
    if (entry && typeof entry === 'object') {
      for (const child of Object.values(entry)) freeze(child);
      Object.freeze(entry);
    }
  };
  freeze(copy);
  return copy;
}
