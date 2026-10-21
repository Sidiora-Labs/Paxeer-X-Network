import { measureWalletOperation } from './measurement';
import { getAddress, type Address, type Hex } from 'viem';
import { decodeV2Contract, SdkClient, HttpError, SnapshotStreamV2, browserStreamTransportV2, type StreamStateV2, type V2ContractTypes } from '@kindle/sdk';
import { parseSiweMessage } from 'viem/siwe';
import { WalletSessionController, type AccountScope } from './session';

/** Mirrors the frozen AuthChallenge wire schema, including string chainId. */
export interface AuthChallenge {
  challengeId: string; wallet: string; nonce: string; domain: string; uri: string;
  chainId: '125'; purpose: 'login' | 'link_wallet' | 'step_up';
  issuedAt: string; expiresAt: string; message: string;
}
export interface WalletLoginProof { challengeId: string; message: string; signature: Hex; scope: AccountScope }
export interface LoginConsent { challengeId: string; domain: string; account: Address; chainId: 125; message: string; expiresAt: string; purpose: 'login'; fee: 'No transaction fee'; }
export function validateLoginChallenge(challenge: AuthChallenge, origin: string, account: Address, expectedStatement: string, now = Date.now()): LoginConsent {
  const page = new URL(origin);
  const uri = new URL(challenge.uri);
  const issued = Date.parse(challenge.issuedAt), expires = Date.parse(challenge.expiresAt);
  if (page.protocol !== 'https:' || uri.origin !== page.origin || challenge.domain !== page.host || challenge.chainId !== '125' || getAddress(challenge.wallet) !== getAddress(account) || challenge.purpose !== 'login') throw new Error('Authentication origin, account, purpose or chain mismatch');
  if (!/^[0-9a-f]{64}$/i.test(challenge.nonce) || !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(challenge.challengeId)) throw new Error('Invalid backend challenge identity');
  if (!Number.isFinite(issued) || !Number.isFinite(expires) || issued > now || expires <= now || expires <= issued || expires - issued > 300_000) throw new Error('Expired or invalid authentication challenge');
  if (!expectedStatement || expectedStatement.length > 256 || challenge.message.length > 2048) throw new Error('Invalid authentication statement');
  const parsed = parseSiweMessage(challenge.message);
  if (parsed.domain !== challenge.domain || parsed.address?.toLowerCase() !== account.toLowerCase() || parsed.uri !== challenge.uri || parsed.chainId !== 125 || parsed.version !== '1' || parsed.nonce !== challenge.nonce || parsed.issuedAt?.getTime() !== issued || parsed.expirationTime?.getTime() !== expires || parsed.statement !== expectedStatement || parsed.scheme || parsed.resources?.length || (parsed.requestId !== undefined && parsed.requestId !== challenge.challengeId) || parsed.notBefore) throw new Error('Signing bytes do not match the recognized login envelope');
  // A parser alone may accept trailing text. Require the exact known EIP4361 layout.
  const exact = `${challenge.domain} wants you to sign in with your Ethereum account:\n${getAddress(account)}\n\n${expectedStatement}\n\nURI: ${challenge.uri}\nVersion: 1\nChain ID: 125\nNonce: ${challenge.nonce}\nIssued At: ${challenge.issuedAt}\nExpiration Time: ${challenge.expiresAt}${parsed.requestId ? `\nRequest ID: ${challenge.challengeId}` : ''}`;
  if (challenge.message !== exact) throw new Error('Unexpected authentication signing bytes');
  return Object.freeze({ challengeId: challenge.challengeId, domain: challenge.domain, account: getAddress(account), chainId: 125, message: challenge.message, expiresAt: challenge.expiresAt, purpose: 'login', fee: 'No transaction fee' });
}

/** Signing creates a proof only. Backend verification/nonce consumption is the
 * sole authority for application login, and a connection never restores it. */
export interface WalletMessageSigner { signMessage(account: Address, message: string): Promise<Hex> }

