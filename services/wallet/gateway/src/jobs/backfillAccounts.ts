import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import type { ProvisionDeps } from '../provision/state.js';
import type { BackfillOutcome } from '../provision/backfill.js';

export const BACKFILL_OUTCOMES: readonly BackfillOutcome[] = [
  'bound',
  'awaiting_owner',
  'awaiting_agent_signature',
  'refused',
  'skipped',
  'failed',
];

export const DEFAULT_BATCH_SIZE = 100;
export const DEFAULT_MAX_BATCHES = 10_000;
const CURSOR_NAME = 'unified_account';

export interface BackfillCommandOptions {
  batchSize: number;
  maxBatches: number;
}

export interface BackfillCommandResult {
  totals: Record<BackfillOutcome, number>;
  batches: number;
  done: boolean;
}

export class BackfillUsageError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'BackfillUsageError';
  }
}

function positiveInteger(flag: string, raw: string | undefined): number {
  if (raw === undefined || !/^[1-9][0-9]*$/.test(raw)) throw new BackfillUsageError(`${flag} takes a positive integer`);
  const value = Number(raw);
  if (!Number.isSafeInteger(value)) throw new BackfillUsageError(`${flag} is out of range`);
  return value;
}

export function parseBackfillArgs(argv: readonly string[]): BackfillCommandOptions {
  const opts: BackfillCommandOptions = { batchSize: DEFAULT_BATCH_SIZE, maxBatches: DEFAULT_MAX_BATCHES };
  const args = argv[0] === '--' ? argv.slice(1) : argv;
  for (let i = 0; i < args.length; i++) {
    const arg = args[i]!;
    const eq = arg.indexOf('=');
    const flag = eq === -1 ? arg : arg.slice(0, eq);
    const inline = eq === -1 ? undefined : arg.slice(eq + 1);
    if (flag !== '--batch-size' && flag !== '--max-batches') throw new BackfillUsageError(`unknown argument ${flag}`);
    const raw = inline ?? args[++i];
    const value = positiveInteger(flag, raw);
    if (flag === '--batch-size') {
      if (value > 500) throw new BackfillUsageError('--batch-size must be between 1 and 500');
      opts.batchSize = value;
    } else {
      opts.maxBatches = value;
    }
  }
  return opts;
}

export async function runBackfill(deps: ProvisionDeps, opts: BackfillCommandOptions): Promise<BackfillCommandResult> {
  const { backfillAccounts } = await import('../provision/backfill.js');
  const totals = Object.fromEntries(BACKFILL_OUTCOMES.map((o) => [o, 0])) as Record<BackfillOutcome, number>;
  let batches = 0;
  let done = false;
  while (batches < opts.maxBatches) {
    const report = await backfillAccounts(deps, { batchSize: opts.batchSize, maxBatches: 1, cursorName: CURSOR_NAME });
    batches += 1;
    for (const o of report.outcomes) totals[o.outcome] += 1;
    if (report.done) {
      done = true;
      break;
    }
  }
  if (done) {
    await deps.pool.query(
      `update account_backfill_cursor set last_wallet_id = null, updated_at = now() where name = $1`,
      [CURSOR_NAME],
    );
  }
  return { totals, batches, done };
}

export function totalsLines(result: BackfillCommandResult): string[] {
  return [
    ...BACKFILL_OUTCOMES.map((o) => `backfill total outcome=${o} count=${result.totals[o]}`),
    `backfill batches=${result.batches} done=${result.done}`,
  ];
}

function failureName(err: unknown): string {
  if (!(err instanceof Error)) return 'error';
  const code = (err as { code?: unknown }).code;
  return typeof code === 'string' ? `${err.name} ${code}` : err.name;
}

export async function main(argv: readonly string[]): Promise<number> {
  let opts: BackfillCommandOptions;
  try {
    opts = parseBackfillArgs(argv);
  } catch (err) {
    if (!(err instanceof BackfillUsageError)) throw err;
    process.stderr.write(`backfill: ${err.message}\nusage: backfill:accounts [--batch-size N] [--max-batches N]\n`);
    return 2;
  }
  const { provisionDepsFromEnv } = await import('../routes/wallet.js');
  const { closePool } = await import('../db/pool.js');
  const deps = provisionDepsFromEnv();
  if (!deps || !deps.attestors) {
    process.stderr.write('backfill: refusing to start without the attestor configuration (ATTESTOR_ENDPOINTS and its TLS files)\n');
    await closePool();
    return 1;
  }
  try {
    const result = await runBackfill(deps, opts);
    process.stdout.write(`${totalsLines(result).join('\n')}\n`);
    return result.done ? 0 : 3;
  } catch (err) {
    process.stderr.write(`backfill: stopped: ${failureName(err)}\n`);
    return 1;
  } finally {
    deps.attestors.close();
    deps.rpc.stop();
    await closePool();
  }
}

const invoked = process.argv[1] ? resolve(process.argv[1]) : '';
if (invoked === fileURLToPath(import.meta.url)) {
  main(process.argv.slice(2)).then(
    (code) => process.exit(code),
    (err: unknown) => {
      process.stderr.write(`backfill: stopped: ${failureName(err)}\n`);
      process.exit(1);
    },
  );
}
