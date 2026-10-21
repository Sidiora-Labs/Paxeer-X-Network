import { query } from '../db/pool.js';
import { env } from '../env.js';

/**
 * Minimal v1 policy engine.
 *
 * Today it enforces:
 *   - per-user requests-per-minute rate limit (counts wallet_signatures)
 *   - per-tx max native value
 *   - per-user daily aggregate native value cap
 *
 * Future (v1.1+):
 *   - per-app scoping (session keys / api-key dimensioning)
 *   - destination blocklist (mixers, sanctioned)
 *   - step-up auth requirement for breaches
 *   - velocity / anomaly detection
 *
 * The engine returns a structured decision so route handlers can shape a
 * helpful 4xx response instead of just throwing.
 */

export type PolicyDecision =
  | { allow: true }
  | { allow: false; code: PolicyDenyCode; message: string };

export type PolicyDenyCode =
  | 'RATE_LIMIT'
  | 'TX_VALUE_CAP'
  | 'DAILY_VALUE_CAP'
  | 'WALLET_DISABLED';

export interface PolicyRequestContext {
  user_id: string;
  /** Native-token value of this tx in wei. 0 for non-tx signatures. */
  value_wei: bigint;
}

/**
 * Evaluate a signing request against the policy engine.
 * Reads recent activity from `wallet_signatures` for caps + rate limits.
 */
export async function evaluate(ctx: PolicyRequestContext): Promise<PolicyDecision> {
  // 1. Per-tx value cap.
  if (ctx.value_wei > env.POLICY_MAX_TX_VALUE_WEI) {
    return {
      allow: false,
      code: 'TX_VALUE_CAP',
      message: `tx value ${ctx.value_wei.toString()} wei exceeds per-tx cap ${env.POLICY_MAX_TX_VALUE_WEI.toString()} wei. Step-up auth required (coming in v1.1).`,
    };
  }

  // 2. Rate limit — count signatures in the last 60s.
  try {
    const { rows } = await query<{ count: string }>(
      `select count(*)::text as count
         from wallet_signatures
        where user_id = $1
          and created_at >= now() - interval '60 seconds'`,
      [ctx.user_id],
    );
    const recentCount = Number(rows[0]?.count ?? '0');
    if (recentCount >= env.POLICY_RATE_LIMIT_PER_MINUTE) {
      return {
        allow: false,
        code: 'RATE_LIMIT',
        message: `rate limit: more than ${env.POLICY_RATE_LIMIT_PER_MINUTE} signing requests in the last 60s`,
      };
    }
  } catch (err) {
    // Fail-open on a transient DB error for rate limiting; the per-tx cap
    // above is the stronger guard.
    // eslint-disable-next-line no-console
    console.warn(
      `[policy] rate-limit lookup failed (fail-open): ${err instanceof Error ? err.message : String(err)}`,
    );
  }

  // 3. Daily aggregate cap (native value) — aggregate in Postgres, not Node.
  if (ctx.value_wei > 0n) {
    try {
      const { rows } = await query<{ total: string }>(
        `select coalesce(sum(value_wei), 0)::text as total
           from wallet_signatures
          where user_id = $1
            and kind = 'transaction'
            and created_at >= now() - interval '24 hours'`,
        [ctx.user_id],
      );
      const dailyTotal = BigInt(rows[0]?.total ?? '0');
      if (dailyTotal + ctx.value_wei > env.POLICY_MAX_DAILY_VALUE_WEI) {
        return {
          allow: false,
          code: 'DAILY_VALUE_CAP',
          message: `daily value cap exceeded. used=${dailyTotal.toString()} request=${ctx.value_wei.toString()} cap=${env.POLICY_MAX_DAILY_VALUE_WEI.toString()}`,
        };
      }
    } catch (err) {
      // eslint-disable-next-line no-console
      console.warn(
        `[policy] daily-cap lookup failed (fail-open): ${err instanceof Error ? err.message : String(err)}`,
      );
    }
  }

  return { allow: true };
}
