import { createPrivateKey, sign } from 'node:crypto';
import { closeSync, constants, fstatSync, openSync, readFileSync } from 'node:fs';
import { isAbsolute } from 'node:path';
import type { Pool } from 'pg';
import { bytesToHex, getAddress, type Hex, type LocalAccount, type TransactionSerializableEIP1559 } from 'viem';
import { RpcPool } from '../rpc/pool.js';
import { NonceStore } from '../nonce/store.js';
import {
  WalletArchivedError,
  agentWalletUserId,
  findArchivedWallet,
  getSigningAccountForRow,
  getMigrationAwareSigningAccountForRow,
  type WalletKind,
  type WalletRow,
} from '../db/wallets.js';
import {
  ADDR_PRECOMPILE,
  AttestorDaemonClient,
  bindLayerX,
  bindLayerXCalldata,
  bindMessage,
  broadcastOnce,
  didFromPublicKey,
  mainAccountId,
  normalisePublicKey,
  readBindNonce,
  readUnifiedAccount,
  signBindWithAttestors,
  signTransactionWithAttestors,
  transactionHash,
  verifyEd25519,
  waitForReceipt,
} from './bind.js';

import { CustodyAuthorityError, publishCustodyAuthority } from '../agent/authority.js';
import type { AgentOriginalRequest, AgentReauthorization } from '../attestor/client.js';

export type ProvisionState =
  | 'new'
  | 'evm_key'
  | 'identity'
  | 'bind_signed'
  | 'funded'
  | 'bind_sent'
  | 'active'
  | 'refused';

export type ProvisionSubject =
  | { kind: 'standard'; userId: string; token: string | null }
  | { kind: 'agent'; did: string; agentSignature?: string | null; custody?: { origin: AgentOriginalRequest; reauthorization?: AgentReauthorization } };

export interface ProvisionDeps {
  pool: Pool;
  rpc: RpcPool;
  attestors: AttestorDaemonClient | null;
  nonces: NonceStore;
  sponsor: LocalAccount | null;
  chainId: number;
  gasCapWei: bigint;
  receiptTimeoutMs: number;
  receiptPollMs: number;
  identityBinding?: { issuer: string; tenant: string; privateKeyFile: string };
}

export interface ProvisionResult {
  state: ProvisionState;
  awaiting: 'owner' | 'agent_signature' | null;
  walletId: string;
  address: `0x${string}`;
  did: string | null;
  mainAccountId: string | null;
  bindMessage: Hex | null;
}

export class ProvisionError extends Error {
  readonly code: string;
  readonly status: number;
  constructor(code: string, status: number, message: string) {
    super(message);
    this.name = 'ProvisionError';
    this.code = code;
    this.status = status;
  }
}

export class ProvisionRefusedError extends ProvisionError {
  readonly boundDid: string | null;
  constructor(reason: string, boundDid: string | null) {
    super('binding_refused', 409, reason);
    this.name = 'ProvisionRefusedError';
    this.boundDid = boundDid;
  }
}

interface ProvisionRow {
  id: string;
  user_id: string;
  kind: WalletKind;
  wallet_id: string | null;
  state: ProvisionState;
  evm_key_generation: number;
  ed_key_generation: number;
  evm_key_id: string | null;
  ed_key_id: string | null;
  ed_public_key: string | null;
  did: string | null;
  main_account_id: string | null;
  bind_nonce: string | null;
  bind_signature: string | null;
  topup_raw_tx: Hex | null;
  topup_tx_hash: Hex | null;
  topup_value_wei: string | null;
  bind_gas: string | null;
  bind_max_fee_wei: string | null;
  bind_raw_tx: Hex | null;
  bind_tx_hash: Hex | null;
  refusal_reason: string | null;
}

interface WalletRecord {
  id: string;
  user_id: string;
  address: `0x${string}`;
  encrypted_private_key: string | null;
  key_version: number;
  chain_id: number;
  kind: WalletKind;
  created_at: string;
  last_used_at: string | null;
  is_disabled: boolean;
  disabled_reason: string | null;
  migrated_at: string | null;
  attestor_key_id: string | null;
}

