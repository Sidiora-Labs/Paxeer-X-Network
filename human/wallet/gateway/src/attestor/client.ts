import { Agent, request as httpsRequest } from 'node:https';
import { randomUUID } from 'node:crypto';
import { readFileSync } from 'node:fs';
import {
  getTransactionType,
  getTypesForEIP712Domain,
  hexToBytes,
  recoverMessageAddress,
  recoverTransactionAddress,
  recoverTypedDataAddress,
  serializeSignature,
  serializeTransaction,
  stringToHex,
  toHex,
  type Hex,
  type TransactionSerializable,
  type TypedDataDefinition,
} from 'viem';
import {
  QuorumUnavailableError,
  selectQuorum,
  type AttestorHealthReport,
  type NodeHealth,
  type QuorumMember,
} from './quorum.js';

export const ATTESTOR_API_VERSION = 1;

export const ATTESTOR_PATHS = {
  health: '/health',
  generate: '/v1/keys/generate',
  refresh: '/v1/keys/refresh',
  sign: '/v1/sign',
} as const;

export const AGENT_HEADERS = {
  key: 'X-Agent-Key',
  nonce: 'X-Agent-Nonce',
  expiry: 'X-Agent-Expiry',
  signature: 'X-Agent-Signature',
} as const;

export type SignKind =
  | 'evm_tx'
  | 'eip712'
  | 'personal_message'
  | 'eth_sign_digest'
  | 'lx_activity'
  | 'lx_bind'
  | 'lx_grant';

export type Curve = 'secp256k1' | 'ed25519';

export const KIND_CURVES: Record<SignKind, Curve> = {
  evm_tx: 'secp256k1',
  eip712: 'secp256k1',
  personal_message: 'secp256k1',
  eth_sign_digest: 'secp256k1',
  lx_activity: 'ed25519',
  lx_bind: 'ed25519',
  lx_grant: 'ed25519',
};

export const SIGNATURE_BYTES: Record<Curve, number> = { secp256k1: 65, ed25519: 64 };

export type Authorisation =
  | { scheme: 'supabase_jwt'; token: string }
  | { scheme: 'agent_ed25519'; publicKey: string; nonce: string; expiry: number; signature: string };

export interface GrantWire {
  from: string;
  recipient: string;
  asset: string;
  per_draw_maximum: string;
  allowance: string;
  recurring: boolean;
  window_length: number;
  expiration: number;
  purpose_hash: string;
  has_reference: boolean;
  reference_hash: string;
  revocation_sequence: number;
}

export interface ConstructionCallWire {
  to: string;
  value: string;
  data: string;
}

export interface ConstructionQuoteWire {
  sponsor: string;
  token: string;
  maxTokenAmount: string;
  tokenAmount: string;
  deadline: string;
  quoteNonce: string;
  gasCost: string;
}

export interface ConstructionWire {
  kind: string;
  chainId: string;
  account?: string;
  address?: string;
  nonce: string;
  calls?: ConstructionCallWire[];
  quote?: ConstructionQuoteWire;
}

export type SignPayload =
  | { kind: 'evm_tx'; transaction: Hex }
  | { kind: 'eip712'; typedData: string }
  | { kind: 'personal_message'; message: Hex }
  | { kind: 'eth_sign_digest'; digest: Hex; construction: ConstructionWire }
  | { kind: 'lx_activity'; activity: Hex; disclosure: ActivityDisclosureWire; approval: ActivityApprovalWire }
  | { kind: 'lx_bind'; message: Hex }
  | { kind: 'lx_grant'; grant: GrantWire };

// Every u128 amount and u64 sequence, bound and expiry travels as a canonical unsigned
// base-10 string (no sign, exponent or leading zeros), identical to the attestor and KMS wire.
export type DecimalWire = string;

export interface ActivityDisclosureWire {
  account: string;
  module: string;
  operation: number;
  amounts: { asset: string; amount: DecimalWire }[];
  destinations: string[];
  sequence: DecimalWire;
  not_before: DecimalWire;
  not_after: DecimalWire;
}

export interface ActivityApprovalWire {
  version: 1;
  principal: string;
  key_id: string;
  network_id: number;
  protocol_version: number;
  session_id: string;
  activity_digest: string;
  expires_at: DecimalWire;
}

