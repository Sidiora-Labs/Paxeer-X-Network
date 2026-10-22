import { keccak256, toBytes, type Hex } from 'viem';
import { encodeErc20Approve, encodeCall, getErc20Metadata } from '../../chainReads.js';
import { env } from '../../env.js';
import { parseUnits } from 'viem';
import type { ActionLeg } from '../../db/actions.js';
import {
  ERC20_APPROVE_SELECTOR,
  ERC20_TRANSFER_FROM_SELECTOR,
  ERC20_TRANSFER_SELECTOR,
  type AgentTxIntent,
} from '../../policy/agent.js';

const ERC20_MOVER_SELECTORS = new Set([
  ERC20_APPROVE_SELECTOR,
  ERC20_TRANSFER_SELECTOR,
  ERC20_TRANSFER_FROM_SELECTOR,
]);

/**
 * Kind-specific intent builders for the durable-action lane.
 *
 * Each builder resolves a request into a normalized `ActionPlan`: the token to
 * approve, the spender, the call target, the raw amount, the encoded calldata,
 * and how to verify the postcondition. The orchestrator is otherwise
 * kind-agnostic — it drives approve → confirm → call → confirm → verify off
 * this plan, so adding a new domain route later is just a new builder.
 */

export interface ActionPlan {
  /** ERC-20 to approve + spend (always set for both current kinds). */
  token: `0x${string}`;
  decimals: number;
  /** Address that must be approved to pull `amountWei`. */
  spender: `0x${string}`;
  /** Contract the primary call targets. */
  contract: `0x${string}`;
  /** Raw base-unit amount to approve / spend. */
  amountWei: string;
  /** Encoded primary-call calldata. */
  callData: Hex;
  /** Native value sent with the call (0 for both current kinds). */
  callValueWei: string;
  /** 4-byte selector of the primary call (audit + policy). */
  selector: string;
  /** Postcondition verification strategy. */
  verify: { type: 'layerx_credit' } | { type: 'none' };
  label: string;
}

export class PlanError extends Error {
  constructor(
    public code: 'INVALID_REQUEST' | 'INVALID_AMOUNT',
    message: string,
  ) {
    super(message);
  }
}

/** The canonical on-chain DID claim: lowercase 0x + 64-hex keccak256(did). */
export function didClaimFor(did: string): Hex {
  return keccak256(toBytes(did));
}

/**
 * LayerX deposit: approve(USDL → vault, amount) then depositUSDL(amount, did).
 * `amount` is a HUMAN decimal string (e.g. "250" / "250.5") converted with the
 * resolved USDL decimals. `didClaim` is the bytes32 the agent obtained from
 * LayerX GET /v1/deposit (which registered it for credit attribution).
 */
export async function buildLayerxDepositPlan(args: {
  amount: string;
  didClaim: string;
}): Promise<ActionPlan> {
  const vault = env.LAYERX_VAULT_ADDRESS;
  if (!vault) throw new PlanError('INVALID_REQUEST', 'LayerX vault not configured');
  const usdl = env.LAYERX_USDL_ADDRESS as `0x${string}`;

  // Cheap, pure validations FIRST so a malformed request never round-trips the
  // RPC for token metadata.
  if (!/^0x[0-9a-fA-F]{64}$/.test(args.didClaim)) {
    throw new PlanError('INVALID_REQUEST', 'did_claim must be a 0x 32-byte hex value');
  }

  const meta = await getErc20Metadata(usdl).catch(() => ({ decimals: null, symbol: null }));
  const decimals = meta.decimals ?? env.LAYERX_USDL_DECIMALS;

  let amountWei: bigint;
  try {
    amountWei = parseUnits(args.amount, decimals);
  } catch {
    throw new PlanError('INVALID_AMOUNT', `amount "${args.amount}" is not a valid decimal`);
  }
  if (amountWei <= 0n) throw new PlanError('INVALID_AMOUNT', 'amount must be positive');

  const { data, selector } = encodeCall('depositUSDL(uint256,bytes32)', [
    amountWei.toString(),
    args.didClaim,
  ]);

  return {
    token: usdl,
    decimals,
    spender: vault as `0x${string}`,
    contract: vault as `0x${string}`,
    amountWei: amountWei.toString(),
    callData: data,
    callValueWei: '0',
    selector,
    verify: { type: 'layerx_credit' },
    label: 'layerx.deposit',
  };
}

/**
 * Generic allowance-and-call: approve(token → spender, amount) then
 * contract.method(args). Amount + numeric args are RAW base units (integer
 * strings) — this advanced route does no decimal conversion, so the agent
 * supplies exactly encodable values (the domain routes are preferred when the
 * agent wants the wallet to own ABI/decimals/sequencing).
 */
export function buildAllowanceAndCallPlan(args: {
  token: string;
  amount: string;
  spender: string;
  contract: string;
  method: string;
  args: string[];
}): ActionPlan {
  let amountWei: bigint;
  try {
    amountWei = BigInt(args.amount);
  } catch {
    throw new PlanError('INVALID_AMOUNT', `amount "${args.amount}" is not an integer`);
  }
  if (amountWei <= 0n) throw new PlanError('INVALID_AMOUNT', 'amount must be positive');

  let encoded: { data: Hex; selector: string };
  try {
    encoded = encodeCall(args.method, args.args);
  } catch (err) {
    throw new PlanError('INVALID_REQUEST', `could not encode call: ${(err as Error).message}`);
  }
  // The primary call is policy-checked from its own calldata, so it must not be
  // a token transfer or approval the approval leg's spender would stand in for.
  if (args.contract.toLowerCase() === args.token.toLowerCase()) {
    throw new PlanError('INVALID_REQUEST', 'the call contract must not be the approved token');
  }
  if (ERC20_MOVER_SELECTORS.has(encoded.selector.toLowerCase())) {
    throw new PlanError('INVALID_REQUEST', 'the call must not be an ERC-20 transfer, transferFrom or approve');
  }

  return {
    token: args.token as `0x${string}`,
    decimals: 0,
    spender: args.spender as `0x${string}`,
    contract: args.contract as `0x${string}`,
    amountWei: amountWei.toString(),
    callData: encoded.data,
    callValueWei: '0',
    selector: encoded.selector,
    verify: { type: 'none' },
    label: `call.${args.method.split('(')[0]}`,
  };
}

/** Encode the approval leg for a plan. */
export function approvalCalldata(plan: ActionPlan): Hex {
  return encodeErc20Approve(plan.spender, BigInt(plan.amountWei));
}

/**
 * The policy intent of one leg. The approval leg names the token, spender and
 * amount it approves; the call leg carries no token annotations, so the policy
 * decodes its recipient from the calldata it actually sends.
 */
export function actionLegIntent(
  plan: ActionPlan,
  leg: ActionLeg,
  tx: { to: `0x${string}`; data: Hex; value: bigint },
): AgentTxIntent {
  const intent: AgentTxIntent = { kind: 'transaction', to: tx.to, value: tx.value, data: tx.data };
  if (leg === 'call') return intent;
  return {
    ...intent,
    tokenContract: plan.token,
    tokenRecipient: plan.spender,
    tokenAmount: BigInt(plan.amountWei),
    isApprove: true,
  };
}