const WALLET_COLUMNS = `id, user_id, address, encrypted_private_key, key_version, chain_id, kind,
  created_at, last_used_at, is_disabled, disabled_reason, migrated_at, attestor_key_id`;

async function loadWallet(pool: Pool, userId: string, kind: WalletKind): Promise<WalletRecord | null> {
  const { rows } = await pool.query<WalletRecord>(
    `select ${WALLET_COLUMNS} from wallets where user_id = $1 and kind = $2 and archived_at is null limit 1`,
    [userId, kind],
  );
  return rows[0] ?? null;
}

async function loadWalletById(pool: Pool, id: string): Promise<WalletRecord> {
  const { rows } = await pool.query<WalletRecord>(`select ${WALLET_COLUMNS} from wallets where id = $1`, [id]);
  if (!rows[0]) throw new ProvisionError('wallet_missing', 500, `wallet ${id} vanished during provisioning`);
  return rows[0];
}

async function updateRow(pool: Pool, id: string, fields: Partial<Record<keyof ProvisionRow, unknown>>): Promise<void> {
  const keys = Object.keys(fields);
  const sets = keys.map((k, i) => `${k} = $${i + 2}`).join(', ');
  await pool.query(`update account_provisioning set ${sets}, updated_at = now() where id = $1`, [
    id,
    ...keys.map((k) => fields[k as keyof ProvisionRow]),
  ]);
}

async function loadRow(pool: Pool, userId: string, kind: WalletKind): Promise<ProvisionRow> {
  await pool.query(
    `insert into account_provisioning (user_id, kind) values ($1, $2) on conflict (user_id, kind) do nothing`,
    [userId, kind],
  );
  const { rows } = await pool.query<ProvisionRow>(
    `select id::text, user_id, kind, wallet_id, state, evm_key_generation, ed_key_generation, evm_key_id, ed_key_id,
            ed_public_key, did, main_account_id, bind_nonce::text, bind_signature, topup_raw_tx, topup_tx_hash,
            topup_value_wei::text, bind_gas::text, bind_max_fee_wei::text, bind_raw_tx, bind_tx_hash, refusal_reason
       from account_provisioning where user_id = $1 and kind = $2`,
    [userId, kind],
  );
  return rows[0]!;
}

interface AgentPrincipal {
  did: string;
  public_key: string;
  wallet_id: string | null;
  is_frozen: boolean;
}

async function loadPrincipal(pool: Pool, did: string): Promise<AgentPrincipal> {
  const { rows } = await pool.query<AgentPrincipal>(
    `select did, public_key, wallet_id, is_frozen from agent_principals where did = $1`,
    [did],
  );
  if (!rows[0]) throw new ProvisionError('agent_unknown', 404, 'agent is not registered');
  if (rows[0].is_frozen) throw new ProvisionError('agent_frozen', 403, 'agent is frozen');
  return rows[0];
}

async function recordAudit(
  pool: Pool,
  entry: { walletId: string | null; address: string; event: 'topup' | 'bind' | 'refusal' | 'backfill'; txHash?: string | null; valueWei?: bigint | null; gas?: bigint | null; maxFeeWei?: bigint | null; outcome?: string | null; reason?: string | null },
): Promise<void> {
  await pool.query(
    `insert into account_setup_audit (wallet_id, address, event, tx_hash, value_wei, gas, max_fee_wei, outcome, reason)
     select $1, $2, $3, $4, $5, $6, $7, $8, $9
      where $4::text is null
         or not exists (select 1 from account_setup_audit where event = $3 and tx_hash = $4::text)
     on conflict do nothing`,
    [
      entry.walletId,
      entry.address.toLowerCase(),
      entry.event,
      entry.txHash ?? null,
      entry.valueWei?.toString() ?? null,
      entry.gas?.toString() ?? null,
      entry.maxFeeWei?.toString() ?? null,
      entry.outcome ?? null,
      entry.reason ?? null,
    ],
  );
}

