import { ed25519 } from '@noble/curves/ed25519';
import { sha256 } from '@noble/hashes/sha2';
import { gatewayRefusal, InvalidParamsError } from './errors.js';

export const AGENT_DID_PREFIX = 'did:layerx:';
export const AGENT_REQUEST_DOMAIN = 'PXW:AGENT-REQUEST:v1';
export const AGENT_CLAIM_DOMAIN = 'PXW:AGENT-CLAIM:v1';
export const AGENT_BIND_DOMAIN = 'LX:PAXEER-BIND:v1';
export const AGENT_BIND_MESSAGE_LENGTH = AGENT_BIND_DOMAIN.length + 32 + 20 + 8;
export const AGENT_REQUEST_DEFAULT_TTL_SECONDS = 60;

export const AGENT_REQUEST_HEADERS = {
  key: 'x-agent-key',
  nonce: 'x-agent-nonce',
  expires: 'x-agent-expires',
  signature: 'x-agent-signature',
} as const;

export interface AgentRequestHeaders {
  'x-agent-key': string;
  'x-agent-nonce': string;
  'x-agent-expires': string;
  'x-agent-signature': string;
}

export type AgentBody = string | Uint8Array;

export interface AgentRequestDigestInput {
  method: string;
  keyId: string;
  nonce: Uint8Array;
  expiry: bigint;
  body: AgentBody;
}

export interface SignAgentRequestInput {
  privateKey: Uint8Array;
  keyId: string;
  method: string;
  url: string;
  body?: AgentBody;
  nonce?: Uint8Array;
  expiresAt?: bigint | number;
  nowSeconds?: number;
}

export interface SignBindingInput {
  privateKey: Uint8Array;
  chainId: bigint | number;
  evmAddress: string | Uint8Array;
  nonce: bigint | number;
}

export interface SignedBinding {
  message: Uint8Array;
  signature: Uint8Array;
  publicKey: Uint8Array;
  did: string;
}

export interface BuildClaimInput {
  privateKey: Uint8Array;
  did: string;
  ownerUserId: string;
  expiresAt?: bigint | number;
  nowSeconds?: number;
}

export interface AgentClaim {
  expires: string;
  signature: string;
}

export interface AgentClaimResult {
  did: string;
  owner_user_id: string;
}

const U32_MAX = 0xffff_ffff;
const U64_MAX = (1n << 64n) - 1n;
const encoder = new TextEncoder();

function toHexString(bytes: Uint8Array): string {
  let out = '';
  for (const b of bytes) out += b.toString(16).padStart(2, '0');
  return out;
}

function concatBytes(parts: Uint8Array[]): Uint8Array {
  const total = parts.reduce((n, p) => n + p.length, 0);
  const out = new Uint8Array(total);
  let offset = 0;
  for (const part of parts) {
    out.set(part, offset);
    offset += part.length;
  }
  return out;
}

function u32be(n: number): Uint8Array {
  const out = new Uint8Array(4);
  new DataView(out.buffer).setUint32(0, n);
  return out;
}

function u64be(n: bigint): Uint8Array {
  const out = new Uint8Array(8);
  new DataView(out.buffer).setBigUint64(0, n);
  return out;
}

function u256be(n: bigint): Uint8Array {
  const out = new Uint8Array(32);
  new DataView(out.buffer).setBigUint64(24, n);
  return out;
}

function lengthPrefixed(value: string): Uint8Array {
  const bytes = encoder.encode(value);
  if (bytes.length > U32_MAX) throw new InvalidParamsError('value', 'field exceeds length limit');
  return concatBytes([u32be(bytes.length), bytes]);
}

function bodyBytes(body: AgentBody | undefined): Uint8Array {
  if (body === undefined) return new Uint8Array(0);
  return typeof body === 'string' ? encoder.encode(body) : body;
}

function checkPrivateKey(privateKey: Uint8Array): void {
  if (!(privateKey instanceof Uint8Array) || privateKey.length !== 32) {
    throw new InvalidParamsError('privateKey', 'the agent private key must be a 32-byte Ed25519 seed');
  }
}

function checkU64(field: string, value: bigint | number): bigint {
  if (typeof value === 'number' && !Number.isSafeInteger(value)) {
    throw new InvalidParamsError(field, `${field} must be an integer`);
  }
  const v = BigInt(value);
  if (v < 0n || v > U64_MAX) throw new InvalidParamsError(field, `${field} must fit an unsigned 64-bit integer`);
  return v;
}

function currentSeconds(nowSeconds: number | undefined): bigint {
  return BigInt(nowSeconds ?? Math.floor(Date.now() / 1000));
}

function randomNonce(): Uint8Array {
  const nonce = new Uint8Array(16);
  globalThis.crypto.getRandomValues(nonce);
  return nonce;
}

function evmAddressBytes(address: string | Uint8Array): Uint8Array {
  if (address instanceof Uint8Array) {
    if (address.length !== 20) throw new InvalidParamsError('evmAddress', 'the EVM address must be 20 bytes');
    return address;
  }
  const hex = address.startsWith('0x') || address.startsWith('0X') ? address.slice(2) : address;
  if (!/^[0-9a-fA-F]{40}$/.test(hex)) {
    throw new InvalidParamsError('evmAddress', 'the EVM address must be 40 hex characters');
  }
  const out = new Uint8Array(20);
  for (let i = 0; i < 20; i++) out[i] = Number.parseInt(hex.slice(i * 2, i * 2 + 2), 16);
  return out;
}

