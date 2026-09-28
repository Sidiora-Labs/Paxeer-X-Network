import { readdirSync, readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join, resolve } from 'node:path';
import { getPool } from './pool.js';

/**
 * Minimal migration runner.
 *
 * Walks `gateway/migrations/*.sql` in filename order, applies anything that
 * hasn't been applied, and records each filename in the `_migrations` table.
 * Idempotent: safe to run every boot.
 *
 * Concurrency safety (the lesson behind migration 003):
 *
 *   When the API boots in cluster mode (API_WORKERS=12 on prod), every
 *   worker enters `runMigrations()` simultaneously. Without coordination,
 *   they all read an empty/stale `_migrations` table, all decide the same
 *   files are pending, all execute the SQL, and 11 of 12 crash on the
 *   ledger insert's primary-key violation — by which time the seed rows
 *   have already been written 12 times. (UNIQUE constraints with
 *   NULLable columns make this worse: NULL != NULL by default, so
 *   `on conflict do nothing` doesn't dedupe wildcard rows.)
 *
 *   Fix: take a Postgres session-scoped advisory lock on a constant key
 *   BEFORE checking the ledger. The first worker to call `pg_advisory_lock`
 *   wins; the others block until release. When they resume they find
 *   every migration already applied and skip cleanly. The lock auto-
 *   releases when the client disconnects, so a crashed leader doesn't
 *   wedge the cluster.
 *
 *   The lock is held on a DEDICATED client we explicitly check out from
 *   the pool, not on a pooled query. If we used `pool.query()` the client
 *   would be returned to the pool with the lock still attached and the
 *   next caller would inherit it — a footgun.
 *
 * Why not a library (node-pg-migrate / drizzle / etc): we have exactly one
 * database with a tiny schema that we fully own. A ~60-line runner removes
 * a dependency, reads identically across every runtime, and is trivial to
 * audit.
 */
const MIGRATION_LOCK_KEY = 'paxeer:migrations';

export async function runMigrations(): Promise<void> {
  const migrationsDir = resolveMigrationsDir();
  const pool = getPool();
  const client = await pool.connect();

  try {
    // Block until we hold the lock. With ≤12 workers and ≤100ms per
    // migration, the tail of the convoy waits at most ~1.2s on a fresh
    // boot — acceptable.
    await client.query(`select pg_advisory_lock(hashtextextended($1, 0))`, [MIGRATION_LOCK_KEY]);

    try {
      // Bootstrap the ledger table itself (must run outside the per-file
      // loop). Idempotent.
      await client.query(`
        create table if not exists _migrations (
          filename   text primary key,
          applied_at timestamptz not null default now()
        )
      `);

      const files = readdirSync(migrationsDir)
        .filter((f) => f.endsWith('.sql'))
        .sort(); // lexical order → 001_x before 002_x

      for (const file of files) {
        const already = await client.query<{ filename: string }>(
          'select filename from _migrations where filename = $1',
          [file],
        );
        if (already.rowCount && already.rowCount > 0) continue;

        const sql = readFileSync(join(migrationsDir, file), 'utf8');

        // Each migration file is run as a single round-trip. node-postgres
        // allows multi-statement text in a simple query. We DON'T wrap in an
        // explicit transaction here — several schema statements (e.g.
        // CREATE INDEX CONCURRENTLY, or future DATABASE-level settings)
        // can't run inside a transaction block, and our migrations are
        // small enough that re-runs on partial failure are fine.
        // eslint-disable-next-line no-console
        console.log(`[migrate] applying ${file}`);
        await client.query(sql);
        await client.query('insert into _migrations(filename) values ($1)', [file]);
        // eslint-disable-next-line no-console
        console.log(`[migrate] applied  ${file}`);
      }
    } finally {
      // Explicit release. The session-end fallback would also release it
      // when the client disconnects, but doing it manually makes the
      // intent obvious and gives the next worker the lock immediately.
      await client
        .query(`select pg_advisory_unlock(hashtextextended($1, 0))`, [MIGRATION_LOCK_KEY])
        .catch(() => undefined);
    }
  } finally {
    client.release();
  }
}

/**
 * Resolve the migrations directory whether we're running from `src/` (tsx dev)
 * or `dist/` (node prod). In both cases the `migrations/` folder sits two
 * levels up from `db/`.
 */
function resolveMigrationsDir(): string {
  const here = dirname(fileURLToPath(import.meta.url));
  return resolve(here, '..', '..', 'migrations');
}