async function lockSubject(pool: Pool, userId: string, kind: WalletKind): Promise<() => Promise<void>> {
  const client = await pool.connect();
  try {
    await client.query(`select pg_advisory_lock(hashtextextended($1, 0))`, [`provision:${userId}:${kind}`]);
  } catch (err) {
    client.release();
    throw err;
  }
  return async () => {
    try {
      await client.query(`select pg_advisory_unlock(hashtextextended($1, 0))`, [`provision:${userId}:${kind}`]);
    } finally {
      client.release();
    }
  };
}

function requireAttestors(deps: ProvisionDeps): AttestorDaemonClient {
  if (!deps.attestors) throw new ProvisionError('attestors_unconfigured', 503, 'no attestor endpoints are configured');
  return deps.attestors;
}

async function fees(rpc: RpcPool): Promise<{ maxFeePerGas: bigint; maxPriorityFeePerGas: bigint }> {
  const [block, tip] = await Promise.all([
    rpc.request<{ baseFeePerGas?: Hex } | null>('eth_getBlockByNumber', ['latest', false]),
    rpc.request<Hex>('eth_maxPriorityFeePerGas', []),
  ]);
  if (!block?.baseFeePerGas) throw new ProvisionError('chain_unavailable', 503, 'latest block carries no base fee');
  const priority = BigInt(tip);
  return { maxFeePerGas: BigInt(block.baseFeePerGas) + priority, maxPriorityFeePerGas: priority };
}

function result(row: ProvisionRow, wallet: WalletRecord, awaiting: ProvisionResult['awaiting'] = null, message: Hex | null = null): ProvisionResult {
  return {
    state: row.state,
    awaiting,
    walletId: wallet.id,
    address: getAddress(wallet.address),
    did: row.did,
    mainAccountId: row.main_account_id,
    bindMessage: message,
  };
}

async function refuse(deps: ProvisionDeps, row: ProvisionRow, wallet: WalletRecord, reason: string, boundDid: string | null): Promise<never> {
  await updateRow(deps.pool, row.id, { state: 'refused', refusal_reason: reason });
  await deps.pool.query(`update wallets set binding_state = 'refused' where id = $1`, [wallet.id]);
  await recordAudit(deps.pool, { walletId: wallet.id, address: wallet.address, event: 'refusal', reason, outcome: boundDid });
  row.state = 'refused';
  row.refusal_reason = reason;
  throw new ProvisionRefusedError(reason, boundDid);
}

async function activate(deps: ProvisionDeps, row: ProvisionRow, wallet: WalletRecord): Promise<void> {
  await updateRow(deps.pool, row.id, { state: 'active' });
  await deps.pool.query(`update wallets set binding_state = 'bound' where id = $1`, [wallet.id]);
  row.state = 'active';
}

async function createEvmKey(deps: ProvisionDeps, row: ProvisionRow, userId: string, kind: WalletKind, owner = userId): Promise<WalletRecord> {
  const archived = await findArchivedWallet({ userId, kind });
  if (archived) throw new WalletArchivedError(archived.id, archived.address);
  const attestors = requireAttestors(deps);
  const keyId = `wallet:${userId}:${kind}:secp256k1:${row.evm_key_generation}`;
  await updateRow(deps.pool, row.id, { evm_key_id: keyId });
  row.evm_key_id = keyId;
  let key;
  try {
    key = await attestors.generate(keyId, 'secp256k1', owner);
  } catch (err) {
    if (err instanceof CustodyAuthorityError) throw new ProvisionError('custody_inventory_pending', 503, `approved inventory is required for ${keyId}`);
    throw err;
  }
  const { rows } = await deps.pool.query<WalletRecord>(
    `insert into wallets (user_id, address, encrypted_private_key, key_version, chain_id, kind, migrated_at, attestor_key_id, binding_state)
     values ($1, $2, null, 1, $3, $4, now(), $5, 'pending')
     returning ${WALLET_COLUMNS}`,
    [userId, key.address, deps.chainId, kind, keyId],
  );
  const wallet = rows[0]!;
  await updateRow(deps.pool, row.id, { state: 'evm_key', wallet_id: wallet.id, evm_key_id: keyId });
  Object.assign(row, { state: 'evm_key', wallet_id: wallet.id, evm_key_id: keyId });
  return wallet;
}