const CANONICAL_DECIMAL = /^(0|[1-9][0-9]*)$/;

export function decimalWire(value: bigint, bits: 64 | 128): DecimalWire {
  if (value < 0n || value >= 1n << BigInt(bits)) {
    throw new AttestorSessionError('session_bad_request', `value is outside u${bits}`);
  }
  return value.toString(10);
}

function checkDecimal(label: string, value: unknown, bits: 64 | 128): void {
  if (typeof value !== 'string' || !CANONICAL_DECIMAL.test(value) || BigInt(value) >= 1n << BigInt(bits)) {
    throw new AttestorSessionError('session_bad_request', `${label} is not a canonical u${bits} decimal string`);
  }
}

function checkApproved(disclosure: ActivityDisclosureWire, approval: ActivityApprovalWire): void {
  disclosure.amounts.forEach((a, i) => checkDecimal(`disclosure amount ${i}`, a.amount, 128));
  checkDecimal('disclosure sequence', disclosure.sequence, 64);
  checkDecimal('disclosure not_before', disclosure.not_before, 64);
  checkDecimal('disclosure not_after', disclosure.not_after, 64);
  checkDecimal('approval expires_at', approval.expires_at, 64);
  if (approval.version !== 1) {
    throw new AttestorSessionError('session_bad_request', 'approval version is not 1');
  }
}

export interface SignRequestWire {
  session_id: string;
  key_id: string;
  kind: SignKind;
  signers: string[];
  transaction?: string;
  typed_data?: string;
  message?: string;
  digest?: string;
  activity?: string;
  grant?: GrantWire;
  construction?: ConstructionWire;
  disclosure?: ActivityDisclosureWire;
  approval?: ActivityApprovalWire;
}

export interface SignResponseWire {
  node_id: string;
  key_id: string;
  kind: string;
  signed_bytes: string;
  signature: string;
  recovery_id?: number;
  audit_sequence: number;
}

export interface KeyGenerateWire {
  session_id: string;
  key_id: string;
  curve: Curve;
  owner: string;
  account?: string;
}

export interface KeyRefreshWire {
  session_id: string;
  key_id: string;
}

export interface KeyResponseWire {
  node_id: string;
  key_id: string;
  curve: Curve;
  public_key: string;
  address?: string;
  did?: string;
  epoch: number;
  participants: string[];
  refreshed: boolean;
  audit_sequence: number;
}

export type RefusalCategory = 'token' | 'policy' | 'quorum' | 'session' | 'key' | 'store';

export interface RefusalWire {
  error: { category: RefusalCategory; code: string; message: string; policy_code?: string };
}

export class AttestorError extends Error {
  readonly category: RefusalCategory;
  readonly code: string;
  readonly policyCode: string | null;
  readonly nodeId: string | null;
  constructor(
    category: RefusalCategory,
    code: string,
    reason: string,
    nodeId: string | null = null,
    policyCode: string | null = null,
  ) {
    super(`${category}: ${code}: ${reason}`);
    this.name = 'AttestorError';
    this.category = category;
    this.code = code;
    this.policyCode = policyCode;
    this.nodeId = nodeId;
  }
}

export class AttestorTokenError extends AttestorError {
  constructor(code: string, reason: string, nodeId: string | null = null) {
    super('token', code, reason, nodeId);
    this.name = 'AttestorTokenError';
  }
}

export class AttestorPolicyError extends AttestorError {
  constructor(code: string, reason: string, nodeId: string | null = null, policyCode: string | null = null) {
    super('policy', code, reason, nodeId, policyCode);
    this.name = 'AttestorPolicyError';
  }
}

export class AttestorQuorumError extends AttestorError {
  constructor(code: string, reason: string, nodeId: string | null = null) {
    super('quorum', code, reason, nodeId);
    this.name = 'AttestorQuorumError';
  }
}

export class AttestorSessionError extends AttestorError {
  constructor(code: string, reason: string, nodeId: string | null = null) {
    super('session', code, reason, nodeId);
    this.name = 'AttestorSessionError';
  }
}

