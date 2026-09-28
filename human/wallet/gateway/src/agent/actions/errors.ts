/**
 * Interpretation-free error envelope for the agent durable-action lane.
 *
 * Every unsuccessful step returns this exact shape. It answers, without the
 * agent having to parse a prose message: what failed, at which phase, whether
 * anything was broadcast (tx_hash + nonce), whether a retry is safe, whether to
 * RESUBMIT vs only POLL, how long to wait, whether the wallet can self-heal,
 * and the exact next action for the agent.
 *
 * Generic strings like `send_failed` / `not_found` / `nonce gap` must NEVER
 * reach an agent without this context — the orchestrator always wraps them.
 */

/** The sub-step an error occurred in. Mirrors the state-machine transitions. */
export type ActionPhase =
  | 'intake'
  | 'validation'
  | 'balance_check'
  | 'allowance_check'
  | 'approval_broadcast'
  | 'approval_confirmation'
  | 'call_simulation'
  | 'call_broadcast'
  | 'call_confirmation'
  | 'credit_verification'
  | 'reconciliation';

/**
 * The four — and only four — retry strategies. The agent NEVER independently
 * decides to replace a transaction; that authority lives entirely in the
 * wallet. The strategy tells the agent exactly what it may do next.
 */
export type RetryStrategy =
  /** Policy denial, invalid amount, forbidden destination. Do not retry. */
  | 'NEVER'
  /** A tx was broadcast. Poll GET /actions/:id — never resubmit. */
  | 'POLL_ACTION'
  /** Transient pre-broadcast failure. The wallet retries internally under the
   *  SAME action; the agent just polls. */
  | 'RETRY_SAME_ACTION'
  /** Frozen wallet, insufficient funds, policy change required. */
  | 'USER_INTERVENTION';

/** Stable machine-readable error codes. */
export type ActionErrorCode =
  // NEVER
  | 'POLICY_DENIED'
  | 'INVALID_REQUEST'
  | 'INVALID_AMOUNT'
  | 'FORBIDDEN_DESTINATION'
  | 'SIMULATION_REVERTED'
  | 'TRANSACTION_REVERTED'
  // USER_INTERVENTION
  | 'AGENT_FROZEN'
  | 'INSUFFICIENT_BALANCE'
  | 'INSUFFICIENT_GAS'
  | 'APPROVAL_CAP_EXCEEDED'
  // POLL_ACTION (something is broadcast)
  | 'APPROVAL_PENDING'
  | 'CALL_PENDING'
  | 'RECONCILING'
  // RETRY_SAME_ACTION (transient, pre-broadcast)
  | 'RPC_UNAVAILABLE'
  | 'NONCE_WEDGED'
  | 'FEE_TOO_LOW';

const STRATEGY: Record<ActionErrorCode, RetryStrategy> = {
  POLICY_DENIED: 'NEVER',
  INVALID_REQUEST: 'NEVER',
  INVALID_AMOUNT: 'NEVER',
  FORBIDDEN_DESTINATION: 'NEVER',
  SIMULATION_REVERTED: 'NEVER',
  TRANSACTION_REVERTED: 'NEVER',

  AGENT_FROZEN: 'USER_INTERVENTION',
  INSUFFICIENT_BALANCE: 'USER_INTERVENTION',
  INSUFFICIENT_GAS: 'USER_INTERVENTION',
  APPROVAL_CAP_EXCEEDED: 'USER_INTERVENTION',

  APPROVAL_PENDING: 'POLL_ACTION',
  CALL_PENDING: 'POLL_ACTION',
  RECONCILING: 'POLL_ACTION',

  RPC_UNAVAILABLE: 'RETRY_SAME_ACTION',
  NONCE_WEDGED: 'RETRY_SAME_ACTION',
  FEE_TOO_LOW: 'RETRY_SAME_ACTION',
};

/** Default backoff (ms) the agent should wait before its next poll/retry. */
const AFTER_MS: Record<RetryStrategy, number | null> = {
  NEVER: null,
  POLL_ACTION: 1_000,
  RETRY_SAME_ACTION: 2_000,
  USER_INTERVENTION: null,
};

export interface ErrorCause {
  tx_hash?: string | null;
  nonce?: number | null;
  detail?: string | null;
  [k: string]: unknown;
}

export interface ErrorEnvelope {
  ok: false;
  action_id: string;
  phase: ActionPhase;
  code: ActionErrorCode;
  message: string;
  cause: ErrorCause;
  retry: {
    allowed: boolean;
    strategy: RetryStrategy;
    after_ms: number | null;
    reuse_idempotency_key: boolean;
    must_not_resubmit: boolean;
  };
  remedy: {
    action: 'GET_ACTION' | 'NONE' | 'OWNER_INTERVENTION';
    endpoint: string | null;
  };
}

export function retryStrategyFor(code: ActionErrorCode): RetryStrategy {
  return STRATEGY[code];
}

/**
 * Build the interpretation-free envelope for an unsuccessful step. The strategy
 * is derived from the code (never hand-passed) so the code↔strategy contract
 * can't drift between call sites.
 */
export function buildErrorEnvelope(args: {
  actionId: string;
  phase: ActionPhase;
  code: ActionErrorCode;
  message: string;
  cause?: ErrorCause;
}): ErrorEnvelope {
  const strategy = STRATEGY[args.code];
  const broadcast = strategy === 'POLL_ACTION';
  const allowed = strategy !== 'NEVER';
  const endpoint = `/v1/agent/actions/${args.actionId}`;

  return {
    ok: false,
    action_id: args.actionId,
    phase: args.phase,
    code: args.code,
    message: args.message,
    cause: args.cause ?? {},
    retry: {
      allowed,
      strategy,
      after_ms: AFTER_MS[strategy],
      // Resubmitting the same request is always safe (idempotent) but for
      // POLL_ACTION it must be a POLL, never a resend.
      reuse_idempotency_key: true,
      must_not_resubmit: broadcast,
    },
    remedy: {
      action:
        strategy === 'USER_INTERVENTION'
          ? 'OWNER_INTERVENTION'
          : strategy === 'NEVER'
            ? 'NONE'
            : 'GET_ACTION',
      endpoint: strategy === 'NEVER' ? null : endpoint,
    },
  };
}

/** The terminal `status` an error code maps a non-terminal action into. */
export function terminalStatusFor(code: ActionErrorCode): string | null {
  switch (code) {
    case 'POLICY_DENIED':
      return 'policy_denied';
    case 'INVALID_REQUEST':
    case 'INVALID_AMOUNT':
    case 'FORBIDDEN_DESTINATION':
      return 'invalid_request';
    case 'SIMULATION_REVERTED':
      return 'simulation_reverted';
    case 'TRANSACTION_REVERTED':
      return 'transaction_reverted';
    case 'INSUFFICIENT_BALANCE':
    case 'INSUFFICIENT_GAS':
      return 'insufficient_balance';
    // POLL_ACTION / RETRY_SAME_ACTION / USER_INTERVENTION codes are NOT
    // terminal — the action stays in-flight (or awaits the owner).
    default:
      return null;
  }
}
