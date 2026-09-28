import { Pool, type PoolClient, type QueryResult, type QueryResultRow } from 'pg';
import { env } from '../env.js';

/**
 * Process-wide Postgres connection pool.
 *
 * Why singleton: node-postgres is designed around a long-lived pool. Creating
 * one per request would exhaust connections instantly. Under Fastify with
 * reasonable concurrency, 20 pooled connections serves several hundred RPS.
 */
let pool: Pool | null = null;

export function getPool(): Pool {
  if (pool) return pool;
  pool = new Pool({
    connectionString: env.DATABASE_URL,
    max: env.DATABASE_POOL_MAX,
    // Fail fast on a truly-dead DB rather than pending forever.
    connectionTimeoutMillis: 5_000,
    idleTimeoutMillis: 30_000,
    // Statement-level cap — any single query over this means a bug or a scan
    // we don't want. Individual call sites can override with `query({ text, ... , timeout })`.
    statement_timeout: 10_000,
    // Our DB is on a private docker network. Add SSL back if pointing at a
    // managed Postgres (e.g. ssl: { rejectUnauthorized: true }).
    ssl: false,
  });

  pool.on('error', (err) => {
    // eslint-disable-next-line no-console
    console.error('[db] idle client error:', err);
  });

  return pool;
}

/**
 * Convenience: run a single parameterised query against the pool.
 * Prefer this over `pool.query` directly so every call goes through one
 * audit-friendly surface.
 */
export async function query<T extends QueryResultRow = QueryResultRow>(
  text: string,
  params?: unknown[],
): Promise<QueryResult<T>> {
  return getPool().query<T>(text, params);
}

/**
 * Run `fn` inside a transaction. Commits on success, rolls back on any throw.
 * Keeps callers from having to juggle BEGIN/COMMIT/ROLLBACK themselves.
 */
export async function withTransaction<T>(fn: (client: PoolClient) => Promise<T>): Promise<T> {
  const client = await getPool().connect();
  try {
    await client.query('BEGIN');
    const result = await fn(client);
    await client.query('COMMIT');
    return result;
  } catch (err) {
    await client.query('ROLLBACK').catch(() => undefined);
    throw err;
  } finally {
    client.release();
  }
}

/** Close the pool. Only call during graceful shutdown. */
export async function closePool(): Promise<void> {
  if (pool) {
    await pool.end();
    pool = null;
  }
}
