/**
 * Vitest setup file — runs before every test file is imported.
 *
 * env.ts validates `process.env` against its Zod schema at module load and
 * calls `process.exit(1)` on failure. That would kill the vitest worker before
 * any test could mount. We supply a minimal-but-valid env here so that any
 * module that transitively imports `env` (which is most of `src/`) loads
 * cleanly without anyone having to source `.env` in the shell.
 *
 * These values are NOT real secrets. They satisfy the schema and never touch
 * the network — every test must use mocks / fakes for IO.
 */
import { randomBytes } from 'node:crypto';

process.env.NODE_ENV ??= 'test';
process.env.SUPABASE_URL ??= 'https://supabase.test.invalid';
process.env.DATABASE_URL ??= 'postgres://localhost:5432/test';
process.env.HYPERPAXEER_RPC_URL ??= 'http://localhost:0/rpc';
process.env.WALLET_MASTER_KEY ??= randomBytes(32).toString('base64');
process.env.CORS_ORIGINS ??= 'http://localhost:3000';