async function storeIdentity(deps: ProvisionDeps, row: ProvisionRow, wallet: WalletRecord, publicKey: string, edKeyId: string | null): Promise<void> {
  const did = didFromPublicKey(publicKey);
  const main = mainAccountId(did);
  await deps.pool.query(
    `update wallets set did = $2, main_account_id = $3, layerx_key_id = $4, binding_state = 'pending' where id = $1`,
    [wallet.id, did, main, edKeyId],
  );
  await updateRow(deps.pool, row.id, { state: 'identity', ed_key_id: edKeyId, ed_public_key: publicKey, did, main_account_id: main });
  Object.assign(row, { state: 'identity', ed_key_id: edKeyId, ed_public_key: publicKey, did, main_account_id: main });
}

async function signBindTransaction(
  deps: ProvisionDeps,
  subject: ProvisionSubject,
  wallet: WalletRecord,
  tx: TransactionSerializableEIP1559,
): Promise<Hex | null> {
  if (wallet.migrated_at || !wallet.encrypted_private_key) {
    if (!wallet.attestor_key_id) throw new ProvisionError('wallet_key_missing', 503, 'wallet has no attestor key');
    if (subject.kind === 'agent') {
      const principal = await loadPrincipal(deps.pool, subject.did);
      if (!subject.custody || subject.custody.origin.did !== subject.did) {
        throw new ProvisionError('agent_request_required', 401, 'binding requires the original signed agent request');
      }
      const account = await getMigrationAwareSigningAccountForRow(wallet as unknown as WalletRow, {
        scheme: 'agent_request', publicKey: principal.public_key, ...subject.custody,
      }, undefined, undefined, { subject: subject.did, route: '/v1/agent/provision', requestHash: wallet.id });
      return account.signTransaction(tx);
    }
    if (!subject.token) return null;
    return signTransactionWithAttestors(requireAttestors(deps), wallet.attestor_key_id, getAddress(wallet.address), tx, subject.token);
  }
  if (!wallet.encrypted_private_key) throw new ProvisionError('wallet_key_missing', 500, 'wallet holds no signing key');
  const account = await getSigningAccountForRow(wallet as unknown as WalletRow);
  return account.signTransaction(tx);
}