export class WalletLogin {
  state: 'anonymous' | 'awaiting_signature' | 'verifying' = 'anonymous';
  private pending = false;
  private issuedProof?: WalletLoginProof;
  constructor(private readonly sessions: WalletSessionController, private readonly wallet: WalletMessageSigner) {}
  async sign(challenge: AuthChallenge, origin: string, expectedStatement: string, consent: (review: LoginConsent) => Promise<boolean>): Promise<WalletLoginProof> {
    if (this.pending || this.state === 'verifying') throw new Error('Login already in progress');
    challenge = Object.freeze({ ...challenge });
    const scope = this.sessions.capture();
    this.pending = true;
    try {
      this.sessions.assertCurrent(scope, true);
      const review = validateLoginChallenge(challenge, origin, scope.account, expectedStatement);
      this.state = 'awaiting_signature';
      if (!await consent(review)) throw new Error('Login consent declined');
      this.sessions.assertCurrent(scope, true);
      validateLoginChallenge(challenge, origin, scope.account, expectedStatement);
      const signature = await this.wallet.signMessage(scope.account, review.message);
      this.sessions.assertCurrent(scope, true);
      validateLoginChallenge(challenge, origin, scope.account, expectedStatement);
      this.state = 'verifying';
      return this.issuedProof = Object.freeze({ challengeId: challenge.challengeId, message: review.message, signature, scope });
    } catch (error) { this.state = 'anonymous'; throw error; }
    finally { this.pending = false; }
  }
  /** Map only frozen proof fields into the backend request. Apply its private
   * session response only after this epoch fence succeeds. */
  async verify<T>(proof: WalletLoginProof, verifyBackend: (proof: Pick<WalletLoginProof, 'challengeId' | 'message' | 'signature'>, signal: AbortSignal) => Promise<T>): Promise<T> {
    if (this.state !== 'verifying' || this.issuedProof !== proof) throw new Error('Login proof is stale or already consumed');
    this.issuedProof = undefined;
    try {
      this.sessions.assertCurrent(proof.scope, true);
      const result = await verifyBackend({ challengeId: proof.challengeId, message: proof.message, signature: proof.signature }, proof.scope.signal);
      this.sessions.assertCurrent(proof.scope, true);
      return result;
    } finally { this.state = 'anonymous'; }
  }
  verificationFinished() { this.issuedProof = undefined; this.state = 'anonymous'; }
}


function accessTokenDeadline(token: string, sessionId: string, now = Date.now()): number {
  if (token.length > 2048) throw new Error('Invalid identity access token');
  const parts = token.split('.');
  if (parts.length !== 3 || parts.some(part => !/^[A-Za-z0-9_-]+$/.test(part)) || parts[2]!.length !== 43) throw new Error('Invalid identity access token');
  const decode = (part: string): unknown => JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(Uint8Array.from(atob(part.replace(/-/g, '+').replace(/_/g, '/')), character => character.charCodeAt(0))));
  const header = decode(parts[0]!), claims = decode(parts[1]!);
  if (typeof header !== 'object' || header === null || !('alg' in header) || header.alg !== 'HS256' || !('typ' in header) || header.typ !== 'JWT') throw new Error('Unrecognized identity access token');
  if (typeof claims !== 'object' || claims === null || !('sid' in claims) || claims.sid !== sessionId || !('exp' in claims) || typeof claims.exp !== 'number' || !Number.isSafeInteger(claims.exp) || !('nbf' in claims) || typeof claims.nbf !== 'number' || !Number.isSafeInteger(claims.nbf) || claims.nbf * 1000 > now + 30_000 || claims.exp * 1000 <= now) throw new Error('Expired or foreign identity access token');
  return claims.exp * 1000;
}