export class AttestorKeyError extends AttestorError {
  constructor(code: string, reason: string, nodeId: string | null = null) {
    super('key', code, reason, nodeId);
    this.name = 'AttestorKeyError';
  }
}

export class AttestorStoreError extends AttestorError {
  constructor(code: string, reason: string, nodeId: string | null = null) {
    super('store', code, reason, nodeId);
    this.name = 'AttestorStoreError';
  }
}

const REFUSAL_STATUS: Record<RefusalCategory, number> = {
  token: 401,
  policy: 403,
  quorum: 503,
  session: 502,
  key: 409,
  store: 502,
};

export function attestorErrorStatus(err: AttestorError): number {
  return REFUSAL_STATUS[err.category];
}

export function attestorErrorBody(err: AttestorError): {
  error: string;
  category: RefusalCategory;
  code: string;
  policy_code?: string;
  message: string;
} {
  return {
    error: `attestor_${err.category}_refused`,
    category: err.category,
    code: err.code,
    ...(err.policyCode !== null ? { policy_code: err.policyCode } : {}),
    message: err.message,
  };
}

export function refusalFromWire(body: unknown, nodeId: string | null): AttestorError {
  const e = (body as Partial<RefusalWire> | null)?.error;
  if (!e || typeof e.code !== 'string' || typeof e.message !== 'string') {
    return new AttestorSessionError('malformed_refusal', 'participant answered with an unreadable error', nodeId);
  }
  if (e.policy_code !== undefined && typeof e.policy_code !== 'string') {
    return new AttestorSessionError('malformed_refusal', 'participant answered with an unreadable policy code', nodeId);
  }
  switch (e.category) {
    case 'token':
      return new AttestorTokenError(e.code, e.message, nodeId);
    case 'policy':
      return new AttestorPolicyError(e.code, e.message, nodeId, e.policy_code ?? null);
    case 'quorum':
      return new AttestorQuorumError(e.code, e.message, nodeId);
    case 'session':
      return new AttestorSessionError(e.code, e.message, nodeId);
    case 'key':
      return new AttestorKeyError(e.code, e.message, nodeId);
    case 'store':
      return new AttestorStoreError(e.code, e.message, nodeId);
    default:
      return new AttestorSessionError('unknown_refusal_category', `category ${String(e.category)}: ${e.message}`, nodeId);
  }
}

export interface AttestorTls {
  cert: string | Buffer;
  key: string | Buffer;
  ca: string | Buffer;
}

export interface AttestorClientOptions {
  endpoints: string[];
  tls: AttestorTls;
  quorum: number;
  healthIntervalMs: number;
  timeoutMs: number;
}

export interface SignInput {
  keyId: string;
  payload: SignPayload;
  authorisation: Authorisation;
}

export interface NodeAudit {
  node_id: string;
  audit_sequence: number;
}

export interface SignResult {
  sessionId: string;
  kind: SignKind;
  signature: Hex;
  recoveryId: number | null;
  signedBytes: Hex;
  participants: string[];
  audit: NodeAudit[];
}

export interface GenerateKeyInput {
  keyId: string;
  curve: Curve;
  owner: string;
  account?: string;
}

export interface KeyResult {
  sessionId: string;
  keyId: string;
  curve: Curve;
  publicKey: Hex;
  address: `0x${string}` | null;
  did: string | null;
  epoch: number;
  participants: string[];
  refreshed: boolean;
  audit: NodeAudit[];
}

interface HttpResult {
  status: number;
  body: unknown;
}

const REFUSAL_PRIORITY: Record<RefusalCategory, number> = { token: 0, policy: 1, key: 2, quorum: 3, session: 4, store: 5 };

const HEX_RE = /^[0-9a-f]+$/;

function bareHex(value: string): string {
  return (value.startsWith('0x') ? value.slice(2) : value).toLowerCase();
}

