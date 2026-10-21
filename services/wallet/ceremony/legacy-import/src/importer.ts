import { closeSync, existsSync, fsyncSync, openSync, readFileSync, writeSync } from 'node:fs';
import type { Pool, PoolClient } from 'pg';

export const DEFAULT_CHAIN_ID = 125;
export const ADMIN_IMPORT_PATH = '/v1/admin/legacy-import';

export interface LegacyWallet {
  id: string;
  user_id: string;
  address: string;
  encrypted_private_key: string | null;
  key_version: number;
  chain_id: number;
  kind: string;
  funded: boolean;
  is_disabled: boolean;
  disabled_reason: string | null;
  created_at: string;
}

export interface CustodyRecord {
  legacy_wallet_id: string;
  user_id: string;
  address: string;
  kind: 'standard' | 'funded';
  chain_id: number;
  key_version: number;
  encrypted_private_key: string;
  attestor_key_id: string;
  identity_key_id: string;
  binding_state: 'unbound';
  custody: 'live' | 'archived';
  is_disabled: boolean;
  disabled_reason: string | null;
  created_at: string;
}

export type Action = 'import' | 'archive' | 'refuse' | 'already_imported';

export interface WalletPlan {
  wallet: LegacyWallet;
  action: Action;
  reason: string | null;
  record: CustodyRecord | null;
}

export type ApplyOutcome = 'imported' | 'archived' | 'already_imported' | 'refused' | 'failed';

export interface WalletResult {
  wallet_id: string;
  address: string;
  kind: string;
  outcome: ApplyOutcome;
  reason: string | null;
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const ADDRESS = /^0x[0-9a-fA-F]{40}$/;

export const secpKeyId = (walletId: string): string => `wallet:${walletId}:secp256k1`;
export const identityKeyId = (walletId: string): string => `wallet:${walletId}:ed25519`;

type Queryable = Pick<Pool, 'query'> | Pick<PoolClient, 'query'>;

export async function readLegacyWallets(db: Queryable): Promise<LegacyWallet[]> {
  const { rows: cols } = await db.query<{ table_name: string; column_name: string }>(
    `select table_name, column_name from information_schema.columns
      where table_schema = current_schema() and table_name in ('wallets', 'funded_accounts')`,
  );
  const have = new Set(cols.map((c) => `${c.table_name}.${c.column_name}`));
  for (const col of ['id', 'user_id', 'address', 'encrypted_private_key', 'key_version', 'chain_id', 'created_at', 'is_disabled', 'disabled_reason']) {
    if (!have.has(`wallets.${col}`)) throw new Error(`legacy schema: wallets.${col} is missing`);
  }
  const kind = have.has('wallets.kind') ? `w.kind` : `'standard'`;
  const funded = have.has('funded_accounts.wallet_id')
    ? `(${kind} = 'funded' or exists (select 1 from funded_accounts f where f.wallet_id = w.id))`
    : `(${kind} = 'funded')`;
  const { rows } = await db.query<LegacyWallet>(
    `select w.id::text as id, w.user_id::text as user_id, w.address, w.encrypted_private_key,
            w.key_version::int as key_version, w.chain_id::int as chain_id, ${kind} as kind, ${funded} as funded,
            w.is_disabled, w.disabled_reason, to_char(w.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"') as created_at
       from wallets w order by w.created_at, w.id`,
  );
  return rows;
}

export function planWallet(w: LegacyWallet, chainId: number, journal: ReadonlySet<string>): WalletPlan {
  const refuse = (reason: string): WalletPlan => ({ wallet: w, action: 'refuse', reason, record: null });
  if (journal.has(w.id)) return { wallet: w, action: 'already_imported', reason: 'wallet is in the import journal', record: null };
  if (!UUID.test(w.id) || !UUID.test(w.user_id)) return refuse('wallet or user id is not a uuid');
  if (!ADDRESS.test(w.address)) return refuse('address is not a 20-byte hex address');
  if (w.chain_id !== chainId) return refuse(`chain ${w.chain_id} is not the target chain ${chainId}`);
  if (w.kind !== 'standard' && w.kind !== 'funded') return refuse(`unknown wallet kind ${w.kind}`);
  if (w.encrypted_private_key === null || w.encrypted_private_key.trim() === '') return refuse('no key ciphertext');
  if (!Number.isInteger(w.key_version) || w.key_version < 1) return refuse('key version is not a positive integer');
  const funded = w.funded || w.kind === 'funded';
  const record: CustodyRecord = {
    legacy_wallet_id: w.id,
    user_id: w.user_id,
    address: w.address.toLowerCase(),
    kind: funded ? 'funded' : 'standard',
    chain_id: w.chain_id,
    key_version: w.key_version,
    encrypted_private_key: w.encrypted_private_key,
    attestor_key_id: secpKeyId(w.id),
    identity_key_id: identityKeyId(w.id),
    binding_state: 'unbound',
    custody: funded ? 'archived' : 'live',
    is_disabled: w.is_disabled,
    disabled_reason: w.disabled_reason,
    created_at: w.created_at,
  };
  return { wallet: w, action: funded ? 'archive' : 'import', reason: funded ? 'funded wallet is archived out of the live path' : null, record };
}

export function planAll(wallets: LegacyWallet[], chainId: number, journal: ReadonlySet<string>): WalletPlan[] {
  const seen = new Map<string, string>();
  return wallets.map((w) => {
    const p = planWallet(w, chainId, journal);
    const key = `${w.address.toLowerCase()}:${w.kind}`;
    const prior = seen.get(key);
    if (p.record !== null && prior !== undefined) {
      return { wallet: w, action: 'refuse', reason: `address already planned for wallet ${prior}`, record: null };
    }
    if (p.record !== null) seen.set(key, w.id);
    return p;
  });
}

export function reportLine(p: WalletPlan): string {
  return JSON.stringify({
    wallet_id: p.wallet.id,
    address: p.wallet.address.toLowerCase(),
    kind: p.wallet.funded ? 'funded' : p.wallet.kind,
    action: p.action,
    attestor_key_id: p.record?.attestor_key_id ?? null,
    custody: p.record?.custody ?? null,
    disabled: p.wallet.is_disabled,
    key_ciphertext: p.wallet.encrypted_private_key ? 'present' : 'absent',
    reason: p.reason,
  });
}

export function summary(plans: WalletPlan[]): Record<Action, number> & { total: number } {
  const out = { total: plans.length, import: 0, archive: 0, refuse: 0, already_imported: 0 };
  for (const p of plans) out[p.action]++;
  return out;
}

export class Journal {
  readonly ids = new Set<string>();