export async function provisionAccount(deps: ProvisionDeps, subject: ProvisionSubject): Promise<ProvisionResult> {
  let userId: string;
  let kind: WalletKind;
  let principal: AgentPrincipal | null = null;
  if (subject.kind === 'standard') {
    userId = subject.userId;
    kind = 'standard';
  } else {
    principal = await loadPrincipal(deps.pool, subject.did);
    userId = agentWalletUserId(subject.did);
    kind = 'agent';
  }
  const unlock = await lockSubject(deps.pool, userId, kind);
  try {
    const row = await loadRow(deps.pool, userId, kind);
    if (row.state === 'refused') {
      throw new ProvisionRefusedError(row.refusal_reason ?? 'binding refused', null);
    }
    let wallet: WalletRecord | null = row.wallet_id ? await loadWalletById(deps.pool, row.wallet_id) : null;

    if (row.state === 'new') {
      const existing = wallet ?? (await loadWallet(deps.pool, userId, kind));
      if (existing) {
        wallet = existing;
        await updateRow(deps.pool, row.id, { state: 'evm_key', wallet_id: existing.id, evm_key_id: existing.attestor_key_id });
        Object.assign(row, { state: 'evm_key', wallet_id: existing.id, evm_key_id: existing.attestor_key_id });
      } else {
        if (subject.kind === 'agent' && !subject.custody) {
          throw new ProvisionError('agent_request_required', 401, 'provisioning requires the original signed agent request');
        }
        wallet = await createEvmKey(deps, row, userId, kind, principal ? `agent:${normalisePublicKey(principal.public_key)}` : userId);
      }
    }
    if (principal && wallet) {
      await deps.pool.query('update agent_principals set wallet_id = $2 where did = $1', [subject.kind === 'agent' ? subject.did : '', wallet.id]);
      await publishCustodyAuthority();
    }
    if (!wallet) throw new ProvisionError('wallet_missing', 500, 'provisioning row has no wallet');
    if (wallet.is_disabled) throw new ProvisionError('wallet_disabled', 403, wallet.disabled_reason ?? 'wallet is disabled');
    const address = getAddress(wallet.address);

    if (row.state === 'evm_key') {
      if (principal) {
        await storeIdentity(deps, row, wallet, normalisePublicKey(principal.public_key), null);
      } else {
        const attestors = requireAttestors(deps);
        const keyId = `account:${userId}:${kind}:ed25519:${row.ed_key_generation}`;
        let key;
        try {
          key = await attestors.generate(keyId, 'ed25519', userId, address);
        } catch (err) {
          if (err instanceof CustodyAuthorityError) throw new ProvisionError('custody_inventory_pending', 503, `approved inventory is required for ${keyId}`);
          throw err;
        }
        await storeIdentity(deps, row, wallet, key.publicKey, keyId);
      }
    }

    if (row.state === 'identity') {
      const bound = await readUnifiedAccount(deps.rpc, address);
      if (bound.didPublicKey) {
        if (bound.didPublicKey === row.ed_public_key) {
          await activate(deps, row, wallet);
          return result(row, wallet);
        }
        return await refuse(deps, row, wallet, 'address is already bound to a different DID', didFromPublicKey(bound.didPublicKey));
      }
      const nonce = await readBindNonce(deps.rpc, address);
      const message = bindMessage(deps.chainId, address, nonce);
      let signature: string;
      if (principal) {
        const offered = subject.kind === 'agent' ? subject.agentSignature : null;
        if (!offered) return result(row, wallet, 'agent_signature', bytesToHex(message));
        if (!verifyEd25519(row.ed_public_key!, message, offered)) {
          throw new ProvisionError('agent_signature_invalid', 403, 'binding signature does not verify under the registered agent key');
        }
        signature = offered.replace(/^0x/, '').toLowerCase();
      } else {
        if (subject.kind !== 'standard' || !subject.token) return result(row, wallet, 'owner', bytesToHex(message));
        signature = await signBindWithAttestors(requireAttestors(deps), row.ed_key_id!, row.ed_public_key!, message, subject.token);
      }
      await updateRow(deps.pool, row.id, { state: 'bind_signed', bind_nonce: nonce.toString(), bind_signature: signature });
      Object.assign(row, { state: 'bind_signed', bind_nonce: nonce.toString(), bind_signature: signature });
    }

    if (row.state === 'bind_signed') {
      let gas = row.bind_gas ? BigInt(row.bind_gas) : null;
      let maxFee = row.bind_max_fee_wei ? BigInt(row.bind_max_fee_wei) : null;
      if (gas === null || maxFee === null) {
        const data = bindLayerXCalldata(row.ed_public_key!, row.bind_signature!);
        const estimate = BigInt(await deps.rpc.request<Hex>('eth_estimateGas', [{ from: address, to: ADDR_PRECOMPILE, data }]));
        const f = await fees(deps.rpc);
        gas = estimate;
        maxFee = f.maxFeePerGas;
        await updateRow(deps.pool, row.id, { bind_gas: gas.toString(), bind_max_fee_wei: maxFee.toString() });
        row.bind_gas = gas.toString();
        row.bind_max_fee_wei = maxFee.toString();
      }
      const required = gas * maxFee;
      if (row.topup_raw_tx) {
        const hash = await broadcastOnce(deps.rpc, row.topup_raw_tx);
        const receipt = await waitForReceipt(deps.rpc, hash, deps.receiptTimeoutMs, deps.receiptPollMs);
        if (receipt.status !== 'success') throw new ProvisionError('topup_reverted', 502, 'sponsor top-up transaction reverted');
      } else {
        const balance = BigInt(await deps.rpc.request<Hex>('eth_getBalance', [address, 'latest']));
        if (balance < required) {
          if (required > deps.gasCapWei) {
            throw new ProvisionError('setup_gas_over_cap', 503, `binding needs ${required} wei, above the setup gas cap ${deps.gasCapWei}`);
          }
          const { rows: paid } = await deps.pool.query(
            `select 1 from account_setup_audit where event = 'topup' and address = $1 limit 1`,
            [address.toLowerCase()],
          );
          if (paid.length > 0) throw new ProvisionError('topup_already_paid', 409, 'this address already received its setup top-up');
          const sponsor = deps.sponsor;
          if (!sponsor) throw new ProvisionError('sponsor_unconfigured', 503, 'no setup sponsor key is configured');
          const value = required - balance;
          const f = await fees(deps.rpc);
          const raw = await deps.nonces.withLock(sponsor.address, async (lease) => {
            const nonce = await lease.next();
            const signed = await sponsor.signTransaction({
              type: 'eip1559',
              chainId: deps.chainId,
              nonce,
              to: address,
              value,
              gas: 21_000n,
              maxFeePerGas: f.maxFeePerGas,
              maxPriorityFeePerGas: f.maxPriorityFeePerGas,
            });
            const hash = transactionHash(signed);
            await updateRow(deps.pool, row.id, { topup_raw_tx: signed, topup_tx_hash: hash, topup_value_wei: value.toString() });
            await recordAudit(deps.pool, { walletId: wallet!.id, address, event: 'topup', txHash: hash, valueWei: value, gas, maxFeeWei: maxFee });
            return signed;
          });
          row.topup_raw_tx = raw;
          const hash = await broadcastOnce(deps.rpc, raw);
          const receipt = await waitForReceipt(deps.rpc, hash, deps.receiptTimeoutMs, deps.receiptPollMs);
          if (receipt.status !== 'success') throw new ProvisionError('topup_reverted', 502, 'sponsor top-up transaction reverted');
        }
      }
      await updateRow(deps.pool, row.id, { state: 'funded' });
      row.state = 'funded';
    }

    if (row.state === 'funded') {
      const f = await fees(deps.rpc);
      const maxFee = BigInt(row.bind_max_fee_wei!);
      const priority = f.maxPriorityFeePerGas < maxFee ? f.maxPriorityFeePerGas : maxFee;
      const signerWallet = wallet;
      const raw = await deps.nonces.withLock(address, async (lease) => {
        const nonce = await lease.next();
        const tx = bindLayerX({
          chainId: deps.chainId,
          address,
          publicKey: row.ed_public_key!,
          signature: row.bind_signature!,
          nonce,
          gas: BigInt(row.bind_gas!),
          maxFeePerGas: maxFee,
          maxPriorityFeePerGas: priority,
        });
        const signed = await signBindTransaction(deps, subject, signerWallet, tx);
        if (!signed) throw new ProvisionError('owner_required', 401, 'the wallet owner must authorise the binding transaction');
        const hash = transactionHash(signed);
        await updateRow(deps.pool, row.id, { state: 'bind_sent', bind_raw_tx: signed, bind_tx_hash: hash });
        return signed;
      }).catch(async (err) => {
        if (err instanceof ProvisionError && err.code === 'owner_required') return null;
        throw err;
      });
      if (!raw) return result(row, wallet, 'owner');
      Object.assign(row, { state: 'bind_sent', bind_raw_tx: raw, bind_tx_hash: transactionHash(raw) });
    }

    if (row.state === 'bind_sent') {
      const hash = await broadcastOnce(deps.rpc, row.bind_raw_tx!);
      const receipt = await waitForReceipt(deps.rpc, hash, deps.receiptTimeoutMs, deps.receiptPollMs);
      await recordAudit(deps.pool, {
        walletId: wallet.id,
        address,
        event: 'bind',
        txHash: hash,
        gas: receipt.gasUsed,
        maxFeeWei: BigInt(row.bind_max_fee_wei!),
        outcome: receipt.status,
      });
      const bound = await readUnifiedAccount(deps.rpc, address);
      if (bound.didPublicKey === row.ed_public_key) {
        await activate(deps, row, wallet);
      } else if (bound.didPublicKey) {
        return await refuse(deps, row, wallet, 'address is already bound to a different DID', didFromPublicKey(bound.didPublicKey));
      } else {
        return await refuse(deps, row, wallet, `binding transaction ${receipt.status} without binding the address`, null);
      }
    }

    return result(row, wallet);
  } finally {
    await unlock();
  }
}