export function signRequestBody(sessionId: string, keyId: string, signers: string[], payload: SignPayload): SignRequestWire {
  const base = { session_id: sessionId, key_id: keyId, kind: payload.kind, signers };
  switch (payload.kind) {
    case 'evm_tx':
      return { ...base, transaction: bareHex(payload.transaction) };
    case 'eip712':
      return { ...base, typed_data: payload.typedData };
    case 'personal_message':
    case 'lx_bind':
      return { ...base, message: bareHex(payload.message) };
    case 'eth_sign_digest':
      return { ...base, digest: bareHex(payload.digest), construction: payload.construction };
    case 'lx_activity':
      if (payload.approval.session_id !== sessionId || payload.approval.key_id !== keyId) {
        throw new AttestorSessionError('session_bad_request', 'the approval binding names another session or key');
      }
      checkApproved(payload.disclosure, payload.approval);
      return { ...base, activity: bareHex(payload.activity), disclosure: payload.disclosure, approval: payload.approval };
    case 'lx_grant':
      return { ...base, grant: payload.grant };
  }
}

export function authorisationHeaders(auth: Authorisation): Record<string, string> {
  if (auth.scheme === 'supabase_jwt') return { authorization: `Bearer ${auth.token}` };
  return {
    [AGENT_HEADERS.key]: bareHex(auth.publicKey),
    [AGENT_HEADERS.nonce]: bareHex(auth.nonce),
    [AGENT_HEADERS.expiry]: String(auth.expiry),
    [AGENT_HEADERS.signature]: bareHex(auth.signature),
  };
}

function firstRefusal(settled: PromiseSettledResult<unknown>[], members: QuorumMember[], markDown: (endpoint: string, reason: unknown) => void): AttestorError | null {
  const refusals: AttestorError[] = [];
  settled.forEach((s, i) => {
    if (s.status === 'fulfilled') return;
    const member = members[i]!;
    if (s.reason instanceof AttestorError) {
      refusals.push(s.reason);
      return;
    }
    markDown(member.endpoint, s.reason);
    refusals.push(
      new AttestorSessionError(
        'participant_unreachable',
        s.reason instanceof Error ? s.reason.message : String(s.reason),
        member.nodeId,
      ),
    );
  });
  if (refusals.length === 0) return null;
  refusals.sort((a, b) => REFUSAL_PRIORITY[a.category] - REFUSAL_PRIORITY[b.category]);
  return refusals[0]!;
}

export class AttestorClient {
  private readonly agent: Agent;
  private readonly nodes: NodeHealth[];
  private readonly opts: AttestorClientOptions;
  private timer: NodeJS.Timeout | null = null;

  constructor(opts: AttestorClientOptions) {
    if (opts.endpoints.length < opts.quorum) {
      throw new Error(`attestor client needs at least ${opts.quorum} endpoints, got ${opts.endpoints.length}`);
    }
    this.opts = opts;
    this.agent = new Agent({
      cert: opts.tls.cert,
      key: opts.tls.key,
      ca: opts.tls.ca,
      keepAlive: true,
      rejectUnauthorized: true,
      minVersion: 'TLSv1.3',
    });
    this.nodes = opts.endpoints.map((endpoint) => ({
      endpoint: endpoint.replace(/\/+$/, ''),
      nodeId: null,
      healthy: false,
      latencyMs: null,
      checkedAt: null,
      report: null,
      lastError: null,
    }));
  }

  get quorum(): number {
    return this.opts.quorum;
  }

  health(): NodeHealth[] {
    return this.nodes.map((n) => ({ ...n }));
  }

  start(): void {
    if (this.timer) return;
    void this.refreshHealth();
    this.timer = setInterval(() => void this.refreshHealth(), this.opts.healthIntervalMs);
    this.timer.unref();
  }

  stop(): void {
    if (this.timer) clearInterval(this.timer);
    this.timer = null;
    this.agent.destroy();
  }

  async refreshHealth(): Promise<NodeHealth[]> {
    await Promise.all(this.nodes.map((node) => this.probe(node)));
    return this.health();
  }

  selectQuorum(): QuorumMember[] {
    return selectQuorum(this.nodes, this.opts.quorum);
  }

