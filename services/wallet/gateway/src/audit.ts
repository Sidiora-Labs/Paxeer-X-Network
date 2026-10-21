import type { Pool } from 'pg';
import type { NodeAudit } from './attestor/client.js';

export type AuditKind = 'transaction' | 'message' | 'typed_data';
export type AuditPath = 'attestor' | 'envelope' | 'none';
export type AuditDecision = 'signed' | 'refused' | 'broadcast' | 'broadcast_failed';

export interface SigningAuditEntry {
  requestId: string;
  clientSubject: string;
  account: string | null;
  walletId: string | null;
  route: string;
  kind: AuditKind;
  path: AuditPath;
  decision: AuditDecision;
  reasonCode: string | null;
  requestHash: string;
  sessionId: string | null;
  attestorAudit: NodeAudit[];
  txHash: string | null;
  nonce: number | null;
}

export interface SigningAuditRow {
  id: string;
  request_id: string;
  client_subject: string;
  account: string | null;
  wallet_id: string | null;
  route: string;
  kind: AuditKind;
  path: AuditPath;
  decision: AuditDecision;
  reason_code: string | null;
  request_hash: string;
  session_id: string | null;
  attestor_audit: NodeAudit[];
  tx_hash: string | null;
  nonce: string | null;
}

export async function recordSigningAudit(pool: Pool, entry: SigningAuditEntry): Promise<void> {
  await pool.query(
    `insert into signing_audit
       (request_id, client_subject, account, wallet_id, route, kind, path, decision,
        reason_code, request_hash, session_id, attestor_audit, tx_hash, nonce)
     values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13, $14)`,
    [
      entry.requestId,
      entry.clientSubject,
      entry.account ? entry.account.toLowerCase() : null,
      entry.walletId,
      entry.route,
      entry.kind,
      entry.path,
      entry.decision,
      entry.reasonCode,
      entry.requestHash,
      entry.sessionId,
      JSON.stringify(entry.attestorAudit),
      entry.txHash,
      entry.nonce,
    ],
  );
}

export async function auditRowsForRequest(pool: Pool, requestId: string): Promise<SigningAuditRow[]> {
  const { rows } = await pool.query<SigningAuditRow>(
    `select id::text, request_id::text, client_subject, account, wallet_id::text, route, kind, path,
            decision, reason_code, request_hash, session_id::text, attestor_audit, tx_hash, nonce::text
       from signing_audit
      where request_id = $1
      order by id`,
    [requestId],
  );
  return rows;
}

export type RateLimitScope = 'client' | 'account';

export class RateLimitedError extends Error {
  readonly scope: RateLimitScope;
  readonly limit: number;
  readonly retryAfterSeconds: number;
  constructor(scope: RateLimitScope, limit: number, retryAfterSeconds: number) {
    super(`rate limit: more than ${limit} signing requests per minute for this ${scope}`);
    this.name = 'RateLimitedError';
    this.scope = scope;
    this.limit = limit;
    this.retryAfterSeconds = retryAfterSeconds;
  }
}

export function rateLimitBody(err: RateLimitedError): {
  error: 'rate_limited';
  scope: RateLimitScope;
  limit_per_minute: number;
  retry_after_seconds: number;
  message: string;
} {
  return {
    error: 'rate_limited',
    scope: err.scope,
    limit_per_minute: err.limit,
    retry_after_seconds: err.retryAfterSeconds,
    message: err.message,
  };
}

export interface RateLimiterOptions {
  pool: Pool;
  clientPerMinute: number;
  accountPerMinute: number;
}

export class RateLimiter {
  constructor(private readonly opts: RateLimiterOptions) {}

  async consume(scope: RateLimitScope, subject: string): Promise<void> {
    const limit = scope === 'client' ? this.opts.clientPerMinute : this.opts.accountPerMinute;
    const { rows } = await this.opts.pool.query<{ hits: number; retry_after: number }>(
      `insert into rate_limit_windows (scope, subject, window_start, hits)
       values ($1, $2, date_trunc('minute', now()), 1)
       on conflict (scope, subject, window_start)
         do update set hits = rate_limit_windows.hits + 1
       returning hits,
                 greatest(1, ceil(extract(epoch from (window_start + interval '1 minute' - now()))))::int
                   as retry_after`,
      [scope, scope === 'account' ? subject.toLowerCase() : subject],
    );
    const row = rows[0];
    if (!row) throw new Error('rate limiter: counter upsert returned no row');
    if (row.hits > limit) throw new RateLimitedError(scope, limit, row.retry_after);
  }
}
