import type { PoolClient } from 'pg';
import { hashMessage, keccak256, recoverMessageAddress, serializeSignature, type Hex } from 'viem';
import { decodeCustody, splitSignature, type SignResult } from '../attestor/client.js';

export interface CustodyAuthorizationIdentity {
  id: Hex;
  userId: string;
  walletId: string;
  address: Hex;
  chainId: number;
  keyId: string;
  custody: Hex;
}

export interface CustodyAuthorizationRecord {
  id: Hex;
  user_id: string;
  wallet_id: string;
  address: Hex;
  chain_id: string | number;
  attestor_key_id: string;
  custody: Hex;
  state: 'signing_unknown' | 'signed';
  signature: Hex | null;
  evidence: SignResult | null;
}

export class CustodyAuthorizationError extends Error {
  constructor(readonly code: string) {
    super(code);
    this.name = 'CustodyAuthorizationError';
  }
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const HALF_ORDER = 0x7fffffffffffffffffffffffffffffff5d576e7357a4501ddfe92f46681b20a0n;
const ORDER = 0xfffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364141n;

function identityValues(identity: CustodyAuthorizationIdentity): unknown[] {
  try {
    if (!UUID.test(identity.userId) || !UUID.test(identity.walletId)
      || !/^0x[0-9a-f]{64}$/.test(identity.id) || !/^0x[0-9a-fA-F]{40}$/.test(identity.address)
      || !Number.isSafeInteger(identity.chainId) || identity.chainId <= 0
      || typeof identity.keyId !== 'string' || identity.keyId.length === 0 || identity.keyId.length > 4096
      || !/^0x(?:[0-9a-fA-F]{2})+$/.test(identity.custody) || identity.custody.length > 65_538
      || keccak256(identity.custody) !== identity.id) throw new Error('identity');
    decodeCustody(identity.custody, identity.address, identity.chainId, true);
  } catch {
    throw new CustodyAuthorizationError('custody_authorization_mismatch');
  }
  return [identity.id, identity.userId, identity.walletId, identity.address.toLowerCase(),
    identity.chainId, identity.keyId, identity.custody.toLowerCase()];
}

async function ownedWallet(client: PoolClient, identity: CustodyAuthorizationIdentity): Promise<void> {
  const values = identityValues(identity);
  const wallet = await client.query(`select id from wallets where id=$1 and user_id=$2
    and lower(address)=$3 and chain_id=$4 and attestor_key_id=$5
    and not is_disabled and archived_at is null and kind='standard' for share`,
  [values[2], values[1], values[3], values[4], values[5]]);
  if (wallet.rowCount !== 1) throw new CustodyAuthorizationError('custody_authorization_mismatch');
}

function stable(value: unknown): string {
  if (Array.isArray(value)) return '[' + value.map(stable).join(',') + ']';
  if (value !== null && typeof value === 'object') {
    return '{' + Object.entries(value).sort(([a], [b]) => a.localeCompare(b))
      .map(([key, item]) => JSON.stringify(key) + ':' + stable(item)).join(',') + '}';
  }
  const encoded = JSON.stringify(value);
  if (encoded === undefined) throw new CustodyAuthorizationError('custody_authorization_proof_invalid');
  return encoded;
}

async function verifyProof(identity: CustodyAuthorizationIdentity, signature: Hex, evidence: SignResult): Promise<void> {
  try {
    if (!/^0x[0-9a-f]{130}$/.test(signature) || !evidence || typeof evidence !== 'object'
      || !UUID.test(evidence.sessionId) || evidence.kind !== 'custody'
      || !/^0x[0-9a-f]{130}$/.test(evidence.signature)
      || evidence.signedBytes !== hashMessage({ raw: identity.custody })
      || !Array.isArray(evidence.participants) || evidence.participants.length !== 3
      || evidence.participants.some(member => typeof member !== 'string' || member.length === 0 || member.length > 4096)
      || new Set(evidence.participants).size !== 3
      || !Array.isArray(evidence.audit) || evidence.audit.length !== 3
      || evidence.audit.some((audit, index) => !audit || audit.node_id !== evidence.participants[index]
        || !Number.isSafeInteger(audit.audit_sequence) || audit.audit_sequence < 0)
      || Buffer.byteLength(stable(evidence)) > 32768) throw new Error('proof');
    const parts = splitSignature(evidence);
    const r = BigInt(parts.r);
    const s = BigInt(parts.s);
    if (r <= 0n || r >= ORDER || s <= 0n || s > HALF_ORDER
      || serializeSignature(parts).toLowerCase() !== signature) throw new Error('signature');
    const recovered = await recoverMessageAddress({ message: { raw: identity.custody }, signature });
    if (recovered.toLowerCase() !== identity.address.toLowerCase()) throw new Error('owner');
  } catch {
    throw new CustodyAuthorizationError('custody_authorization_proof_invalid');
  }
}

export async function readCustodyAuthorization(
  client: PoolClient, identity: CustodyAuthorizationIdentity,
): Promise<CustodyAuthorizationRecord | null> {
  const values = identityValues(identity);
  await ownedWallet(client, identity);
  const found = await client.query<CustodyAuthorizationRecord>(
    'select * from wallet_custody_authorizations where id=$1 for update', [identity.id]);
  const record = found.rows[0];
  if (!record) return null;
  if (record.id !== values[0] || record.user_id !== identity.userId.toLowerCase()
    || record.wallet_id !== identity.walletId.toLowerCase() || record.address !== values[3]
    || String(record.chain_id) !== String(identity.chainId) || record.attestor_key_id !== identity.keyId
    || record.custody !== values[6]) throw new CustodyAuthorizationError('custody_authorization_mismatch');
  if (record.state === 'signed') {
    if (!record.signature || !record.evidence) throw new CustodyAuthorizationError('custody_authorization_proof_invalid');
    await verifyProof(identity, record.signature, record.evidence);
  } else if (record.state !== 'signing_unknown' || record.signature !== null || record.evidence !== null) {
    throw new CustodyAuthorizationError('custody_authorization_proof_invalid');
  }
  return record;
}

export async function retainCustodyAuthorization(
  client: PoolClient, identity: CustodyAuthorizationIdentity,
): Promise<{ record: CustodyAuthorizationRecord; attempt: boolean }> {
  const values = identityValues(identity);
  await ownedWallet(client, identity);
  const inserted = await client.query(`insert into wallet_custody_authorizations
    (id,user_id,wallet_id,address,chain_id,attestor_key_id,custody,state)
    values($1,$2,$3,$4,$5,$6,$7,'signing_unknown') on conflict(id) do nothing returning id`, values);
  const record = await readCustodyAuthorization(client, identity);
  if (!record) throw new CustodyAuthorizationError('custody_authorization_unavailable');
  return { record, attempt: inserted.rowCount === 1 };
}

export async function completeCustodyAuthorization(
  client: PoolClient, identity: CustodyAuthorizationIdentity, signature: Hex, evidence: SignResult,
): Promise<CustodyAuthorizationRecord> {
  const record = await readCustodyAuthorization(client, identity);
  if (!record) throw new CustodyAuthorizationError('custody_authorization_unavailable');
  await verifyProof(identity, signature, evidence);
  if (record.state === 'signed') {
    if (record.signature !== signature || stable(record.evidence) !== stable(evidence)) {
      throw new CustodyAuthorizationError('custody_authorization_proof_changed');
    }
    return record;
  }
  const saved = await client.query<CustodyAuthorizationRecord>(`update wallet_custody_authorizations
    set state='signed',signature=$2,evidence=$3::jsonb,completed_at=now(),updated_at=now()
    where id=$1 and state='signing_unknown' returning *`, [identity.id, signature, JSON.stringify(evidence)]);
  if (saved.rowCount !== 1 || !saved.rows[0]) throw new CustodyAuthorizationError('custody_authorization_unavailable');
  return saved.rows[0];
}
