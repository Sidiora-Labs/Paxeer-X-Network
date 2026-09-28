import { beforeEach, describe, expect, it, vi } from 'vitest';

/**
 * Agent policy engine (v2) unit tests — src/policy/agent.ts.
 *
 * Guard the fail-closed decision matrix: freeze kill switch, read_only mode,
 * contract-creation block, deny>allow precedence, trade_only allowlist,
 * withdrawal allowlist, native-transfer switch, approve cap, per-token cap,
 * per-tx/daily value caps with the owner-budget escape hatch, and rate limit.
 *
 * The three DB-backed helpers the engine calls are mocked so the suite is pure.
 */

vi.mock('../src/db/agents.js', () => ({
  sumAgentDailyValue: vi.fn(),
  reserveBudget: vi.fn(),
  countAgentSignaturesLastMinute: vi.fn(),
}));

import {
  countAgentSignaturesLastMinute,
  reserveBudget,
  sumAgentDailyValue,
} from '../src/db/agents.js';
import type {
  AgentPolicyRow,
  AgentPolicyRuleRow,
  AgentPrincipalRow,
} from '../src/db/agents.js';
import {
  ERC20_APPROVE_SELECTOR,
  ERC20_TRANSFER_FROM_SELECTOR,
  ERC20_TRANSFER_SELECTOR,
} from '../src/policy/funded.js';
import { effectivePolicy, evaluateAgent, type AgentTxIntent } from '../src/policy/agent.js';

const DID = 'did:matrix:00112233-4455-6677-8899-aabbccddeeff:0011223344556677';
const CONTRACT = '0x1111111111111111111111111111111111111111';
const TOKEN = '0x2222222222222222222222222222222222222222';
const PEER = '0x3333333333333333333333333333333333333333';
const ATTACKER = '0x000000000000000000000000000000000000bad1';

function principal(overrides: Partial<AgentPrincipalRow> = {}): AgentPrincipalRow {
  return {
    did: DID,
    owner_user_id: '00112233-4455-6677-8899-aabbccddeeff',
    label: '00112233-4455-6677-8899-aabbccddeeff',
    key_fingerprint: '0011223344556677',
    public_key: '00'.repeat(32),
    wallet_id: 'w_agent',
    is_frozen: false,
    created_at: new Date().toISOString(),
    last_seen_at: null,
    ...overrides,
  };
}

/** Permissive 'full' policy so each test can tighten exactly one knob. */
function policy(overrides: Partial<AgentPolicyRow> = {}): AgentPolicyRow {
  return {
    did: DID,
    mode: 'full',
    max_tx_value_wei: 10n ** 24n,
    max_daily_value_wei: 10n ** 25n,
    rate_limit_per_min: 30,
    max_approve_wei: 10n ** 30n,
    allow_native_transfer: true,
    withdrawal_allowlist_only: false,
    daily_reset_utc_hour: 0,
    updated_at: new Date().toISOString(),
    updated_by: null,
    ...overrides,
  };
}

function rule(
  effect: AgentPolicyRuleRow['effect'],
  subject: AgentPolicyRuleRow['subject'],
  value: string,
  overrides: Partial<AgentPolicyRuleRow> = {},
): AgentPolicyRuleRow {
  return {
    id: `r_${Math.random().toString(36).slice(2)}`,
    did: DID,
    effect,
    subject,
    value,
    max_value_wei: null,
    note: null,
    created_at: new Date().toISOString(),
    created_by: null,
    ...overrides,
  };
}

function tx(overrides: Partial<AgentTxIntent> = {}): AgentTxIntent {
  return { kind: 'transaction', to: CONTRACT, value: 0n, data: '0xabcdef01', ...overrides };
}

function transferCalldata(to: string, amount = 1n): string {
  return (
    ERC20_TRANSFER_SELECTOR +
    to.replace(/^0x/, '').toLowerCase().padStart(64, '0') +
    amount.toString(16).padStart(64, '0')
  );
}

function approveCalldata(spender: string, amount = 1n): string {
  return (
    ERC20_APPROVE_SELECTOR +
    spender.replace(/^0x/, '').toLowerCase().padStart(64, '0') +
    amount.toString(16).padStart(64, '0')
  );
}

function transferFromCalldata(from: string, to: string, amount = 1n): string {
  return (
    ERC20_TRANSFER_FROM_SELECTOR +
    from.replace(/^0x/, '').toLowerCase().padStart(64, '0') +
    to.replace(/^0x/, '').toLowerCase().padStart(64, '0') +
    amount.toString(16).padStart(64, '0')
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(sumAgentDailyValue).mockResolvedValue(0n);
  vi.mocked(reserveBudget).mockResolvedValue(null);
  vi.mocked(countAgentSignaturesLastMinute).mockResolvedValue(0);
});