  async sign(input: SignInput): Promise<SignResult> {
    let members: QuorumMember[];
    try {
      members = this.selectQuorum();
    } catch (err) {
      if (err instanceof QuorumUnavailableError) {
        throw new AttestorQuorumError('quorum_unavailable', err.message);
      }
      throw err;
    }
    const kind = input.payload.kind;
    const curve = KIND_CURVES[kind];
    const sessionId = input.payload.kind === 'lx_activity' ? input.payload.approval.session_id : randomUUID();
    const participants = members.map((m) => m.nodeId);
    const body = JSON.stringify(signRequestBody(sessionId, input.keyId, participants, input.payload));
    const headers = authorisationHeaders(input.authorisation);
    const settled = await Promise.allSettled(
      members.map((m) => this.post(m, ATTESTOR_PATHS.sign, body, headers, (b) => parseSignResponse(b, m.nodeId, curve))),
    );
    const refusal = firstRefusal(settled, members, (e, r) => this.markDown(e, r));
    if (refusal) throw refusal;
    const answers = settled.map((s) => (s as PromiseFulfilledResult<SignResponseWire>).value);

    const first = answers[0]!;
    for (let i = 0; i < answers.length; i++) {
      const a = answers[i]!;
      const expectedNode = members[i]!.nodeId;
      if (a.node_id !== expectedNode) {
        throw new AttestorSessionError('participant_mismatch', `expected ${expectedNode}, answered by ${a.node_id}`, a.node_id);
      }
      if (a.key_id !== input.keyId || a.kind !== kind) {
        throw new AttestorSessionError('request_mismatch', `participant answered for ${a.key_id} ${a.kind}`, a.node_id);
      }
      if (a.signed_bytes !== first.signed_bytes) {
        throw new AttestorSessionError('signed_bytes_disagreement', 'participants signed different bytes', a.node_id);
      }
      if (a.signature !== first.signature || a.recovery_id !== first.recovery_id) {
        throw new AttestorSessionError('signature_disagreement', 'participants returned different signatures', a.node_id);
      }
    }
    return {
      sessionId,
      kind,
      signature: `0x${first.signature}`,
      recoveryId: first.recovery_id ?? null,
      signedBytes: `0x${first.signed_bytes}`,
      participants,
      audit: answers.map((a) => ({ node_id: a.node_id, audit_sequence: a.audit_sequence })),
    };
  }

  async generateKey(input: GenerateKeyInput): Promise<KeyResult> {
    const sessionId = randomUUID();
    const wire: KeyGenerateWire = { session_id: sessionId, key_id: input.keyId, curve: input.curve, owner: input.owner };
    if (input.account !== undefined) wire.account = input.account;
    return this.keyOperation(ATTESTOR_PATHS.generate, sessionId, wire);
  }

  async refreshKey(keyId: string): Promise<KeyResult> {
    const sessionId = randomUUID();
    const wire: KeyRefreshWire = { session_id: sessionId, key_id: keyId };
    return this.keyOperation(ATTESTOR_PATHS.refresh, sessionId, wire);
  }

  private async keyOperation(path: string, sessionId: string, wire: KeyGenerateWire | KeyRefreshWire): Promise<KeyResult> {
    const members: QuorumMember[] = this.nodes.map((n) => {
      if (n.nodeId === null || !n.healthy || n.latencyMs === null) {
        throw new AttestorQuorumError('participant_unavailable', `${n.endpoint} is not ready for a key operation`);
      }
      return { endpoint: n.endpoint, nodeId: n.nodeId, latencyMs: n.latencyMs };
    });
    const body = JSON.stringify(wire);
    const settled = await Promise.allSettled(
      members.map((m) => this.post(m, path, body, {}, (b) => parseKeyResponse(b, m.nodeId))),
    );
    const refusal = firstRefusal(settled, members, (e, r) => this.markDown(e, r));
    if (refusal) throw refusal;
    const answers = settled.map((s) => (s as PromiseFulfilledResult<KeyResponseWire>).value);
    const first = answers[0]!;
    for (let i = 0; i < answers.length; i++) {
      const a = answers[i]!;
      if (a.node_id !== members[i]!.nodeId) {
        throw new AttestorSessionError('participant_mismatch', `expected ${members[i]!.nodeId}, answered by ${a.node_id}`, a.node_id);
      }
      if (
        a.key_id !== wire.key_id ||
        a.curve !== first.curve ||
        a.public_key !== first.public_key ||
        a.address !== first.address ||
        a.did !== first.did ||
        a.epoch !== first.epoch ||
        a.refreshed !== first.refreshed
      ) {
        throw new AttestorSessionError('key_disagreement', 'participants disagree on the key', a.node_id);
      }
    }
    return {
      sessionId,
      keyId: first.key_id,
      curve: first.curve,
      publicKey: `0x${first.public_key}`,
      address: (first.address as `0x${string}` | undefined) ?? null,
      did: first.did ?? null,
      epoch: first.epoch,
      participants: first.participants,
      refreshed: first.refreshed,
      audit: answers.map((a) => ({ node_id: a.node_id, audit_sequence: a.audit_sequence })),
    };
  }

