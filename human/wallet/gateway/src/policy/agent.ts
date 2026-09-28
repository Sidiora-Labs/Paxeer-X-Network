import { env } from '../env.js';
import {
  countAgentSignaturesLastMinute,
  reserveBudget,
  sumAgentDailyValue,
  type AgentPolicyRow,
  type AgentPolicyRuleRow,
  type AgentPrincipalRow,
} from '../db/agents.js';

export const ERC20_APPROVE_SELECTOR = '0x095ea7b3';
export const ERC20_TRANSFER_SELECTOR = '0xa9059cbb';
export const ERC20_TRANSFER_FROM_SELECTOR = '0x23b872dd';

export function decodeAddressArg(data: string, argIndex: number): string | null {
  const start = 2 + 8 + 64 * argIndex;
  const end = start + 64;
  if (data.length < end) return null;
  const slot = data.slice(start, end);
  if (!/^0{24}[0-9a-fA-F]{40}$/.test(slot)) return null;
  return `0x${slot.slice(24).toLowerCase()}`;
}

/**
 * Agent policy engine (v2).
 *
 * Evaluated top-to-bottom, fail-closed on every hard limit. Returns a
 * structured decision so routes can shape a precise 4xx and, on allow, release
 * any reserved budget if the subsequent send fails.
 *
 *   0. Freeze            → AGENT_FROZEN (owner kill switch)
 *   1. Mode              → MODE_READ_ONLY (read_only blocks all signing)
 *   2. Shape             → CONTRACT_CREATION_BLOCKED (creation is permitted only
 *                          for full-mode agents) / INVALID_TX_SHAPE
 *   3. Denylist          → CONTRACT_DENYLISTED / SELECTOR_DENYLISTED /
 *                          TOKEN_DENYLISTED / ADDRESS_DENYLISTED (deny beats allow)
 *   4. Allowlist         → CONTRACT_NOT_ALLOWLISTED (trade_only, when allow rules exist)
 *   5. Withdrawal        → WITHDRAWAL_NOT_ALLOWLISTED (recipient/spender must be allowed)
 *   6. Native transfer   → NATIVE_TRANSFER_BLOCKED (unless allow_native_transfer)
 *   7. Approve cap       → APPROVE_CAP
 *   8. Per-rule cap      → TOKEN_VALUE_CAP (allow-token rule's max_value_wei)
 *   9. Value caps        → TX_VALUE_CAP / DAILY_VALUE_CAP (budgets are the
 *                          escape hatch above these for native value)
 *  10. Rate limit        → RATE_LIMIT
 */

export type AgentDenyCode =
  | 'AGENT_FROZEN'
  | 'MODE_READ_ONLY'
  | 'CONTRACT_CREATION_BLOCKED'
  | 'INVALID_TX_SHAPE'
  | 'CONTRACT_DENYLISTED'
  | 'SELECTOR_DENYLISTED'
  | 'TOKEN_DENYLISTED'
  | 'ADDRESS_DENYLISTED'
  | 'CONTRACT_NOT_ALLOWLISTED'
  | 'WITHDRAWAL_NOT_ALLOWLISTED'
  | 'NATIVE_TRANSFER_BLOCKED'
  | 'APPROVE_CAP'
  | 'TOKEN_VALUE_CAP'
  | 'TX_VALUE_CAP'
  | 'DAILY_VALUE_CAP'
  | 'RATE_LIMIT';

export type AgentDecision =
  | { allow: true; reservedBudgetId: string | null; reservedValueWei: bigint }
  | { allow: false; code: AgentDenyCode; message: string; detail?: Record<string, unknown> };

export interface AgentTxIntent {
  kind: 'transaction' | 'message' | 'typed_data';
  to?: string;
  value?: bigint;
  data?: string;
  /**
   * Structured annotations from high-level routes (transfer/approve). When
   * absent for a raw tx, the engine decodes ERC-20 transfer/approve calldata
   * itself to apply token + withdrawal + approve gates.
   */
  tokenContract?: string | null;
  tokenRecipient?: string | null;
  tokenAmount?: bigint | null;
  isApprove?: boolean;
}

export interface EffectivePolicy {
  mode: AgentPolicyRow['mode'];
  maxTxValueWei: bigint;
  maxDailyValueWei: bigint;
  rateLimitPerMin: number;
  maxApproveWei: bigint;
  allowNativeTransfer: boolean;
  withdrawalAllowlistOnly: boolean;
  dailyResetUtcHour: number;
}