describe('effectivePolicy', () => {
  it('falls back to the safe env defaults when the policy row is null', () => {
    const eff = effectivePolicy(null);
    expect(eff.mode).toBe('read_only');
    expect(eff.maxTxValueWei).toBe(0n);
    expect(eff.maxDailyValueWei).toBe(0n);
    expect(eff.rateLimitPerMin).toBe(30);
    expect(eff.maxApproveWei).toBe(0n);
    expect(eff.allowNativeTransfer).toBe(false);
    expect(eff.withdrawalAllowlistOnly).toBe(true);
    expect(eff.dailyResetUtcHour).toBe(0);
  });

  it('uses row values and falls back per-field for nulls', () => {
    const eff = effectivePolicy(
      policy({ mode: 'trade_only', max_tx_value_wei: null, rate_limit_per_min: null, max_approve_wei: 5n }),
    );
    expect(eff.mode).toBe('trade_only');
    expect(eff.maxTxValueWei).toBe(0n);
    expect(eff.rateLimitPerMin).toBe(30);
    expect(eff.maxApproveWei).toBe(5n);
  });
});

describe('evaluateAgent — freeze + mode', () => {
  it('denies everything when the principal is frozen', async () => {
    const d = await evaluateAgent({ principal: principal({ is_frozen: true }), policy: policy(), rules: [], intent: tx() });
    expect(d.allow).toBe(false);
    if (!d.allow) expect(d.code).toBe('AGENT_FROZEN');
  });

  it('denies a transaction in read_only mode', async () => {
    const d = await evaluateAgent({ principal: principal(), policy: policy({ mode: 'read_only' }), rules: [], intent: tx() });
    expect(d.allow).toBe(false);
    if (!d.allow) expect(d.code).toBe('MODE_READ_ONLY');
  });

  it('denies message signing in read_only mode', async () => {
    const d = await evaluateAgent({ principal: principal(), policy: policy({ mode: 'read_only' }), rules: [], intent: { kind: 'message' } });
    expect(d.allow).toBe(false);
    if (!d.allow) expect(d.code).toBe('MODE_READ_ONLY');
  });

  it('allows message signing in full mode without touching value/rate gates', async () => {
    const d = await evaluateAgent({ principal: principal(), policy: policy(), rules: [], intent: { kind: 'message' } });
    expect(d.allow).toBe(true);
    expect(countAgentSignaturesLastMinute).not.toHaveBeenCalled();
  });
});

describe('evaluateAgent — tx shape', () => {
  it('allows contract creation (no `to`) for a full-mode agent', async () => {
    const d = await evaluateAgent({ principal: principal(), policy: policy(), rules: [], intent: tx({ to: undefined, data: '0x6080' }) });
    expect(d.allow).toBe(true);
  });

  it('blocks contract creation for a non-full (trade_only) agent', async () => {
    const d = await evaluateAgent({ principal: principal(), policy: policy({ mode: 'trade_only' }), rules: [], intent: tx({ to: undefined, data: '0x6080' }) });
    expect(d.allow).toBe(false);
    if (!d.allow) expect(d.code).toBe('CONTRACT_CREATION_BLOCKED');
  });

  it('rejects data shorter than a 4-byte selector', async () => {
    const d = await evaluateAgent({ principal: principal(), policy: policy(), rules: [], intent: tx({ data: '0xabcd' }) });
    expect(d.allow).toBe(false);
    if (!d.allow) expect(d.code).toBe('INVALID_TX_SHAPE');
  });
});