export class BrowserIdentitySession {
  private loginPending = false;
  private token?: string;
  private value?: V2ContractTypes['Session'];
  private rotating?: Promise<void>;
  private generation = 0;
  private abort = new AbortController();
  private observers = new Set<() => void>();
  private readonly unbind: () => void;
  private readonly origin: string;
  private readonly apiOrigin: string;
  private readonly streamOrigin: string;
  private feed?: SnapshotStreamV2;
  private feedToken?: string;
  private feedState: StreamStateV2 = 'closed';
  private feedReason?: string;
  private feedTimer?: ReturnType<typeof setTimeout>;
  private expiryTimer?: ReturnType<typeof setTimeout>;
  private tokenExpiresAt = 0;
  private revalidating = false;
  private revalidation?: Promise<void>;
  private readonly restorePage = (event: PageTransitionEvent) => { if (event.persisted) void this.revalidateAfterRestore(); };
  private feedAttempts = 0;
  private privatePortfolio?: V2ContractTypes['PortfolioUpdate'];
  private readonly restoreVisibility = () => { if (document.visibilityState === 'visible' && this.value) void this.restartPrivateStream().catch(error => { this.feedReason = error instanceof Error ? error.message : 'Session verification failed'; this.notify(); }); };
  private readonly restoreOnline = () => { if (this.value) void this.restartPrivateStream().catch(error => { this.feedReason = error instanceof Error ? error.message : 'Session verification failed'; this.notify(); }); };
  constructor(private readonly wallets: WalletSessionController, identityOrigin: string = window.location.origin, apiOrigin: string = window.location.origin, streamOrigin: string = `wss://${window.location.host}`, private readonly verifyConnection?: (scope: AccountScope) => Promise<void>) {
    const origin = new URL(identityOrigin);
    if (origin.protocol !== 'https:' || origin.origin !== window.location.origin || origin.origin !== identityOrigin) throw new Error('Identity requires the HTTPS application origin');
    this.origin = origin.origin;
    this.apiOrigin = apiOrigin;
    this.streamOrigin = streamOrigin;
    new SdkClient({ apiOrigin, identityOrigin, mediaOrigin: identityOrigin, streamOrigin, approvedStreamOrigins: [streamOrigin], production: true });
    document.addEventListener('visibilitychange', this.restoreVisibility);
    window.addEventListener('online', this.restoreOnline);
    window.addEventListener('pageshow', this.restorePage);
    this.unbind = wallets.subscribe(() => this.invalidate());
  }
  private expireCachedSession() {
    if (this.value && (this.tokenExpiresAt <= Date.now() || !Number.isFinite(Date.parse(this.value.accessExpiresAt)) || Date.parse(this.value.accessExpiresAt) <= Date.now() || !Number.isFinite(Date.parse(this.value.refreshExpiresAt)) || Date.parse(this.value.refreshExpiresAt) <= Date.now())) this.invalidate(false);
  }
  get current() { this.expireCachedSession(); return this.revalidating ? undefined : this.value; }
  get accessToken() { this.expireCachedSession(); return this.revalidating ? undefined : this.token; }
  get privateStreamState() { return this.feedState; }
  get privateStreamReason() { return this.feedReason; }
  get portfolio() { this.expireCachedSession(); return this.revalidating ? undefined : this.privatePortfolio; }
  subscribe(observer: () => void) { this.observers.add(observer); return () => { this.observers.delete(observer); }; }
  private notify() { for (const observer of this.observers) observer(); }
  private csrf() {
    const entry = document.cookie.split(';').map(value => value.trim()).find(value => value.startsWith('__Host-kindle_csrf='));
    const value = entry?.slice('__Host-kindle_csrf='.length);
    if (!value || !/^[0-9a-f]{64}$/.test(value)) throw new Error('Session CSRF token unavailable');
    return value;
  }
  private async fetchJson(path: string, body: unknown | undefined, token: string | undefined, signal: AbortSignal, method?: 'DELETE'): Promise<unknown> {
    const headers: Record<string, string> = { Accept: 'application/json' };
    if (token) headers.Authorization = `Bearer ${token}`;
    if (body !== undefined) {
      headers['Content-Type'] = 'application/json';
      if (path !== '/v2/auth/challenges' && path !== '/v2/auth/sessions') headers['X-CSRF-Token'] = this.csrf();
    }
    const kind = path === '/v2/auth/challenges' ? 'auth.challenge' : path === '/v2/auth/sessions:refresh' ? 'auth.refresh' : body === undefined ? 'auth.read' : 'auth.login';
    const response = await measureWalletOperation(kind, `epoch:${this.wallets.sessionEpoch}`, async () => { const response = await fetch(new URL(path, this.origin), { method: method ?? (body === undefined ? 'GET' : 'POST'), headers, credentials: 'same-origin', redirect: 'error', cache: 'no-store', signal, ...(body === undefined ? {} : { body: JSON.stringify(body) }) }); if (!response.ok) throw new Error(`Identity request failed (${response.status})`); return response; });
    if (!response.ok) throw new Error(`Identity request failed (${response.status})`);
    if (response.status === 204) { signal.throwIfAborted(); return null; }
    const value: unknown = await response.json();
    signal.throwIfAborted();
    return value;
  }
  private install(data: V2ContractTypes['LoginData'], scope: AccountScope, generation: number) {
    this.wallets.assertCurrent(scope, true);
    if (!Number.isFinite(Date.parse(data.session.accessExpiresAt)) || !Number.isFinite(Date.parse(data.session.refreshExpiresAt)) || generation !== this.generation || data.session.wallet.toLowerCase() !== scope.account.toLowerCase() || data.session.revokedAt !== null || Date.parse(data.session.accessExpiresAt) <= Date.now() || Date.parse(data.session.refreshExpiresAt) <= Date.now()) throw new Error('Foreign, stale or revoked identity session');
    if (this.feedToken && this.feedToken !== data.accessToken) this.closePrivateStream(false);
    const deadline = accessTokenDeadline(data.accessToken, data.session.sessionId);
    this.tokenExpiresAt = Math.min(deadline, Date.parse(data.session.accessExpiresAt));
    this.token = data.accessToken;
    this.value = Object.freeze({ ...data.session, scopes: [...data.session.scopes] });
    Object.freeze(this.value.scopes);
    this.scheduleExpiry();
    this.notify();
  }
  async login(signer: WalletMessageSigner, kind: 'eoa_eip191' | 'erc1271', consent: (review: LoginConsent) => Promise<boolean>): Promise<void> {
    if (this.loginPending) throw new Error('Login already in progress');
    this.loginPending = true;
    try {
    const scope = this.wallets.capture(), generation = this.generation;
    this.wallets.assertCurrent(scope, true);
    const challenge = decodeV2Contract('ChallengeResponse', await this.fetchJson('/v2/auth/challenges', { wallet: scope.account, origin: this.origin }, undefined, scope.signal)).data.challenge;
    if (!challenge.message.endsWith(`\nRequest ID: ${challenge.challengeId}`)) throw new Error('Login challenge request binding missing');
    const login = new WalletLogin(this.wallets, signer);
    const proof = await login.sign(challenge, this.origin, 'Sign in to Sidiora.', consent);
    const result = await login.verify(proof, async (_, signal) => decodeV2Contract('LoginResponse', await this.fetchJson('/v2/auth/sessions', { challengeId: proof.challengeId, proof: { kind, wallet: scope.account, signature: proof.signature }, deviceLabel: 'Browser wallet' }, undefined, signal)));
    this.install(result.data, scope, generation);
    await this.readSession();
    } finally { this.loginPending = false; }
  }
  private scheduleExpiry() {
    clearTimeout(this.expiryTimer);
    if (!this.value) return;
    const deadline = Math.min(this.tokenExpiresAt, Date.parse(this.value.accessExpiresAt), Date.parse(this.value.refreshExpiresAt));
    this.expiryTimer = setTimeout(() => { this.expireCachedSession(); if (this.value) this.scheduleExpiry(); }, Math.min(2_147_483_647, Math.max(1, deadline - Date.now())));
  }
  async refresh(): Promise<void> {
    this.expireCachedSession();
    if (!this.value || !this.token) throw new Error('Fresh wallet proof is required to restore application access');
    if (this.rotating) return this.rotating;
    const scope = this.wallets.capture(), generation = this.generation;
    this.wallets.assertCurrent(scope, true);
    const work = (async () => {
      try {
        const result = decodeV2Contract('RefreshResponse', await this.fetchJson('/v2/auth/sessions:refresh', {}, undefined, this.abort.signal));
        this.install(result.data, scope, generation);
      } catch (error) { if (generation === this.generation) this.invalidate(false); throw error; }
    })();
    this.rotating = work;
    try { await work; } finally { if (this.rotating === work) this.rotating = undefined; }
  }
  async restore(): Promise<void> { throw new Error('Fresh wallet proof is required. Sign in explicitly to restore application access.'); }
  async readSession(): Promise<V2ContractTypes['Session']> {
    const generation = this.generation;
    try { return await this.readCurrentSession(); }
    catch (error) { if (generation === this.generation) this.invalidate(false); throw error; }
  }
  private async readCurrentSession(): Promise<V2ContractTypes['Session']> {
    const scope = this.wallets.capture(), generation = this.generation;
    if (!this.value || !this.token) throw new Error('Application sign-in required');
    if (Math.min(this.tokenExpiresAt, Date.parse(this.value.accessExpiresAt)) <= Date.now() + 30_000) await this.refresh();
    const value = await this.fetchJson('/v2/auth/sessions', undefined, this.token, this.abort.signal);
    const envelope = decodeV2Contract('SessionsResponse', value);
    const session = envelope.data.items.find(entry => entry.sessionId === this.value?.sessionId);
    this.wallets.assertCurrent(scope, true);
    if (generation !== this.generation || !session || session.revokedAt !== null || session.wallet.toLowerCase() !== scope.account.toLowerCase() || !Number.isFinite(Date.parse(session.accessExpiresAt)) || Date.parse(session.accessExpiresAt) <= Date.now() || !Number.isFinite(Date.parse(session.refreshExpiresAt)) || Date.parse(session.refreshExpiresAt) <= Date.now()) { this.invalidate(false); throw new Error('Session revoked or changed'); }
    this.value = Object.freeze({ ...session, scopes: [...session.scopes] }); Object.freeze(this.value.scopes); this.scheduleExpiry(); this.notify();
    if (!this.revalidating) this.startPrivateStream();
    return session;
  }
  revalidateAfterRestore(): Promise<void> {
    if (this.revalidation) return this.revalidation;
    this.expireCachedSession();
    if (!this.value || !this.token) return Promise.resolve();
    const generation = this.generation, scope = this.wallets.capture();
    this.revalidating = true;
    this.closePrivateStream(true);
    this.notify();
    const work = (async () => {
      try {
        if (!this.verifyConnection) throw new Error('Wallet connection authority unavailable after page restoration');
        await this.verifyConnection(scope);
        this.wallets.assertCurrent(scope, true);
        await this.readSession();
        if (generation !== this.generation) return;
        this.revalidating = false;
        this.notify();
        this.startPrivateStream();
      } catch {
        if (generation === this.generation) this.invalidate(false);
      }
    })();
    this.revalidation = work;
    void work.finally(() => { if (this.revalidation === work) this.revalidation = undefined; });
    return work;
  }
  private closePrivateStream(clear: boolean) {
    clearTimeout(this.feedTimer); this.feedTimer = undefined;
    const feed = this.feed; this.feed = undefined; this.feedToken = undefined;
    feed?.close();
    this.feedState = 'closed'; this.feedReason = undefined;
    if (clear) this.privatePortfolio = undefined;
  }
  private startPrivateStream() {
    if (!this.value || !this.token || this.feed) return;
    const sessionId = this.value.sessionId, wallet = this.value.wallet.toLowerCase(), generation = this.generation;
    const scope = this.wallets.capture();
    this.wallets.assertCurrent(scope, true);
    const current = () => this.current?.sessionId === sessionId && this.generation === generation && !this.abort.signal.aborted && !scope.signal.aborted;
    const assertCurrent = () => { this.wallets.assertCurrent(scope, true); if (!current()) throw new Error('Private stream authorization changed'); };
    const topic = `private.portfolio.${wallet}`;
    const client = new SdkClient({ apiOrigin: this.apiOrigin, identityOrigin: this.origin, mediaOrigin: this.origin, streamOrigin: this.streamOrigin, approvedStreamOrigins: [this.streamOrigin], production: true, session: { mode: 'bearer', token: () => current() ? this.token : undefined } });
    const transport = browserStreamTransportV2(client, { applicationOrigin: this.origin, snapshotPath: '/v2/snapshots' }, true);
    const authorize = async <T>(work: () => Promise<T>): Promise<T> => {
      assertCurrent();
      try { const result = await work(); assertCurrent(); return result; }
      catch (error) { if (error instanceof HttpError && (error.status === 401 || error.status === 403)) this.invalidate(false); throw error; }
    };
    const installPortfolio = (payload: unknown, expectedRevision: string) => {
      const portfolio = decodeV2Contract('PortfolioUpdate', payload);
      if (portfolio.summary.wallet.toLowerCase() !== wallet || portfolio.snapshotId !== portfolio.summary.snapshotId || portfolio.summaryRevision !== expectedRevision || !portfolio.completeBatch) throw new Error('Foreign or incomplete private portfolio snapshot');
      for (const change of portfolio.changes) {
        if (change.operation === 'upsert' && (!change.position || change.position.wallet.toLowerCase() !== wallet || change.positionId !== change.position.positionId)) throw new Error('Foreign private portfolio position');
        if (change.operation === 'delete' && change.position !== null) throw new Error('Invalid private portfolio deletion');
      }
      this.privatePortfolio = Object.freeze(portfolio); this.notify();
    };
    const feed = new SnapshotStreamV2({
      transport: { snapshot: (topics, signal) => authorize(() => transport.snapshot(topics, signal)), ticket: (topics, signal) => authorize(() => { if (!transport.ticket) throw new Error('Private stream ticket authority unavailable'); return transport.ticket(topics, signal); }), connect: input => authorize(() => transport.connect(input)) },
      installSnapshot: snapshot => {
        assertCurrent();
        if (snapshot.topics.length !== 1 || snapshot.topics[0]?.topic !== topic) throw new Error('Foreign private portfolio snapshot');
        installPortfolio(snapshot.topics[0].payload, snapshot.topics[0].revision);
      },
      applyEvent: event => {
        assertCurrent();
        if (event.topic !== topic) throw new Error('Foreign private portfolio topic');
        installPortfolio(event.payload, event.revision);
      },
      onState: (state, reason) => {
        if (!current() || this.feed !== feed) return;
        this.feedState = state; this.feedReason = reason;
        if (state === 'live') this.feedAttempts = 0;
        if ((state === 'stale' || state === 'resync-required') && this.feedAttempts < 5) {
          clearTimeout(this.feedTimer);
          const delay = Math.min(30_000, 1000 * 2 ** this.feedAttempts++);
          this.feedTimer = setTimeout(() => { this.feedTimer = undefined; if (current()) void this.restartPrivateStream(false).catch(error => { this.feedReason = error instanceof Error ? error.message : 'Session verification failed'; this.notify(); }); }, delay);
        }
        this.notify();
      },
    });
    this.feed = feed; this.feedToken = this.token;
    void feed.start([topic], this.abort.signal);
  }
  async restartPrivateStream(resetAttempts = true): Promise<void> {
    if (!this.value) return;
    if (resetAttempts) this.feedAttempts = 0;
    this.closePrivateStream(false);
    await this.readSession();
  }
  async logout(all = false): Promise<void> {
    if (all) throw new Error('Logout-all endpoint is not admitted');
    const token = this.token, sessionId = this.value?.sessionId;
    this.invalidate(false);
    try { if (token && sessionId) await this.fetchJson(`/v2/auth/sessions/${sessionId}`, {}, token, new AbortController().signal, 'DELETE'); }
    finally { document.cookie = '__Host-kindle_csrf=; Path=/; Secure; SameSite=Lax; Max-Age=0'; }
  }
  invalidate(revoke = true) {
    const token = this.token, sessionId = this.value?.sessionId;
    this.generation++; clearTimeout(this.expiryTimer); this.expiryTimer = undefined; this.closePrivateStream(true); this.abort.abort(); this.abort = new AbortController();
    this.token = undefined; this.value = undefined; this.tokenExpiresAt = 0; this.rotating = undefined; this.revalidating = false; this.revalidation = undefined;
    this.notify();
    if (revoke && token && sessionId) void this.fetchJson(`/v2/auth/sessions/${sessionId}`, {}, token, new AbortController().signal, 'DELETE').catch(() => { this.notify(); });
  }
  dispose() { document.removeEventListener('visibilitychange', this.restoreVisibility); window.removeEventListener('online', this.restoreOnline); window.removeEventListener('pageshow', this.restorePage); this.unbind(); this.invalidate(); this.observers.clear(); }
}