/** Resolve a policy row against the env defaults (NULL → default). */
export function effectivePolicy(p: AgentPolicyRow | null): EffectivePolicy {
  return {
    mode: p?.mode ?? env.AGENT_DEFAULT_MODE,
    maxTxValueWei: p?.max_tx_value_wei ?? env.AGENT_DEFAULT_MAX_TX_VALUE_WEI,
    maxDailyValueWei: p?.max_daily_value_wei ?? env.AGENT_DEFAULT_MAX_DAILY_VALUE_WEI,
    rateLimitPerMin: p?.rate_limit_per_min ?? env.AGENT_DEFAULT_RATE_LIMIT_PER_MINUTE,
    maxApproveWei: p?.max_approve_wei ?? env.AGENT_DEFAULT_MAX_APPROVE_WEI,
    allowNativeTransfer: p?.allow_native_transfer ?? false,
    withdrawalAllowlistOnly: p?.withdrawal_allowlist_only ?? true,
    dailyResetUtcHour: p?.daily_reset_utc_hour ?? 0,
  };
}

interface RuleIndex {
  denyContracts: Set<string>;
  denySelectors: Set<string>;
  denyTokens: Set<string>;
  denyAddresses: Set<string>;
  allowContracts: Set<string>;
  allowTokenCaps: Map<string, bigint | null>; // token -> per-tx cap (null = uncapped)
  hasAllowContracts: boolean;
  withdrawalAllowlist: Set<string>;
}

function indexRules(rules: AgentPolicyRuleRow[]): RuleIndex {
  const idx: RuleIndex = {
    denyContracts: new Set(),
    denySelectors: new Set(),
    denyTokens: new Set(),
    denyAddresses: new Set(),
    allowContracts: new Set(),
    allowTokenCaps: new Map(),
    hasAllowContracts: false,
    withdrawalAllowlist: new Set(),
  };
  for (const r of rules) {
    const v = r.value.toLowerCase();
    if (r.effect === 'deny') {
      if (r.subject === 'contract') idx.denyContracts.add(v);
      else if (r.subject === 'selector') idx.denySelectors.add(v);
      else if (r.subject === 'token') idx.denyTokens.add(v);
      else if (r.subject === 'address') idx.denyAddresses.add(v);
    } else {
      if (r.subject === 'contract') {
        idx.allowContracts.add(v);
        idx.hasAllowContracts = true;
      } else if (r.subject === 'token') {
        idx.allowTokenCaps.set(v, r.max_value_wei);
      } else if (r.subject === 'withdrawal') {
        idx.withdrawalAllowlist.add(v);
      }
    }
  }
  return idx;
}

function deny(code: AgentDenyCode, message: string, detail?: Record<string, unknown>): AgentDecision {
  return { allow: false, code, message, detail };
}

/** Most-recent UTC daily-reset boundary as an ISO string. */
function dailyWindowStartIso(hour: number): string {
  const now = new Date();
  const start = new Date(
    Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate(), hour, 0, 0, 0),
  );
  if (start.getTime() > now.getTime()) start.setUTCDate(start.getUTCDate() - 1);
  return start.toISOString();
}

export interface EvaluateAgentInput {
  principal: AgentPrincipalRow;
  policy: AgentPolicyRow | null;
  rules: AgentPolicyRuleRow[];
  intent: AgentTxIntent;
}

/**
 * Evaluate an agent signing intent. On allow for a value-moving native tx that
 * exceeded the normal caps, `reservedBudgetId` carries the budget that was
 * atomically charged — the route MUST release it if the send fails.
 */