describe('evaluateAgent — denylist (deny beats allow)', () => {
  it('denies a denylisted contract', async () => {
    const d = await evaluateAgent({ principal: principal(), policy: policy(), rules: [rule('deny', 'contract', CONTRACT)], intent: tx() });
    expect(d.allow).toBe(false);
    if (!d.allow) expect(d.code).toBe('CONTRACT_DENYLISTED');
  });

  it('denies a denylisted selector', async () => {
    const d = await evaluateAgent({ principal: principal(), policy: policy(), rules: [rule('deny', 'selector', '0xabcdef01')], intent: tx({ data: '0xabcdef01' }) });
    expect(d.allow).toBe(false);
    if (!d.allow) expect(d.code).toBe('SELECTOR_DENYLISTED');
  });

  it('denies a denylisted token (auto-decoded ERC-20 transfer)', async () => {
    const d = await evaluateAgent({ principal: principal(), policy: policy(), rules: [rule('deny', 'token', TOKEN)], intent: tx({ to: TOKEN, data: transferCalldata(PEER) }) });
    expect(d.allow).toBe(false);
    if (!d.allow) expect(d.code).toBe('TOKEN_DENYLISTED');
  });

  it('denies a denylisted counterparty (auto-decoded transfer recipient)', async () => {
    const d = await evaluateAgent({ principal: principal(), policy: policy(), rules: [rule('deny', 'address', ATTACKER)], intent: tx({ to: TOKEN, data: transferCalldata(ATTACKER) }) });
    expect(d.allow).toBe(false);
    if (!d.allow) expect(d.code).toBe('ADDRESS_DENYLISTED');
  });

  it('decodes the transferFrom recipient at arg index 1 for the address denylist', async () => {
    const d = await evaluateAgent({ principal: principal(), policy: policy(), rules: [rule('deny', 'address', ATTACKER)], intent: tx({ to: TOKEN, data: transferFromCalldata(PEER, ATTACKER) }) });
    expect(d.allow).toBe(false);
    if (!d.allow) expect(d.code).toBe('ADDRESS_DENYLISTED');
  });

  it('deny wins when the same contract is both allowed and denied', async () => {
    const d = await evaluateAgent({ principal: principal(), policy: policy({ mode: 'trade_only' }), rules: [rule('allow', 'contract', CONTRACT), rule('deny', 'contract', CONTRACT)], intent: tx() });
    expect(d.allow).toBe(false);
    if (!d.allow) expect(d.code).toBe('CONTRACT_DENYLISTED');
  });
});

describe('evaluateAgent — trade_only allowlist', () => {
  it('denies a non-allowlisted contract when allow rules exist', async () => {
    const d = await evaluateAgent({ principal: principal(), policy: policy({ mode: 'trade_only' }), rules: [rule('allow', 'contract', PEER)], intent: tx() });
    expect(d.allow).toBe(false);
    if (!d.allow) expect(d.code).toBe('CONTRACT_NOT_ALLOWLISTED');
  });

  it('allows an allowlisted contract in trade_only', async () => {
    const d = await evaluateAgent({ principal: principal(), policy: policy({ mode: 'trade_only' }), rules: [rule('allow', 'contract', CONTRACT)], intent: tx() });
    expect(d.allow).toBe(true);
  });

  it('full mode does not require the allowlist even when allow rules exist', async () => {
    const d = await evaluateAgent({ principal: principal(), policy: policy({ mode: 'full' }), rules: [rule('allow', 'contract', PEER)], intent: tx() });
    expect(d.allow).toBe(true);
  });
});

describe('evaluateAgent — withdrawal + native transfer', () => {
  it('blocks an ERC-20 transfer to a non-allowlisted recipient when allowlist-only', async () => {
    const d = await evaluateAgent({ principal: principal(), policy: policy({ withdrawal_allowlist_only: true }), rules: [], intent: tx({ to: TOKEN, data: transferCalldata(PEER) }) });
    expect(d.allow).toBe(false);
    if (!d.allow) expect(d.code).toBe('WITHDRAWAL_NOT_ALLOWLISTED');
  });

  it('allows an ERC-20 transfer to an allowlisted recipient', async () => {
    const d = await evaluateAgent({ principal: principal(), policy: policy({ withdrawal_allowlist_only: true }), rules: [rule('allow', 'withdrawal', PEER)], intent: tx({ to: TOKEN, data: transferCalldata(PEER) }) });
    expect(d.allow).toBe(true);
  });

  it('blocks a bare native transfer to a non-allowlisted target (withdrawal gate first)', async () => {
    const d = await evaluateAgent({ principal: principal(), policy: policy({ withdrawal_allowlist_only: true, allow_native_transfer: true }), rules: [], intent: tx({ to: PEER, value: 1n, data: '0x' }) });
    expect(d.allow).toBe(false);
    if (!d.allow) expect(d.code).toBe('WITHDRAWAL_NOT_ALLOWLISTED');
  });

  it('blocks a native transfer when allow_native_transfer is false', async () => {
    const d = await evaluateAgent({ principal: principal(), policy: policy({ withdrawal_allowlist_only: false, allow_native_transfer: false }), rules: [], intent: tx({ to: PEER, value: 1n, data: '0x' }) });
    expect(d.allow).toBe(false);
    if (!d.allow) expect(d.code).toBe('NATIVE_TRANSFER_BLOCKED');
  });

  it('allows a native transfer to an allowlisted target with the switch on', async () => {
    const d = await evaluateAgent({ principal: principal(), policy: policy({ withdrawal_allowlist_only: true, allow_native_transfer: true, max_tx_value_wei: 10n ** 24n }), rules: [rule('allow', 'withdrawal', PEER)], intent: tx({ to: PEER, value: 1n, data: '0x' }) });
    expect(d.allow).toBe(true);
    if (d.allow) expect(d.reservedValueWei).toBe(1n);
  });
});

