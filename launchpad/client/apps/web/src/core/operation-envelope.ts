/**
 * Signed operation envelope (shared/auth/operation.go `OperationEnvelope`).
 *
 * Every owner write that a production service authorizes through
 * `sharedauth.OperationAuthorizer` carries two headers:
 *   - the envelope header: base64url (no padding) of the envelope JSON;
 *   - the signature header: the wallet's EIP-191 personal_sign signature over
 *     `Statement()` = "KindleLaunch signed operation v<N>\n" + json.Marshal(envelope).
 *
 * The server never signs the bytes it received: it decodes the header,
 * re-marshals the struct with Go's encoding/json and verifies the signature
 * over those bytes. The client therefore has to produce exactly what
 * json.Marshal produces: struct field order, no whitespace, and Go's HTML-safe
 * string escaping (<, >, & and U+2028/U+2029 as \u escapes).
 *
 * This module is the one signer for media/user writes (PATCH /v3/profiles,
 * PATCH /v3/me/preferences, PATCH /v1/profiles, PUT/DELETE /v1/me/watchlist)
 * and for the livestream lifecycle writes, which use the same envelope with
 * their own signature header (`X-Kindle-Signature`).
 */

export const OPERATION_ENVELOPE_HEADER = 'X-Kindle-Operation';
export const OPERATION_SIGNATURE_HEADER = 'X-Kindle-Operation-Signature';
/** media/livestream reads the signature from this header (media/livestream/internal/streams/connected_routes.go). */
export const LIVESTREAM_SIGNATURE_HEADER = 'X-Kindle-Signature';
export const OPERATION_CHAIN_ID = '125';
/** Must equal the service's operation environment (media/user ACCOUNT_OPERATION_ENVIRONMENT, default "production"). */
export const OPERATION_ENVIRONMENT = process.env.NEXT_PUBLIC_ACTION_ENVIRONMENT || 'production';
/** shared/auth/operation.go rejects expiresAt - issuedAt > 300. */
export const OPERATION_MAX_LIFETIME_SECONDS = 300;

export type OperationMethod = 'POST' | 'PUT' | 'PATCH' | 'DELETE';
export type OperationVersion = 1 | 2;

/** Field order is the Go struct order; canonicalOperationJSON relies on it. */
export interface OperationEnvelope {
  version: OperationVersion;
  chainId: string;
  environment: string;
  actor: string;
  action: string;
  method: OperationMethod;
  path: string;
  bodyHash: string;
  nonce: string;
  issuedAt: number;
  expiresAt: number;
}

const TOKEN = /^[a-zA-Z0-9_.:-]{1,128}$/;
const DIGEST = /^[0-9a-f]{64}$/;
const WALLET = /^0x[0-9a-f]{40}$/;
const METHODS: readonly string[] = ['POST', 'PUT', 'PATCH', 'DELETE'];

export class OperationEnvelopeError extends Error {
  constructor(message: string) { super(message); this.name = 'OperationEnvelopeError'; }
}

/** encoding/json string encoding with HTML escaping (Go's json.Marshal default). */
function goJSONString(value: string): string {
  let out = '"';
  for (let index = 0; index < value.length; index++) {
    const code = value.charCodeAt(index);
    const character = value[index]!;
    if (code === 0x22) out += '\\"';
    else if (code === 0x5c) out += '\\\\';
    else if (code === 0x0a) out += '\\n';
    else if (code === 0x0d) out += '\\r';
    else if (code === 0x09) out += '\\t';
    else if (code < 0x20 || code === 0x3c || code === 0x3e || code === 0x26 || code === 0x2028 || code === 0x2029) out += `\\u${code.toString(16).padStart(4, '0')}`;
    else if (code >= 0xd800 && code <= 0xdfff) {
      // Go replaces invalid UTF-8 with U+FFFD; a well-formed pair passes through as UTF-8.
      const next = value.charCodeAt(index + 1);
      if (code <= 0xdbff && next >= 0xdc00 && next <= 0xdfff) { out += character + value[index + 1]; index++; }
      else out += '�';
    } else out += character;
  }
  return `${out}"`;
}

/** The exact bytes json.Marshal(OperationEnvelope) yields for this envelope. */
export function canonicalOperationJSON(envelope: OperationEnvelope): string {
  const integer = (value: number) => {
    if (!Number.isSafeInteger(value)) throw new OperationEnvelopeError('Operation timestamps must be integers');
    return String(value);
  };
  return `{"version":${integer(envelope.version)},"chainId":${goJSONString(envelope.chainId)},"environment":${goJSONString(envelope.environment)},"actor":${goJSONString(envelope.actor)},"action":${goJSONString(envelope.action)},"method":${goJSONString(envelope.method)},"path":${goJSONString(envelope.path)},"bodyHash":${goJSONString(envelope.bodyHash)},"nonce":${goJSONString(envelope.nonce)},"issuedAt":${integer(envelope.issuedAt)},"expiresAt":${integer(envelope.expiresAt)}}`;
}

/**
 * path.Clean(p) === p, and url.ParseRequestURI leaves RawPath empty: only the
 * bytes Go's encodePath never escapes (unreserved plus $&+,/:;=@) may appear.
 */
function canonicalPath(path: string): boolean {
  if (!path.startsWith('/') || !/^[A-Za-z0-9\-_.~$&+,/:;=@]+$/.test(path)) return false;
  if (path === '/') return true;
  if (path.endsWith('/')) return false;
  return path.slice(1).split('/').every(segment => segment !== '' && segment !== '.' && segment !== '..');
}