export async function evaluateAgent(input: EvaluateAgentInput): Promise<AgentDecision> {
  const { principal, intent } = input;
  const eff = effectivePolicy(input.policy);

  // 0. Freeze — the instant kill switch.
  if (principal.is_frozen) {
    return deny('AGENT_FROZEN', 'agent is frozen by its owner; no actions permitted');
  }

  // 1. Mode — read_only forbids all signing (tx, message, typed-data).
  if (eff.mode === 'read_only') {
    return deny('MODE_READ_ONLY', 'agent policy mode is read_only; signing is disabled');
  }

  // Messages / typed-data carry no destination or transferable value at this
  // layer; the mode gate above is the only check.
  if (intent.kind !== 'transaction') {
    return { allow: true, reservedBudgetId: null, reservedValueWei: 0n };
  }

  const idx = indexRules(input.rules);
  const value = intent.value ?? 0n;
  const data = (intent.data ?? '0x').toLowerCase();
  const isEmptyData = data === '0x' || data === '';

  // 2. Shape — contract creation. Permitted only for full-mode agents (the
  //    trusted tier that already bypasses the trade allowlist). A creation tx
  //    has no destination contract / selector / token to gate, so the
  //    denylist, allowlist and withdrawal checks below do not apply; only the
  //    native value caps + rate limit guard it.
  if (!intent.to) {
    if (eff.mode !== 'full') {
      return deny(
        'CONTRACT_CREATION_BLOCKED',
        'contract creation requires a full-mode agent policy',
      );
    }
    if (value > eff.maxTxValueWei) {
      return deny('TX_VALUE_CAP', 'deployment value exceeds the per-tx cap', {
        value: value.toString(),
        cap: eff.maxTxValueWei.toString(),
      });
    }
    if (value > 0n) {
      const windowStart = dailyWindowStartIso(eff.dailyResetUtcHour);
      const dailySpent = await sumAgentDailyValue(principal.did, windowStart);
      if (dailySpent + value > eff.maxDailyValueWei) {
        return deny('DAILY_VALUE_CAP', 'daily value cap exceeded by this deployment', {
          used: dailySpent.toString(),
          request: value.toString(),
          cap: eff.maxDailyValueWei.toString(),
        });
      }
    }
    const limited = await rateLimited(principal.did, eff.rateLimitPerMin);
    if (limited) return limited;
    return { allow: true, reservedBudgetId: null, reservedValueWei: value };
  }
  const to = intent.to.toLowerCase();

  // Decode selector + ERC-20 transfer/approve operands when not provided.
  let selector: string | null = null;
  let tokenContract = intent.tokenContract?.toLowerCase() ?? null;
  let tokenRecipient = intent.tokenRecipient?.toLowerCase() ?? null;
  let tokenAmount = intent.tokenAmount ?? null;
  let isApprove = intent.isApprove ?? false;

  if (!isEmptyData) {
    if (data.length < 10) {
      return deny('INVALID_TX_SHAPE', 'tx.data must be empty or contain a 4-byte selector');
    }
    selector = data.slice(0, 10);
    // Auto-decode the common ERC-20 movers so token/withdrawal gates apply even
    // on a raw sign/send (not just the high-level helper routes).
    if (intent.tokenContract === undefined) {
      if (selector === ERC20_TRANSFER_SELECTOR) {
        tokenContract = to;
        tokenRecipient = decodeAddressArg(data, 0);
      } else if (selector === ERC20_APPROVE_SELECTOR) {
        tokenContract = to;
        tokenRecipient = decodeAddressArg(data, 0);
        isApprove = true;
      } else if (selector === ERC20_TRANSFER_FROM_SELECTOR) {
        tokenContract = to;
        tokenRecipient = decodeAddressArg(data, 1);
      }
    }
  }

  // 3. Denylist — deny always beats allow.
  if (idx.denyContracts.has(to)) {
    return deny('CONTRACT_DENYLISTED', 'target contract is on the deny list', { contract: to });
  }
  if (selector && idx.denySelectors.has(selector)) {
    return deny('SELECTOR_DENYLISTED', 'method selector is on the deny list', { selector });
  }
  if (tokenContract && idx.denyTokens.has(tokenContract)) {
    return deny('TOKEN_DENYLISTED', 'token is on the deny list', { token: tokenContract });
  }
  if (tokenRecipient && idx.denyAddresses.has(tokenRecipient)) {
    return deny('ADDRESS_DENYLISTED', 'counterparty address is on the deny list', {
      address: tokenRecipient,
    });
  }

  // 4. Allowlist — in trade_only, when explicit allow-contract rules exist, the
  //    target MUST be allowlisted. `full` mode skips the allowlist requirement.
  if (eff.mode === 'trade_only' && idx.hasAllowContracts && !idx.allowContracts.has(to)) {
    return deny('CONTRACT_NOT_ALLOWLISTED', 'target contract is not on the trade allowlist', {
      contract: to,
    });
  }

  // 5. Withdrawal allowlist — any recipient that can RECEIVE funds (native
  //    transfer target, ERC-20 transfer recipient, or approve spender) must be
  //    on the allowlist when withdrawal_allowlist_only is set.
  if (eff.withdrawalAllowlistOnly) {
    const recipientNeedingAllow: string | null =
      value > 0n && isEmptyData
        ? to // bare native transfer → funds go to `to`
        : tokenRecipient ?? null; // erc-20 transfer recipient / approve spender
    if (recipientNeedingAllow && !idx.withdrawalAllowlist.has(recipientNeedingAllow)) {
      return deny(
        'WITHDRAWAL_NOT_ALLOWLISTED',
        'recipient/spender is not on the withdrawal allowlist',
        { recipient: recipientNeedingAllow },
      );
    }
  }

  // 6. Native transfer switch — a bare PAX transfer is a withdrawal vector.
  if (value > 0n && isEmptyData && !eff.allowNativeTransfer) {
    return deny('NATIVE_TRANSFER_BLOCKED', 'native PAX transfers are disabled for this agent');
  }

  // 7. Approve cap.
  if (isApprove) {
    const approveAmount = tokenAmount ?? decodeApproveAmount(data);
    if (approveAmount !== null && approveAmount > eff.maxApproveWei) {
      return deny('APPROVE_CAP', 'approve amount exceeds the per-agent approval cap', {
        amount: approveAmount.toString(),
        cap: eff.maxApproveWei.toString(),
      });
    }
  }

  // 8. Per-rule token cap (allow-token rule's max_value_wei = per-tx ceiling).
  if (tokenContract && tokenAmount !== null && idx.allowTokenCaps.has(tokenContract)) {
    const cap = idx.allowTokenCaps.get(tokenContract);
    if (cap !== null && cap !== undefined && tokenAmount > cap) {
      return deny('TOKEN_VALUE_CAP', 'token amount exceeds the per-tx cap for this token', {
        token: tokenContract,
        amount: tokenAmount.toString(),
        cap: cap.toString(),
      });
    }
  }

  // 9. Native value caps. Budgets are the escape hatch ABOVE the normal caps.
  let reservedBudgetId: string | null = null;
  if (value > 0n) {
    const withinPerTx = value <= eff.maxTxValueWei;
    const windowStart = dailyWindowStartIso(eff.dailyResetUtcHour);
    const dailySpent = await sumAgentDailyValue(principal.did, windowStart);
    const withinDaily = dailySpent + value <= eff.maxDailyValueWei;

    if (!withinPerTx || !withinDaily) {
      // Try to draw the overage from an owner-granted budget covering `to`.
      reservedBudgetId = await reserveBudget({
        did: principal.did,
        targetContract: to,
        token: null,
        valueWei: value,
      });
      if (!reservedBudgetId) {
        if (!withinPerTx) {
          return deny('TX_VALUE_CAP', 'tx value exceeds the per-tx cap and no budget covers it', {
            value: value.toString(),
            cap: eff.maxTxValueWei.toString(),
          });
        }
        return deny('DAILY_VALUE_CAP', 'daily value cap exceeded and no budget covers it', {
          used: dailySpent.toString(),
          request: value.toString(),
          cap: eff.maxDailyValueWei.toString(),
        });
      }
    }
  }

  // 10. Rate limit (fail-open on DB hiccup; the caps above are the hard guard).
  const limited = await rateLimited(principal.did, eff.rateLimitPerMin);
  if (limited) return limited;

  return { allow: true, reservedBudgetId, reservedValueWei: value };
}

/**
 * Shared rate-limit gate: deny when the agent exceeded its per-minute signing
 * budget. Fails open on a transient DB error (the value caps are the hard
 * guard). Returns null when within the limit.
 */
async function rateLimited(did: string, perMin: number): Promise<AgentDecision | null> {
  try {
    const recent = await countAgentSignaturesLastMinute(did);
    if (recent >= perMin) {
      return deny('RATE_LIMIT', `more than ${perMin} signing requests in 60s`);
    }
  } catch {
    // fail-open
  }
  return null;
}

/** Decode the `amount` (2nd arg) of an ERC-20 approve(address,uint256) calldata. */
function decodeApproveAmount(data: string): bigint | null {
  // selector(8) + arg0 address(64) + arg1 uint256(64); 0x prefix => offsets +2.
  const start = 2 + 8 + 64;
  const end = start + 64;
  if (data.length < end) return null;
  const slot = data.slice(start, end);
  if (!/^[0-9a-f]{64}$/.test(slot)) return null;
  return BigInt(`0x${slot}`);
}