  private async probe(node: NodeHealth): Promise<void> {
    const started = performance.now();
    try {
      const res = await this.http('GET', `${node.endpoint}${ATTESTOR_PATHS.health}`, null, {});
      const latency = performance.now() - started;
      if (res.status !== 200 && res.status !== 503) throw new Error(`health answered ${res.status}`);
      const report = parseHealth(res.body);
      if ((res.status === 200) !== report.ready) {
        throw new Error(`health answered ${res.status} with ready ${String(report.ready)}`);
      }
      node.nodeId = report.node_id;
      node.report = report;
      node.latencyMs = latency;
      node.healthy = report.ready;
      node.lastError = report.ready ? null : (report.readiness_error ?? report.share_error ?? 'not ready');
    } catch (err) {
      node.healthy = false;
      node.latencyMs = null;
      node.lastError = err instanceof Error ? err.message : String(err);
    } finally {
      node.checkedAt = Date.now();
    }
  }

  private markDown(endpoint: string, reason: unknown): void {
    const node = this.nodes.find((n) => n.endpoint === endpoint);
    if (!node) return;
    node.healthy = false;
    node.lastError = reason instanceof Error ? reason.message : String(reason);
  }

  private async post<T>(
    member: QuorumMember,
    path: string,
    body: string,
    headers: Record<string, string>,
    parse: (body: unknown) => T,
  ): Promise<T> {
    const res = await this.http('POST', `${member.endpoint}${path}`, body, headers);
    if (res.status !== 200) throw refusalFromWire(res.body, member.nodeId);
    return parse(res.body);
  }

  private http(method: 'GET' | 'POST', url: string, body: string | null, extra: Record<string, string>): Promise<HttpResult> {
    return new Promise((resolve, reject) => {
      const req = httpsRequest(
        url,
        {
          method,
          agent: this.agent,
          timeout: this.opts.timeoutMs,
          headers: body
            ? { ...extra, 'content-type': 'application/json', 'content-length': Buffer.byteLength(body) }
            : { ...extra, accept: 'application/json' },
        },
        (res) => {
          const chunks: Buffer[] = [];
          res.on('data', (c: Buffer) => chunks.push(c));
          res.on('end', () => {
            const text = Buffer.concat(chunks).toString('utf8');
            let parsed: unknown = null;
            if (text.length > 0) {
              try {
                parsed = JSON.parse(text);
              } catch {
                reject(new Error(`attestor answered non-JSON with status ${res.statusCode}`));
                return;
              }
            }
            resolve({ status: res.statusCode ?? 0, body: parsed });
          });
          res.on('error', reject);
        },
      );
      req.on('timeout', () => req.destroy(new Error(`attestor request timed out after ${this.opts.timeoutMs} ms`)));
      req.on('error', reject);
      if (body) req.write(body);
      req.end();
    });
  }
}

function isUint(v: unknown): v is number {
  return typeof v === 'number' && Number.isInteger(v) && v >= 0;
}

function parseHealth(body: unknown): AttestorHealthReport {
  const b = body as Partial<AttestorHealthReport> | null;
  if (
    !b ||
    typeof b.node_id !== 'string' ||
    b.node_id.length === 0 ||
    typeof b.region !== 'string' ||
    !isUint(b.share_count) ||
    !isUint(b.refresh_epoch) ||
    !isUint(b.audit_sequence) ||
    typeof b.audit_head !== 'string' ||
    typeof b.peers !== 'object' ||
    b.peers === null ||
    !isUint(b.reachable_peers) ||
    typeof b.ready !== 'boolean' ||
    (b.readiness_error !== undefined && typeof b.readiness_error !== 'string') ||
    (b.share_error !== undefined && typeof b.share_error !== 'string')
  ) {
    throw new Error('health answer does not match the attestor schema');
  }
  return b as AttestorHealthReport;
}