export function agentPublicKey(privateKey: Uint8Array): Uint8Array {
  checkPrivateKey(privateKey);
  return ed25519.getPublicKey(privateKey);
}

export function agentDid(publicKey: Uint8Array): string {
  if (!(publicKey instanceof Uint8Array) || publicKey.length !== 32) {
    throw new InvalidParamsError('publicKey', 'the agent public key must be 32 bytes');
  }
  return AGENT_DID_PREFIX + toHexString(publicKey);
}

export function agentRequestMethod(method: string, url: string): string {
  const path = /^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//.test(url) ? new URL(url).pathname : (url.split(/[?#]/, 1)[0] ?? url);
  return `${method.toUpperCase()} ${path}`;
}

export function agentRequestDigest(input: AgentRequestDigestInput): Uint8Array {
  const method = encoder.encode(input.method);
  const keyId = encoder.encode(input.keyId);
  if (method.length > U32_MAX || keyId.length > U32_MAX) {
    throw new InvalidParamsError('method', 'agent request field exceeds length limit');
  }
  if (input.nonce.length !== 16) throw new InvalidParamsError('nonce', 'agent request nonce must be 16 bytes');
  const expiry = checkU64('expiry', input.expiry);
  return sha256(
    concatBytes([
      encoder.encode(AGENT_REQUEST_DOMAIN),
      u32be(method.length),
      method,
      u32be(keyId.length),
      keyId,
      input.nonce,
      u64be(expiry),
      sha256(bodyBytes(input.body)),
    ]),
  );
}

export function signAgentRequest(input: SignAgentRequestInput): AgentRequestHeaders {
  checkPrivateKey(input.privateKey);
  if (!input.keyId) throw new InvalidParamsError('keyId', 'the agent key id is required');
  const nonce = input.nonce ?? randomNonce();
  const expiry =
    input.expiresAt === undefined
      ? currentSeconds(input.nowSeconds) + BigInt(AGENT_REQUEST_DEFAULT_TTL_SECONDS)
      : checkU64('expiresAt', input.expiresAt);
  const digest = agentRequestDigest({
    method: agentRequestMethod(input.method, input.url),
    keyId: input.keyId,
    nonce,
    expiry,
    body: bodyBytes(input.body),
  });
  return {
    [AGENT_REQUEST_HEADERS.key]: input.keyId,
    [AGENT_REQUEST_HEADERS.nonce]: toHexString(nonce),
    [AGENT_REQUEST_HEADERS.expires]: expiry.toString(),
    [AGENT_REQUEST_HEADERS.signature]: toHexString(ed25519.sign(digest, input.privateKey)),
  };
}

export function bindingMessage(chainId: bigint | number, evmAddress: string | Uint8Array, nonce: bigint | number): Uint8Array {
  return concatBytes([
    encoder.encode(AGENT_BIND_DOMAIN),
    u256be(checkU64('chainId', chainId)),
    evmAddressBytes(evmAddress),
    u64be(checkU64('nonce', nonce)),
  ]);
}

export function signBinding(input: SignBindingInput): SignedBinding {
  checkPrivateKey(input.privateKey);
  const message = bindingMessage(input.chainId, input.evmAddress, input.nonce);
  const publicKey = ed25519.getPublicKey(input.privateKey);
  return {
    message,
    signature: ed25519.sign(message, input.privateKey),
    publicKey,
    did: agentDid(publicKey),
  };
}

export function agentClaimDigest(did: string, ownerUserId: string, expiry: bigint | number): Uint8Array {
  return sha256(
    concatBytes([
      encoder.encode(AGENT_CLAIM_DOMAIN),
      lengthPrefixed(did),
      lengthPrefixed(ownerUserId),
      u64be(checkU64('expiry', expiry)),
    ]),
  );
}

export function buildClaim(input: BuildClaimInput): AgentClaim {
  checkPrivateKey(input.privateKey);
  if (!input.did) throw new InvalidParamsError('did', 'the agent DID is required');
  if (!input.ownerUserId) throw new InvalidParamsError('ownerUserId', 'the owner user id is required');
  const expiry =
    input.expiresAt === undefined
      ? currentSeconds(input.nowSeconds) + BigInt(AGENT_REQUEST_DEFAULT_TTL_SECONDS)
      : checkU64('expiresAt', input.expiresAt);
  return {
    expires: expiry.toString(),
    signature: toHexString(ed25519.sign(agentClaimDigest(input.did, input.ownerUserId, expiry), input.privateKey)),
  };
}

export async function claimAgent(
  fetchImpl: typeof fetch,
  gatewayBase: string,
  token: string,
  did: string,
  claim: AgentClaim,
): Promise<AgentClaimResult> {
  if (!token) throw new InvalidParamsError('token', 'the owner session token is required');
  const res = await fetchImpl(`${gatewayBase.replace(/\/$/, '')}/v1/agents/${encodeURIComponent(did)}/claim`, {
    method: 'POST',
    headers: { authorization: `Bearer ${token}`, 'content-type': 'application/json' },
    body: JSON.stringify({ expires: claim.expires, signature: claim.signature }),
  });
  let payload: unknown = null;
  try {
    payload = await res.json();
  } catch {
    payload = null;
  }
  if (!res.ok) throw gatewayRefusal(res.status, payload);
  const record = typeof payload === 'object' && payload !== null ? (payload as Record<string, unknown>) : {};
  if (typeof record.did !== 'string' || typeof record.owner_user_id !== 'string') {
    throw gatewayRefusal(res.status, payload);
  }
  return { did: record.did, owner_user_id: record.owner_user_id };
}