describe('evaluateAgent — approve + token caps', () => {
  it('denies an approve above the approval cap (auto-decoded amount)', async () => {
    const d = await evaluateAgent({ principal: principal(), policy: policy({ max_approve_wei: 100n, withdrawal_allowlist_only: false }), rules: [], intent: tx({ to: TOKEN, data: approveCalldata(PEER, 1000n) }) });
    expect(d.allow).toBe(false);
    if (!d.allow) expect(d.code).toBe('APPROVE_CAP');
  });

  it('allows an approve within the cap', async () => {
    const d = await evaluateAgent({ principal: principal(), policy: policy({ max_approve_wei: 1000n, withdrawal_allowlist_only: false }), rules: [], intent: tx({ to: TOKEN, data: approveCalldata(PEER, 100n) }) });
    expect(d.allow).toBe(true);
  });

  it('enforces a per-token cap from an allow-token rule (high-level intent)', async () => {
    const d = await evaluateAgent({
      principal: principal(),
      policy: policy({ withdrawal_allowlist_only: false }),
      rules: [rule('allow', 'token', TOKEN, { max_value_wei: 500n })],
      intent: tx({ to: TOKEN, tokenContract: TOKEN, tokenRecipient: PEER, tokenAmount: 1000n, data: transferCalldata(PEER, 1000n) }),
    });
    expect(d.allow).toBe(false);
    if (!d.allow) expect(d.code).toBe('TOKEN_VALUE_CAP');
  });
});

describe('evaluateAgent — value caps + budget escape hatch', () => {
  const base = () =>
    policy({ withdrawal_allowlist_only: true, allow_native_transfer: true, max_tx_value_wei: 100n, max_daily_value_wei: 1000n });
  const allowPeer = () => [rule('allow', 'withdrawal', PEER)];

  it('denies a tx above the per-tx cap when no budget covers it', async () => {
    vi.mocked(reserveBudget).mockResolvedValue(null);
    const d = await evaluateAgent({ principal: principal(), policy: base(), rules: allowPeer(), intent: tx({ to: PEER, value: 500n, data: '0x' }) });
    expect(d.allow).toBe(false);
    if (!d.allow) expect(d.code).toBe('TX_VALUE_CAP');
  });

  it('denies when the daily cap would be exceeded and no budget covers it', async () => {
    vi.mocked(sumAgentDailyValue).mockResolvedValue(950n);
    vi.mocked(reserveBudget).mockResolvedValue(null);
    const d = await evaluateAgent({ principal: principal(), policy: base(), rules: allowPeer(), intent: tx({ to: PEER, value: 100n, data: '0x' }) });
    expect(d.allow).toBe(false);
    if (!d.allow) expect(d.code).toBe('DAILY_VALUE_CAP');
  });

  it('allows above-cap spend when an owner budget covers it and returns the reservation', async () => {
    vi.mocked(reserveBudget).mockResolvedValue('budget_42');
    const d = await evaluateAgent({ principal: principal(), policy: base(), rules: allowPeer(), intent: tx({ to: PEER, value: 500n, data: '0x' }) });
    expect(d.allow).toBe(true);
    if (d.allow) {
      expect(d.reservedBudgetId).toBe('budget_42');
      expect(d.reservedValueWei).toBe(500n);
    }
  });

  it('allows spend within caps without reserving a budget', async () => {
    const d = await evaluateAgent({ principal: principal(), policy: base(), rules: allowPeer(), intent: tx({ to: PEER, value: 50n, data: '0x' }) });
    expect(d.allow).toBe(true);
    if (d.allow) expect(d.reservedBudgetId).toBeNull();
    expect(reserveBudget).not.toHaveBeenCalled();
  });
});

describe('evaluateAgent — rate limit', () => {
  it('denies when the per-minute signing rate is exceeded', async () => {
    vi.mocked(countAgentSignaturesLastMinute).mockResolvedValue(30);
    const d = await evaluateAgent({ principal: principal(), policy: policy({ rate_limit_per_min: 30 }), rules: [], intent: tx() });
    expect(d.allow).toBe(false);
    if (!d.allow) expect(d.code).toBe('RATE_LIMIT');
  });

  it('fails open if the rate-limit query throws', async () => {
    vi.mocked(countAgentSignaturesLastMinute).mockRejectedValue(new Error('db down'));
    const d = await evaluateAgent({ principal: principal(), policy: policy(), rules: [], intent: tx() });
    expect(d.allow).toBe(true);
  });
});

describe('evaluateAgent — happy path', () => {
  it('allows a value-0 contract call in full mode', async () => {
    const d = await evaluateAgent({ principal: principal(), policy: policy(), rules: [], intent: tx() });
    expect(d.allow).toBe(true);
    if (d.allow) {
      expect(d.reservedBudgetId).toBeNull();
      expect(d.reservedValueWei).toBe(0n);
    }
  });
});
