import { getAddress } from 'viem';
import { ProvisionError, ProvisionRefusedError, provisionAccount, type ProvisionDeps, type ProvisionSubject } from './state.js';

export type BackfillOutcome =
  | 'bound'
  | 'awaiting_owner'
  | 'awaiting_agent_signature'
  | 'refused'
  | 'skipped'
  | 'failed';

export interface BackfillWalletOutcome {
  wallet_id: string;
  address: `0x${string}`;
  kind: string;
  outcome: BackfillOutcome;
  did: string | null;
  main_account_id: string | null;
  reason: string | null;
}

export interface BackfillOptions {
  batchSize: number;
  maxBatches: number;
  cursorName?: string;
}

export interface BackfillReport {
  outcomes: BackfillWalletOutcome[];
  cursor: string | null;
  done: boolean;
}

interface Candidate {
  id: string;
  user_id: string;
  address: string;
  kind: string;
  principal_did: string | null;
}

export async function backfillAccounts(deps: ProvisionDeps, opts: BackfillOptions): Promise<BackfillReport> {
  if (!Number.isInteger(opts.batchSize) || opts.batchSize < 1 || opts.batchSize > 500) {
    throw new Error('backfill batch size must be between 1 and 500');
  }
  if (!Number.isInteger(opts.maxBatches) || opts.maxBatches < 1) throw new Error('backfill needs at least one batch');
  const name = opts.cursorName ?? 'unified_account';
  await deps.pool.query(`insert into account_backfill_cursor (name) values ($1) on conflict (name) do nothing`, [name]);
  const { rows: cursorRows } = await deps.pool.query<{ last_wallet_id: string | null }>(
    `select last_wallet_id::text from account_backfill_cursor where name = $1`,
    [name],
  );
  let cursor = cursorRows[0]?.last_wallet_id ?? null;
  const outcomes: BackfillWalletOutcome[] = [];
  let done = false;

  for (let batch = 0; batch < opts.maxBatches; batch++) {
    const { rows } = await deps.pool.query<Candidate>(
      `select w.id::text, w.user_id::text, w.address, w.kind, p.did as principal_did
         from wallets w
         left join agent_principals p on p.wallet_id = w.id
        where w.archived_at is null
          and w.did is null
          and (w.migrated_at is not null or w.kind = 'agent')
          and ($1::uuid is null or w.id > $1::uuid)
        order by w.id
        limit $2`,
      [cursor, opts.batchSize],
    );
    for (const w of rows) {
      outcomes.push(await backfillOne(deps, w));
      cursor = w.id;
      await deps.pool.query(
        `update account_backfill_cursor set last_wallet_id = $2, updated_at = now() where name = $1`,
        [name, cursor],
      );
    }
    if (rows.length < opts.batchSize) {
      done = true;
      break;
    }
  }
  return { outcomes, cursor, done };
}

async function backfillOne(deps: ProvisionDeps, w: Candidate): Promise<BackfillWalletOutcome> {
  const base = { wallet_id: w.id, address: getAddress(w.address), kind: w.kind };
  let outcome: BackfillWalletOutcome;
  if (w.kind === 'agent' && !w.principal_did) {
    outcome = { ...base, outcome: 'skipped', did: null, main_account_id: null, reason: 'agent wallet has no registered principal' };
  } else {
    const subject: ProvisionSubject =
      w.kind === 'agent' ? { kind: 'agent', did: w.principal_did! } : { kind: 'standard', userId: w.user_id, token: null };
    try {
      const r = await provisionAccount(deps, subject);
      const mapped: BackfillOutcome =
        r.state === 'active' ? 'bound' : r.awaiting === 'agent_signature' ? 'awaiting_agent_signature' : 'awaiting_owner';
      outcome = { ...base, outcome: mapped, did: r.did, main_account_id: r.mainAccountId, reason: null };
    } catch (err) {
      if (err instanceof ProvisionRefusedError) {
        outcome = { ...base, outcome: 'refused', did: null, main_account_id: null, reason: err.message };
      } else {
        const code = err instanceof ProvisionError ? err.code : (err as Error).name;
        outcome = { ...base, outcome: 'failed', did: null, main_account_id: null, reason: code };
      }
    }
  }
  await deps.pool.query(
    `insert into account_setup_audit (wallet_id, address, event, outcome, reason) values ($1, $2, 'backfill', $3, $4)`,
    [w.id, w.address.toLowerCase(), outcome.outcome, outcome.reason],
  );
  return outcome;
}
