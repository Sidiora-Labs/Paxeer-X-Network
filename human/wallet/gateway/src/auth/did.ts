import { createPublicKey, verify as cryptoVerify } from 'node:crypto';

/**
 * Matrix DID verification for the agent-native auth lane.
 *
 * A Matrix executor identity is an ed25519 keypair (see
 * executor/cmd/mcl-execute/identity.go). Its DID is:
 *
 *   did:matrix:<label>:<keyfp>
 *
 * where <keyfp> = hex(ed25519_public_key)[:16] (first 8 bytes, 16 hex chars)
 * and <label> is a free-form suffix — in hosted Matrix it is the owner's
 * Supabase user_id, which is how we bind an agent to a human.
 *
 * The DID embeds only the first 8 bytes of the public key, so the agent MUST
 * present the full 32-byte key at verify time. We:
 *   1. check hex(presentedKey)[:16] === the DID's keyfp segment, and
 *   2. ed25519-verify the agent's signature over the challenge message.
 *
 * ed25519 verification uses Node's native crypto. Raw 32-byte keys aren't
 * directly loadable, so we wrap them in the fixed 12-byte SPKI/DER prefix for
 * Ed25519 and hand the result to createPublicKey — no extra dependency.
 */

const DID_RE = /^did:matrix:([A-Za-z0-9_-]{1,128}):([0-9a-f]{16})$/;
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

// ASN.1 SPKI prefix for an Ed25519 public key (RFC 8410):
//   SEQUENCE(0x30 0x2a) { SEQUENCE(0x30 0x05) { OID 1.3.101.112 } BIT STRING(0x03 0x21 0x00) }
const ED25519_SPKI_PREFIX = Buffer.from('302a300506032b6570032100', 'hex');

export interface ParsedDid {
  did: string;
  label: string;
  keyFingerprint: string; // 16 lowercase hex chars
}

/** Parse + validate a did:matrix string. Returns null if malformed. */
export function parseDid(did: string): ParsedDid | null {
  if (typeof did !== 'string') return null;
  const m = DID_RE.exec(did.trim());
  if (!m) return null;
  return { did: did.trim(), label: m[1]!, keyFingerprint: m[2]! };
}

/** True when the DID's label segment is a UUID (i.e. an owner Supabase id). */
export function labelIsUuid(label: string): boolean {
  return UUID_RE.test(label);
}

/** Strip an optional 0x prefix and lowercase. */
function stripHex(s: string): string {
  return (s.startsWith('0x') || s.startsWith('0X') ? s.slice(2) : s).toLowerCase();
}

/** Validate a 32-byte (64-hex) ed25519 public key. Returns the raw buffer or null. */
export function parsePublicKey(publicKeyHex: string): Buffer | null {
  if (typeof publicKeyHex !== 'string') return null;
  const hex = stripHex(publicKeyHex);
  if (!/^[0-9a-f]{64}$/.test(hex)) return null;
  return Buffer.from(hex, 'hex');
}

/**
 * The exact message bytes an agent signs to prove control of its DID key.
 * Domain-separated so an ed25519 signature minted here can't be replayed as a
 * Matrix envelope signature or vice-versa.
 */
export function challengeMessage(did: string, nonce: string): Buffer {
  return Buffer.from(`paxeer-agent-auth:${did}:${nonce}`, 'utf8');
}

export interface VerifyAgentSigInput {
  did: string;
  publicKeyHex: string;
  nonce: string;
  signatureHex: string;
}

export interface VerifyAgentSigResult {
  ok: boolean;
  reason?: string;
  parsed?: ParsedDid;
  publicKeyHex?: string; // normalized lowercase, no 0x
}

/**
 * Verify that `signature` is a valid ed25519 signature over
 * challengeMessage(did, nonce) by the key whose fingerprint matches the DID.
 *
 * Pure crypto + structural checks — does NOT touch the DB or the nonce store.
 * Callers (routes) are responsible for consuming the nonce + checking expiry.
 */
export function verifyAgentSignature(input: VerifyAgentSigInput): VerifyAgentSigResult {
  const parsed = parseDid(input.did);
  if (!parsed) return { ok: false, reason: 'malformed_did' };

  const rawKey = parsePublicKey(input.publicKeyHex);
  if (!rawKey) return { ok: false, reason: 'malformed_public_key' };

  const keyHex = rawKey.toString('hex');
  // The DID fingerprint is the first 16 hex chars (8 bytes) of the public key.
  if (keyHex.slice(0, 16) !== parsed.keyFingerprint) {
    return { ok: false, reason: 'public_key_does_not_match_did' };
  }

  const sigHex = stripHex(input.signatureHex);
  if (!/^[0-9a-f]{128}$/.test(sigHex)) {
    return { ok: false, reason: 'malformed_signature' };
  }
  const signature = Buffer.from(sigHex, 'hex');

  let keyObject;
  try {
    keyObject = createPublicKey({
      key: Buffer.concat([ED25519_SPKI_PREFIX, rawKey]),
      format: 'der',
      type: 'spki',
    });
  } catch {
    return { ok: false, reason: 'invalid_public_key_encoding' };
  }

  const message = challengeMessage(parsed.did, input.nonce);
  let valid = false;
  try {
    // Algorithm arg is null for Ed25519 (the algorithm is implied by the key).
    valid = cryptoVerify(null, message, keyObject, signature);
  } catch {
    return { ok: false, reason: 'verify_threw' };
  }

  if (!valid) return { ok: false, reason: 'bad_signature', parsed };
  return { ok: true, parsed, publicKeyHex: keyHex };
}
