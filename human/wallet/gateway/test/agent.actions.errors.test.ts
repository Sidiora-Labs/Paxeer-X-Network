import { describe, it, expect } from 'vitest';
import {
  buildErrorEnvelope,
  retryStrategyFor,
  terminalStatusFor,
  type ActionErrorCode,
} from '../src/agent/actions/errors.js';

/**
 * The interpretation-free error envelope is the agent-facing contract. These
 * lock the code↔strategy mapping, the must_not_resubmit safety flag, and the
 * remedy shape so a refactor can't silently tell an agent to resend a
 * broadcast tx.
 */

const ALL_CODES: ActionErrorCode[] = [
  'POLICY_DENIED',
  'INVALID_REQUEST',
  'INVALID_AMOUNT',
  'FORBIDDEN_DESTINATION',
  'SIMULATION_REVERTED',
  'TRANSACTION_REVERTED',
  'AGENT_FROZEN',
  'INSUFFICIENT_BALANCE',
  'INSUFFICIENT_GAS',
  'APPROVAL_CAP_EXCEEDED',
  'APPROVAL_PENDING',
  'CALL_PENDING',
  'RECONCILING',
  'RPC_UNAVAILABLE',
  'NONCE_WEDGED',
  'FEE_TOO_LOW',
];

describe('action error envelope', () => {
  it('maps every code to exactly one of the four retry strategies', () => {
    const strategies = new Set(['NEVER', 'POLL_ACTION', 'RETRY_SAME_ACTION', 'USER_INTERVENTION']);
    for (const code of ALL_CODES) {
      expect(strategies.has(retryStrategyFor(code))).toBe(true);
    }
  });

  it('NEVER codes forbid retry and expose no remedy endpoint', () => {
    const env = buildErrorEnvelope({
      actionId: 'act_0123456789abcdef01234567',
      phase: 'validation',
      code: 'POLICY_DENIED',
      message: 'denied by policy',
    });
    expect(env.ok).toBe(false);
    expect(env.retry.strategy).toBe('NEVER');
    expect(env.retry.allowed).toBe(false);
    expect(env.retry.must_not_resubmit).toBe(false);
    expect(env.retry.after_ms).toBeNull();
    expect(env.remedy.action).toBe('NONE');
    expect(env.remedy.endpoint).toBeNull();
  });

  it('POLL_ACTION codes forbid resubmission and point at the poll endpoint', () => {
    const id = 'act_0123456789abcdef01234567';
    const env = buildErrorEnvelope({
      actionId: id,
      phase: 'approval_confirmation',
      code: 'APPROVAL_PENDING',
      message: 'approval broadcast, not yet confirmed',
      cause: { tx_hash: '0xdead', nonce: 7 },
    });
    expect(env.retry.strategy).toBe('POLL_ACTION');
    expect(env.retry.allowed).toBe(true);
    expect(env.retry.must_not_resubmit).toBe(true);
    expect(env.retry.reuse_idempotency_key).toBe(true);
    expect(env.remedy.action).toBe('GET_ACTION');
    expect(env.remedy.endpoint).toBe(`/v1/agent/actions/${id}`);
    expect(env.cause.tx_hash).toBe('0xdead');
    expect(env.cause.nonce).toBe(7);
  });

  it('RETRY_SAME_ACTION codes allow retry without resubmission', () => {
    const env = buildErrorEnvelope({
      actionId: 'act_0123456789abcdef01234567',
      phase: 'reconciliation',
      code: 'RPC_UNAVAILABLE',
      message: 'rpc hiccup',
    });
    expect(env.retry.strategy).toBe('RETRY_SAME_ACTION');
    expect(env.retry.allowed).toBe(true);
    expect(env.retry.must_not_resubmit).toBe(false);
    expect(env.remedy.action).toBe('GET_ACTION');
  });

  it('USER_INTERVENTION codes route to the owner', () => {
    const env = buildErrorEnvelope({
      actionId: 'act_0123456789abcdef01234567',
      phase: 'balance_check',
      code: 'INSUFFICIENT_BALANCE',
      message: 'not enough USDL',
    });
    expect(env.retry.strategy).toBe('USER_INTERVENTION');
    expect(env.remedy.action).toBe('OWNER_INTERVENTION');
  });

  it('only genuinely-terminal codes produce a terminal status', () => {
    expect(terminalStatusFor('POLICY_DENIED')).toBe('policy_denied');
    expect(terminalStatusFor('INVALID_AMOUNT')).toBe('invalid_request');
    expect(terminalStatusFor('SIMULATION_REVERTED')).toBe('simulation_reverted');
    expect(terminalStatusFor('TRANSACTION_REVERTED')).toBe('transaction_reverted');
    expect(terminalStatusFor('INSUFFICIENT_BALANCE')).toBe('insufficient_balance');
    // In-flight / retryable codes must NOT terminate the action.
    expect(terminalStatusFor('APPROVAL_PENDING')).toBeNull();
    expect(terminalStatusFor('RPC_UNAVAILABLE')).toBeNull();
    expect(terminalStatusFor('AGENT_FROZEN')).toBeNull();
  });
});