function parseSignResponse(body: unknown, nodeId: string, curve: Curve): SignResponseWire {
  const b = body as Partial<SignResponseWire> | null;
  if (
    !b ||
    typeof b.node_id !== 'string' ||
    typeof b.key_id !== 'string' ||
    typeof b.kind !== 'string' ||
    typeof b.signed_bytes !== 'string' ||
    !HEX_RE.test(b.signed_bytes) ||
    typeof b.signature !== 'string' ||
    !HEX_RE.test(b.signature) ||
    !isUint(b.audit_sequence)
  ) {
    throw new AttestorSessionError('malformed_response', 'sign answer does not match the attestor schema', nodeId);
  }
  const length = b.signature.length / 2;
  if (length !== SIGNATURE_BYTES[curve]) {
    throw new AttestorSessionError(
      'bad_signature_length',
      `expected ${SIGNATURE_BYTES[curve]} ${curve} signature bytes, got ${length}`,
      nodeId,
    );
  }
  if (curve === 'secp256k1') {
    const v = Number.parseInt(b.signature.slice(128, 130), 16);
    if (!(b.recovery_id === 0 || b.recovery_id === 1) || v !== b.recovery_id) {
      throw new AttestorSessionError('bad_recovery_id', 'secp256k1 signature without a matching recovery id', nodeId);
    }
  } else if (b.recovery_id !== undefined) {
    throw new AttestorSessionError('bad_recovery_id', 'ed25519 signature carries a recovery id', nodeId);
  }
  return b as SignResponseWire;
}

function parseKeyResponse(body: unknown, nodeId: string): KeyResponseWire {
  const b = body as Partial<KeyResponseWire> | null;
  if (
    !b ||
    typeof b.node_id !== 'string' ||
    typeof b.key_id !== 'string' ||
    (b.curve !== 'secp256k1' && b.curve !== 'ed25519') ||
    typeof b.public_key !== 'string' ||
    !HEX_RE.test(b.public_key) ||
    b.public_key.length !== (b.curve === 'secp256k1' ? 130 : 64) ||
    (b.address !== undefined && (typeof b.address !== 'string' || !/^0x[0-9a-fA-F]{40}$/.test(b.address))) ||
    (b.did !== undefined && typeof b.did !== 'string') ||
    !isUint(b.epoch) ||
    !Array.isArray(b.participants) ||
    !b.participants.every((p) => typeof p === 'string') ||
    typeof b.refreshed !== 'boolean' ||
    !isUint(b.audit_sequence)
  ) {
    throw new AttestorSessionError('malformed_response', 'key answer does not match the attestor schema', nodeId);
  }
  return b as KeyResponseWire;
}

export function attestorClientFromConfig(cfg: {
  ATTESTOR_ENDPOINTS: string[];
  ATTESTOR_CLIENT_CERT_FILE?: string;
  ATTESTOR_CLIENT_KEY_FILE?: string;
  ATTESTOR_CA_FILE?: string;
  ATTESTOR_QUORUM: number;
  ATTESTOR_HEALTH_INTERVAL_MS: number;
  ATTESTOR_TIMEOUT_MS: number;
}): AttestorClient | null {
  if (cfg.ATTESTOR_ENDPOINTS.length === 0) return null;
  if (!cfg.ATTESTOR_CLIENT_CERT_FILE || !cfg.ATTESTOR_CLIENT_KEY_FILE || !cfg.ATTESTOR_CA_FILE) {
    throw new Error('attestor client certificate, key and CA files are required with ATTESTOR_ENDPOINTS');
  }
  return new AttestorClient({
    endpoints: cfg.ATTESTOR_ENDPOINTS,
    tls: {
      cert: readFileSync(cfg.ATTESTOR_CLIENT_CERT_FILE),
      key: readFileSync(cfg.ATTESTOR_CLIENT_KEY_FILE),
      ca: readFileSync(cfg.ATTESTOR_CA_FILE),
    },
    quorum: cfg.ATTESTOR_QUORUM,
    healthIntervalMs: cfg.ATTESTOR_HEALTH_INTERVAL_MS,
    timeoutMs: cfg.ATTESTOR_TIMEOUT_MS,
  });
}

