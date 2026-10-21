#!/usr/bin/env node
import { parseArgs } from 'node:util';
import pg from 'pg';
import { DEFAULT_CHAIN_ID, Journal, applyPlans, planAll, readLegacyWallets, reportLine, summary } from './importer.js';

const USAGE = `usage: legacy-import (--dry-run | --apply) --journal <path> [--chain-id <id>]
  env LEGACY_DATABASE_URL   legacy wallet Postgres (read only)
  env GATEWAY_ADMIN_URL     gateway base URL (--apply)
  env GATEWAY_ADMIN_TOKEN   gateway admin bearer token (--apply)`;

async function main(): Promise<number> {
  const { values } = parseArgs({
    options: {
      'dry-run': { type: 'boolean', default: false },
      apply: { type: 'boolean', default: false },
      journal: { type: 'string' },
      'chain-id': { type: 'string', default: String(DEFAULT_CHAIN_ID) },
    },
  });
  if (values['dry-run'] === values.apply || !values.journal) {
    process.stderr.write(`${USAGE}\n`);
    return 2;
  }
  const chainId = Number(values['chain-id']);
  if (!Number.isInteger(chainId) || chainId < 1) throw new Error('--chain-id must be a positive integer');
  const dsn = process.env.LEGACY_DATABASE_URL?.trim();
  if (!dsn) throw new Error('LEGACY_DATABASE_URL is not set');
  const admin = values.apply
    ? { url: process.env.GATEWAY_ADMIN_URL?.trim() ?? '', token: process.env.GATEWAY_ADMIN_TOKEN?.trim() ?? '' }
    : null;
  if (admin && (!admin.url || !admin.token)) throw new Error('--apply needs GATEWAY_ADMIN_URL and GATEWAY_ADMIN_TOKEN');

  const journal = new Journal(values.journal);
  const pool = new pg.Pool({ connectionString: dsn, max: 1 });
  let wallets;
  try {
    const client = await pool.connect();
    try {
      await client.query('begin transaction read only');
      wallets = await readLegacyWallets(client);
      await client.query('commit');
    } finally {
      client.release();
    }
  } finally {
    await pool.end();
  }
  const plans = planAll(wallets, chainId, journal.ids);
  for (const p of plans) process.stdout.write(`${reportLine(p)}\n`);
  process.stdout.write(`${JSON.stringify({ summary: summary(plans) })}\n`);
  if (!admin) return 0;

  const results = await applyPlans(plans, admin, journal);
  for (const r of results) process.stdout.write(`${JSON.stringify({ result: r })}\n`);
  return results.some((r) => r.outcome === 'failed') ? 1 : 0;
}

main().then(
  (code) => process.exit(code),
  (err) => {
    process.stderr.write(`legacy-import: ${(err as Error).message}\n`);
    process.exit(1);
  },
);