  constructor(readonly path: string) {
    if (!existsSync(path)) return;
    for (const line of readFileSync(path, 'utf8').split('\n')) {
      if (line.trim() === '') continue;
      const entry = JSON.parse(line) as { wallet_id?: unknown };
      if (typeof entry.wallet_id !== 'string') throw new Error(`journal ${path} holds an entry without wallet_id`);
      this.ids.add(entry.wallet_id);
    }
  }

  record(walletId: string, outcome: ApplyOutcome): void {
    const fd = openSync(this.path, 'a', 0o600);
    try {
      writeSync(fd, `${JSON.stringify({ wallet_id: walletId, outcome, at: new Date().toISOString() })}\n`);
      fsyncSync(fd);
    } finally {
      closeSync(fd);
    }
    this.ids.add(walletId);
  }
}

export interface AdminClient {
  url: string;
  token: string;
}

export async function applyPlans(plans: WalletPlan[], admin: AdminClient, journal: Journal): Promise<WalletResult[]> {
  const endpoint = new URL(ADMIN_IMPORT_PATH, admin.url).toString();
  const results: WalletResult[] = [];
  for (const p of plans) {
    const base = { wallet_id: p.wallet.id, address: p.wallet.address.toLowerCase(), kind: p.record?.kind ?? p.wallet.kind };
    if (journal.ids.has(p.wallet.id)) {
      results.push({ ...base, outcome: 'already_imported', reason: 'wallet is in the import journal' });
      continue;
    }
    if (p.record === null) {
      results.push({ ...base, outcome: p.action === 'already_imported' ? 'already_imported' : 'refused', reason: p.reason });
      continue;
    }
    let res: Response;
    try {
      res = await fetch(endpoint, {
        method: 'POST',
        headers: { 'content-type': 'application/json', authorization: `Bearer ${admin.token}` },
        body: JSON.stringify(p.record),
      });
    } catch (err) {
      results.push({ ...base, outcome: 'failed', reason: `gateway unreachable: ${(err as Error).message}` });
      continue;
    }
    const text = await res.text();
    if (res.status === 409) {
      journal.record(p.wallet.id, 'already_imported');
      results.push({ ...base, outcome: 'already_imported', reason: 'gateway already holds this wallet' });
      continue;
    }
    if (!res.ok) {
      results.push({ ...base, outcome: 'failed', reason: `gateway answered ${res.status}: ${text.slice(0, 200)}` });
      continue;
    }
    const outcome: ApplyOutcome = p.record.custody === 'archived' ? 'archived' : 'imported';
    journal.record(p.wallet.id, outcome);
    results.push({ ...base, outcome, reason: null });
  }
  return results;
}