export async function signThroughAttestors(client: AttestorClient, input: SignInput): Promise<SignResult> {
  return client.sign(input);
}

function splitSignature(result: SignResult): { r: Hex; s: Hex; yParity: number } {
  const bytes = hexToBytes(result.signature);
  if (bytes.length !== SIGNATURE_BYTES.secp256k1) {
    throw new AttestorSessionError('bad_signature_length', `expected 65 signature bytes, got ${bytes.length}`);
  }
  const yParity = bytes[64]!;
  if ((yParity !== 0 && yParity !== 1) || result.recoveryId !== yParity) {
    throw new AttestorSessionError('bad_recovery_id', 'secp256k1 signature without a matching recovery id');
  }
  return {
    r: toHex(bytes.subarray(0, 32)),
    s: toHex(bytes.subarray(32, 64)),
    yParity,
  };
}

const ZERO_WORD: Hex = `0x${'00'.repeat(32)}`;

export function unsignedTransactionBytes(tx: TransactionSerializable): Hex {
  if (getTransactionType(tx) === 'legacy') return serializeTransaction(tx);
  return serializeTransaction(tx, { r: ZERO_WORD, s: ZERO_WORD, yParity: 0 });
}

export function encodeTypedData(td: TypedDataDefinition): string {
  const domain = (td.domain ?? {}) as Record<string, unknown>;
  const types = td.types as Record<string, unknown>;
  const withDomain = 'EIP712Domain' in types
    ? types
    : { EIP712Domain: getTypesForEIP712Domain({ domain: td.domain as never }), ...types };
  return JSON.stringify(
    { domain, types: withDomain, primaryType: td.primaryType, message: td.message },
    (_k, v: unknown) => (typeof v === 'bigint' ? v.toString() : v),
  );
}

export interface AttestorSigning<T> {
  value: T;
  result: SignResult;
}

export interface AttestorSigner {
  address: `0x${string}`;
  signTransaction(tx: TransactionSerializable): Promise<AttestorSigning<Hex>>;
  signMessage(message: string): Promise<AttestorSigning<Hex>>;
  signTypedData(td: TypedDataDefinition): Promise<AttestorSigning<Hex>>;
}

export function attestorSigner(
  client: AttestorClient,
  wallet: { keyId: string; address: `0x${string}`; chainId: number },
  authorisation: Authorisation,
): AttestorSigner {
  const expectSigner = (recovered: string, sessionId: string): void => {
    if (recovered.toLowerCase() !== wallet.address.toLowerCase()) {
      throw new AttestorSessionError(
        'signer_mismatch',
        `session ${sessionId} produced a signature for ${recovered}, not the wallet address`,
      );
    }
  };
  return {
    address: wallet.address,
    async signTransaction(tx) {
      const result = await client.sign({
        keyId: wallet.keyId,
        payload: { kind: 'evm_tx', transaction: unsignedTransactionBytes(tx) },
        authorisation,
      });
      const signed = serializeTransaction(tx, splitSignature(result));
      expectSigner(await recoverTransactionAddress({ serializedTransaction: signed as never }), result.sessionId);
      return { value: signed, result };
    },
    async signMessage(message) {
      const result = await client.sign({
        keyId: wallet.keyId,
        payload: { kind: 'personal_message', message: stringToHex(message) },
        authorisation,
      });
      const signature = serializeSignature(splitSignature(result));
      expectSigner(await recoverMessageAddress({ message, signature }), result.sessionId);
      return { value: signature, result };
    },
    async signTypedData(td) {
      const result = await client.sign({
        keyId: wallet.keyId,
        payload: { kind: 'eip712', typedData: encodeTypedData(td) },
        authorisation,
      });
      const signature = serializeSignature(splitSignature(result));
      expectSigner(await recoverTypedDataAddress({ ...td, signature } as never), result.sessionId);
      return { value: signature, result };
    },
  };
}
