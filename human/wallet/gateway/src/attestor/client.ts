import { Agent, request as httpsRequest } from 'node:https';
import { randomUUID } from 'node:crypto';
import { readFileSync } from 'node:fs';
import {
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

export type SignKind =
  | 'evm_tx'
  | 'eip712'
  | 'personal_message'
  | 'eth_sign_digest'
  | 'lx_activity'
  | 'lx_bind'
  | 'lx_grant';

export type Authorisation =
  | { scheme: 'supabase_jwt'; token: string }
  | { scheme: 'agent_ed25519'; did: string; nonce: string; expiry: number; signature: string };

export interface SignRequestWire {
  api_version: number;
  key_id: string;
  kind: SignKind;
  bytes: Hex;
  context: Record<string, unknown>;
  authorisation: Authorisation;
  participants: string[];
  session_id: string;
}

export interface SignResponseWire {
  session_id: string;
  node_id: string;
  signature: Hex;
  recovery_id: number | null;
  audit_sequence: number;
}

export type RefusalCategory = 'token' | 'policy' | 'quorum' | 'session';

export interface RefusalWire {
  error: { category: RefusalCategory; code: string; reason: string };
}

export class AttestorError extends Error {
  readonly category: RefusalCategory;
  readonly code: string;
  readonly nodeId: string | null;
  constructor(category: RefusalCategory, code: string, reason: string, nodeId: string | null = null) {
    super(`${category}: ${code}: ${reason}`);
    this.name = 'AttestorError';
    this.category = category;
    this.code = code;
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
  constructor(code: string, reason: string, nodeId: string | null = null) {
    super('policy', code, reason, nodeId);
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

const REFUSAL_STATUS: Record<RefusalCategory, number> = {
  token: 401,
  policy: 403,
  quorum: 503,
  session: 502,
};

export function attestorErrorStatus(err: AttestorError): number {
  return REFUSAL_STATUS[err.category];
}

export function attestorErrorBody(err: AttestorError): {
  error: string;
  category: RefusalCategory;
  code: string;
  message: string;
} {
  return {
    error: `attestor_${err.category}_refused`,
    category: err.category,
    code: err.code,
    message: err.message,
  };
}

function refusalFromWire(body: unknown, nodeId: string | null): AttestorError {
  const e = (body as Partial<RefusalWire> | null)?.error;
  if (!e || typeof e.code !== 'string' || typeof e.reason !== 'string') {
    return new AttestorSessionError('malformed_refusal', 'participant answered with an unreadable error', nodeId);
  }
  switch (e.category) {
    case 'token':
      return new AttestorTokenError(e.code, e.reason, nodeId);
    case 'policy':
      return new AttestorPolicyError(e.code, e.reason, nodeId);
    case 'quorum':
      return new AttestorQuorumError(e.code, e.reason, nodeId);
    case 'session':
      return new AttestorSessionError(e.code, e.reason, nodeId);
    default:
      return new AttestorSessionError('unknown_refusal_category', `category ${String(e.category)}: ${e.reason}`, nodeId);
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
  kind: SignKind;
  bytes: Hex;
  context: Record<string, unknown>;
  authorisation: Authorisation;
}

export interface NodeAudit {
  node_id: string;
  audit_sequence: number;
}

export interface SignResult {
  sessionId: string;
  signature: Hex;
  recoveryId: number | null;
  participants: string[];
  audit: NodeAudit[];
}

interface HttpResult {
  status: number;
  body: unknown;
}

const REFUSAL_PRIORITY: Record<RefusalCategory, number> = { token: 0, policy: 1, quorum: 2, session: 3 };

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
    const sessionId = randomUUID();
    const participants = members.map((m) => m.nodeId);
    const wire: SignRequestWire = {
      api_version: ATTESTOR_API_VERSION,
      key_id: input.keyId,
      kind: input.kind,
      bytes: input.bytes,
      context: input.context,
      authorisation: input.authorisation,
      participants,
      session_id: sessionId,
    };
    const body = JSON.stringify(wire);
    const settled = await Promise.allSettled(
      members.map((m) => this.postSign(m, body)),
    );

    const refusals: AttestorError[] = [];
    const answers: SignResponseWire[] = [];
    settled.forEach((s, i) => {
      if (s.status === 'fulfilled') {
        answers.push(s.value);
        return;
      }
      const member = members[i]!;
      if (s.reason instanceof AttestorError) {
        refusals.push(s.reason);
      } else {
        this.markDown(member.endpoint, s.reason);
        refusals.push(
          new AttestorSessionError(
            'participant_unreachable',
            s.reason instanceof Error ? s.reason.message : String(s.reason),
            member.nodeId,
          ),
        );
      }
    });
    if (refusals.length > 0) {
      refusals.sort((a, b) => REFUSAL_PRIORITY[a.category] - REFUSAL_PRIORITY[b.category]);
      throw refusals[0]!;
    }

    const first = answers[0]!;
    for (let i = 0; i < answers.length; i++) {
      const a = answers[i]!;
      const expectedNode = members[i]!.nodeId;
      if (a.session_id !== sessionId) {
        throw new AttestorSessionError('session_mismatch', `participant answered for session ${a.session_id}`, a.node_id);
      }
      if (a.node_id !== expectedNode) {
        throw new AttestorSessionError('participant_mismatch', `expected ${expectedNode}, answered by ${a.node_id}`, a.node_id);
      }
      if (a.signature.toLowerCase() !== first.signature.toLowerCase() || a.recovery_id !== first.recovery_id) {
        throw new AttestorSessionError('signature_disagreement', 'participants returned different signatures', a.node_id);
      }
    }
    return {
      sessionId,
      signature: first.signature,
      recoveryId: first.recovery_id,
      participants,
      audit: answers.map((a) => ({ node_id: a.node_id, audit_sequence: a.audit_sequence })),
    };
  }

  private async probe(node: NodeHealth): Promise<void> {
    const started = performance.now();
    try {
      const res = await this.http('GET', `${node.endpoint}/v1/health`, null);
      const latency = performance.now() - started;
      if (res.status !== 200) throw new Error(`health answered ${res.status}`);
      const report = parseHealth(res.body);
      node.nodeId = report.node_id;
      node.report = report;
      node.latencyMs = latency;
      node.healthy = report.ready;
      node.lastError = report.ready ? null : (report.readiness_error ?? 'not ready');
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

  private async postSign(member: QuorumMember, body: string): Promise<SignResponseWire> {
    const res = await this.http('POST', `${member.endpoint}/v1/sign`, body);
    if (res.status !== 200) throw refusalFromWire(res.body, member.nodeId);
    return parseSignResponse(res.body, member.nodeId);
  }

  private http(method: 'GET' | 'POST', url: string, body: string | null): Promise<HttpResult> {
    return new Promise((resolve, reject) => {
      const req = httpsRequest(
        url,
        {
          method,
          agent: this.agent,
          timeout: this.opts.timeoutMs,
          headers: body
            ? { 'content-type': 'application/json', 'content-length': Buffer.byteLength(body) }
            : { accept: 'application/json' },
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

function parseHealth(body: unknown): AttestorHealthReport {
  const b = body as Partial<AttestorHealthReport> | null;
  if (
    !b ||
    typeof b.node_id !== 'string' ||
    b.node_id.length === 0 ||
    typeof b.ready !== 'boolean' ||
    typeof b.reachable_peers !== 'number' ||
    typeof b.audit_sequence !== 'number'
  ) {
    throw new Error('health answer does not match the attestor schema');
  }
  return b as AttestorHealthReport;
}

function parseSignResponse(body: unknown, nodeId: string): SignResponseWire {
  const b = body as Partial<SignResponseWire> | null;
  if (
    !b ||
    typeof b.session_id !== 'string' ||
    typeof b.node_id !== 'string' ||
    typeof b.signature !== 'string' ||
    !/^0x[0-9a-fA-F]+$/.test(b.signature) ||
    typeof b.audit_sequence !== 'number' ||
    !(b.recovery_id === null || b.recovery_id === 0 || b.recovery_id === 1)
  ) {
    throw new AttestorSessionError('malformed_response', 'sign answer does not match the attestor schema', nodeId);
  }
  return b as SignResponseWire;
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
  if (bytes.length !== 64) {
    throw new AttestorSessionError('bad_signature_length', `expected 64 signature bytes, got ${bytes.length}`);
  }
  if (result.recoveryId !== 0 && result.recoveryId !== 1) {
    throw new AttestorSessionError('missing_recovery_id', 'secp256k1 signature without a recovery id');
  }
  return {
    r: toHex(bytes.subarray(0, 32)),
    s: toHex(bytes.subarray(32, 64)),
    yParity: result.recoveryId,
  };
}

export function encodeTypedData(td: TypedDataDefinition): Hex {
  const json = JSON.stringify(
    { domain: td.domain ?? {}, types: td.types, primaryType: td.primaryType, message: td.message },
    (_k, v: unknown) => (typeof v === 'bigint' ? v.toString() : v),
  );
  return stringToHex(json);
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
  const context = { chain_id: wallet.chainId, address: wallet.address.toLowerCase() };
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
      const unsigned = serializeTransaction(tx);
      const result = await client.sign({
        keyId: wallet.keyId,
        kind: 'evm_tx',
        bytes: unsigned,
        context,
        authorisation,
      });
      const signed = serializeTransaction(tx, splitSignature(result));
      expectSigner(await recoverTransactionAddress({ serializedTransaction: signed as never }), result.sessionId);
      return { value: signed, result };
    },
    async signMessage(message) {
      const result = await client.sign({
        keyId: wallet.keyId,
        kind: 'personal_message',
        bytes: stringToHex(message),
        context,
        authorisation,
      });
      const signature = serializeSignature(splitSignature(result));
      expectSigner(await recoverMessageAddress({ message, signature }), result.sessionId);
      return { value: signature, result };
    },
    async signTypedData(td) {
      const result = await client.sign({
        keyId: wallet.keyId,
        kind: 'eip712',
        bytes: encodeTypedData(td),
        context,
        authorisation,
      });
      const signature = serializeSignature(splitSignature(result));
      expectSigner(await recoverTypedDataAddress({ ...td, signature } as never), result.sessionId);
      return { value: signature, result };
    },
  };
}