/** Mirrors OperationEnvelope.Statement(): rejects what the server would reject, then returns the signed message. */
export function operationStatement(envelope: OperationEnvelope): string {
  if (!WALLET.test(envelope.actor) || /^0x0{40}$/.test(envelope.actor)) throw new OperationEnvelopeError('Operation actor must be a normalized wallet');
  if (!canonicalPath(envelope.path)) throw new OperationEnvelopeError('Operation path must be canonical');
  if ((envelope.version !== 1 && envelope.version !== 2) || envelope.chainId !== OPERATION_CHAIN_ID || !TOKEN.test(envelope.environment) || !TOKEN.test(envelope.action) || !METHODS.includes(envelope.method)) throw new OperationEnvelopeError('Invalid operation envelope');
  if (!DIGEST.test(envelope.nonce) || !DIGEST.test(envelope.bodyHash)) throw new OperationEnvelopeError('Invalid operation digest or nonce');
  if (!Number.isSafeInteger(envelope.issuedAt) || !Number.isSafeInteger(envelope.expiresAt) || envelope.issuedAt < 0 || envelope.expiresAt <= envelope.issuedAt || envelope.expiresAt - envelope.issuedAt > OPERATION_MAX_LIFETIME_SECONDS) throw new OperationEnvelopeError('Invalid operation lifetime');
  return `KindleLaunch signed operation v${envelope.version}\n${canonicalOperationJSON(envelope)}`;
}

const hex = (bytes: Uint8Array) => Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('');
const utf8 = (value: string | Uint8Array) => typeof value === 'string' ? new TextEncoder().encode(value) : value;

/** sha256 hex of the exact request body bytes (BodyDigestV2: the empty body hashes too). */
export async function operationBodyHash(body: string | Uint8Array): Promise<string> {
  const bytes = utf8(body);
  return hex(new Uint8Array(await crypto.subtle.digest('SHA-256', bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer)));
}

/** base64url without padding (base64.RawURLEncoding) of the canonical envelope bytes. */
export function encodeOperationEnvelope(envelope: OperationEnvelope): string {
  let binary = '';
  for (const byte of new TextEncoder().encode(canonicalOperationJSON(envelope))) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

export interface OperationRequest {
  /** Wallet that owns the session and signs; normalized to lowercase. */
  actor: string;
  action: string;
  method: OperationMethod;
  /** Request path exactly as the service sees it (no query). */
  path: string;
  /** The exact body bytes sent; '' for a bodyless DELETE. */
  body: string;
  version?: OperationVersion;
  environment?: string;
  lifetimeSeconds?: number;
}

/** Builds and validates an envelope for an explicit nonce and issue time. */
export async function buildOperationEnvelope(request: OperationRequest, nonce: string, issuedAt: number): Promise<OperationEnvelope> {
  const version = request.version ?? 2;
  if (version === 2 && request.body.length === 0 && request.method !== 'DELETE') throw new OperationEnvelopeError('A v2 operation needs a body unless it is a DELETE');
  if (version === 1 && request.body.length === 0) throw new OperationEnvelopeError('A v1 operation needs a JSON body');
  const lifetime = request.lifetimeSeconds ?? OPERATION_MAX_LIFETIME_SECONDS;
  const envelope: OperationEnvelope = {
    version, chainId: OPERATION_CHAIN_ID, environment: request.environment ?? OPERATION_ENVIRONMENT,
    actor: request.actor.toLowerCase(), action: request.action, method: request.method, path: request.path,
    bodyHash: await operationBodyHash(request.body), nonce, issuedAt, expiresAt: issuedAt + lifetime,
  };
  operationStatement(envelope);
  return envelope;
}

export interface OperationSigner {
  /** EIP-191 personal_sign over the UTF-8 message by the actor wallet. */
  signMessage(message: string): Promise<string>;
  /** Throws when the account or session changed while the wallet prompt was open. */
  assertCurrent?(): void;
}

export interface SignedOperation { envelope: OperationEnvelope; statement: string; encoded: string; signature: string }

/** Signs a fresh envelope (random 32-byte nonce, now, lifetime <= 300 s). */
export async function signOperation(request: OperationRequest, signer: OperationSigner): Promise<SignedOperation> {
  const nonce = hex(crypto.getRandomValues(new Uint8Array(32)));
  const envelope = await buildOperationEnvelope(request, nonce, Math.floor(Date.now() / 1000));
  const statement = operationStatement(envelope);
  signer.assertCurrent?.();
  const signature = await signer.signMessage(statement);
  signer.assertCurrent?.();
  if (!/^0x(?:[0-9a-fA-F]{2}){1,4096}$/.test(signature)) throw new OperationEnvelopeError('The wallet returned an invalid signature');
  if (Math.floor(Date.now() / 1000) >= envelope.expiresAt) throw new OperationEnvelopeError('Took too long to send, try again');
  return { envelope, statement, encoded: encodeOperationEnvelope(envelope), signature };
}

/** Request headers for a signed operation. */
export function operationHeaders(signed: SignedOperation, signatureHeader: string = OPERATION_SIGNATURE_HEADER): Record<string, string> {
  return { [OPERATION_ENVELOPE_HEADER]: signed.encoded, [signatureHeader]: signed.signature };
}