export async function produceIdentityBinding(
  deps: ProvisionDeps,
  userId: string,
  provision: ProvisionResult,
): Promise<string | null> {
  const config = deps.identityBinding;
  if (!config || provision.state !== 'active') return null;
  if (!provision.did || !/^did:layerx:[0-9a-f]{64}$/.test(provision.did)
    || provision.did === `did:layerx:${'0'.repeat(64)}`) {
    throw new ProvisionError('identity_binding_unavailable', 503, 'active wallet has no valid LayerX identity');
  }
  const { rows } = await deps.pool.query<{ ed_public_key: string }>(
    `select p.ed_public_key from account_provisioning p join wallets w on w.id = p.wallet_id
      where p.user_id = $1 and p.kind = 'standard' and p.wallet_id = $2
        and p.state = 'active' and p.did = $3 and w.user_id = $1 and w.kind = 'standard'
        and w.did = p.did and w.binding_state = 'bound' and w.archived_at is null
        and w.is_disabled = false and lower(w.address) = lower($4)`,
    [userId, provision.walletId, provision.did, provision.address],
  );
  const publicKey = rows[0]?.ed_public_key;
  if (rows.length !== 1 || !publicKey || didFromPublicKey(publicKey) !== provision.did) {
    throw new ProvisionError('identity_binding_refused', 409, 'wallet identity is not active for this user');
  }
  const bound = await readUnifiedAccount(deps.rpc, provision.address);
  if (bound.didPublicKey !== publicKey) {
    throw new ProvisionError('identity_binding_refused', 409, 'wallet identity differs from its on-chain binding');
  }
  let descriptor: number | undefined;
  try {
    if (!isAbsolute(config.privateKeyFile)) throw new Error('invalid key path');
    descriptor = openSync(config.privateKeyFile, constants.O_RDONLY | constants.O_NOFOLLOW);
    const metadata = fstatSync(descriptor);
    if (!metadata.isFile() || metadata.nlink !== 1 || (metadata.mode & 0o777) !== 0o600
      || metadata.uid !== process.getuid?.() || metadata.size < 1 || metadata.size > 16_384) {
      throw new Error('invalid key file');
    }
    const key = createPrivateKey(readFileSync(descriptor));
    if (key.asymmetricKeyType !== 'ec' || key.asymmetricKeyDetails?.namedCurve !== 'prime256v1') {
      throw new Error('invalid producer key');
    }
    const header = Buffer.from(JSON.stringify({ alg: 'ES256', typ: 'layerx-wallet-binding+jwt' })).toString('base64url');
    const payload = Buffer.from(JSON.stringify({
      iss: config.issuer,
      sub: userId,
      did: provision.did,
      tenant: config.tenant,
    })).toString('base64url');
    const input = `${header}.${payload}`;
    if (input.length + 87 > 4096) throw new Error('identity binding exceeds protocol bound');
    const signature = sign('sha256', Buffer.from(input, 'ascii'), { key, dsaEncoding: 'ieee-p1363' });
    if (signature.length !== 64) throw new Error('invalid producer signature');
    return `${input}.${signature.toString('base64url')}`;
  } catch {
    throw new ProvisionError('identity_binding_unavailable', 503, 'wallet identity binding signer is unavailable');
  } finally {
    if (descriptor !== undefined) closeSync(descriptor);
  }
}
