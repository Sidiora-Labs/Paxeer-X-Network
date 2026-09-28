import { createWalletClient, http, type Hex } from 'viem';
import { hyperPaxeer } from '../../chain.js';
import { env } from '../../env.js';
import {
  getErc20Allowance,
  getErc20Balance,
  getNativeBalance,
  getNonce,
  isTxKnown,
  resolveGas,
  simulate,
  waitForReceipt,
} from '../../chainReads.js';
import { findPrincipal, getPolicy, listRules, releaseBudget } from '../../db/agents.js';
import { evaluateAgent, type AgentTxIntent } from '../../policy/agent.js';
import { findWalletByAddress, getSigningAccountForRow, logSignature, type WalletRow } from '../../db/wallets.js';
import { getDepositByTx, layerxEnabled } from '../../layerx/db.js';
import { withDistributedWalletLock } from './nonceLock.js';
import {
  buildErrorEnvelope,
  terminalStatusFor,
  type ActionErrorCode,
  type ActionPhase,
} from './errors.js';
import { approvalCalldata, type ActionPlan } from './plan.js';
import { getAction, updateAction, type ActionRow, type ActionStatus } from '../../db/actions.js';

/**
 * The durable-action orchestrator: it advances ONE action by exactly one
 * transition per call, persisting state at every step so a crash or a client
 * disconnect is always recoverable. It is re-entrant — a broadcast leg checks
 * for an already-assigned tx hash before sending, so a resumed action never
 * allocates a second nonce or double-broadcasts.
 *
 * All correctness the spec demands lives here:
 *   - cross-process per-wallet nonce locking (withDistributedWalletLock)
 *   - confirmation before dependent calls (approval confirmed → then call)
 *   - preflight simulation immediately before the broadcast
 *   - deterministic fee-bump REPLACEMENT only after proving a tx is still
 *     pending / was dropped (the wallet owns this; the agent never decides)
 *   - budget rollback ONLY when nothing was broadcast
 *   - interpretation-free error envelopes for every unsuccessful step
 */

const CONFIRMATIONS = env.ACTION_CONFIRMATIONS;

/** Number of receipt-wait timeouts on a leg before the wallet replaces the tx. */
const REPLACE_AFTER_ATTEMPTS = 3;

/** Advance a leased action by one transition. Never throws. */
export async function advanceAction(row: ActionRow): Promise<void> {
  try {
    switch (row.status) {
      case 'received':
      case 'validating':
        await validate(row);
        return;
      case 'awaiting_approval':
        await broadcastApproval(row);
        return;
      case 'approval_pending':
        await confirmLeg(row, 'approval');
        return;
      case 'approval_confirmed':
      case 'simulating_call':
        await simulateAndBroadcastCall(row);
        return;
      case 'call_pending':
        await confirmLeg(row, 'call');
        return;
      case 'reconciling':
        await reconcile(row);
        return;
      default:
        // terminal — nothing to do.
        return;
    }
  } catch (err) {
    // Any unexpected throw is transient infrastructure — never terminal, never
    // a resend. Surface as RETRY_SAME_ACTION so the worker re-attempts.
    await fail(row, 'reconciliation', 'RPC_UNAVAILABLE', (err as Error).message, {}, false);
  }
}

// -----------------------------------------------------------------------------
// Phase 1: validate — resolve plan, policy-gate, balance + allowance checks.
// -----------------------------------------------------------------------------

async function validate(row: ActionRow): Promise<void> {
  await updateAction(row.id, { status: 'validating', phase: 'validation' });

  const plan = row.plan as unknown as ActionPlan | null;
  if (!plan) {
    // The route COMMITS the insert before it builds + persists the plan, so a
    // fresh plan-less row is intake-in-progress; claimNextAction shields those
    // for INTAKE_GRACE_SECONDS. Reaching here means the intake orphaned (route
    // crashed mid-plan) — terminal it.
    await fail(row, 'validation', 'INVALID_REQUEST', 'action has no execution plan', {}, true);
    return;
  }

  const principal = await findPrincipal(row.did);
  if (!principal) {
    await fail(row, 'validation', 'INVALID_REQUEST', 'unknown principal', {}, true);
    return;
  }
  if (principal.is_frozen) {
    await fail(row, 'validation', 'AGENT_FROZEN', 'agent is frozen by its owner', {}, true);
    return;
  }

  const wallet = row.wallet_address ? await findWalletByAddress(row.wallet_address) : null;
  if (!wallet) {
    await fail(row, 'validation', 'INVALID_REQUEST', 'agent wallet not provisioned', {}, true);
    return;
  }

  const [policy, rules] = await Promise.all([getPolicy(row.did), listRules(row.did)]);
  const amountWei = BigInt(plan.amountWei);

  // Policy-gate BOTH legs: the approve and the primary call.
  const approveIntent: AgentTxIntent = {
    kind: 'transaction',
    to: plan.token,
    value: 0n,
    data: approvalCalldata(plan),
    tokenContract: plan.token,
    tokenRecipient: plan.spender,
    tokenAmount: amountWei,
    isApprove: true,
  };
  const callIntent: AgentTxIntent = {
    kind: 'transaction',
    to: plan.contract,
    value: 0n,
    data: plan.callData,
    tokenContract: plan.token,
    tokenRecipient: plan.spender,
    tokenAmount: amountWei,
  };

  for (const intent of [approveIntent, callIntent]) {
    const decision = await evaluateAgent({ principal, policy, rules, intent });
    if (!decision.allow) {
      const code: ActionErrorCode =
        decision.code === 'APPROVE_CAP' ? 'APPROVAL_CAP_EXCEEDED' : 'POLICY_DENIED';
      await fail(row, 'validation', code, decision.message, { policy_code: decision.code }, true);
      return;
    }
  }

  // Balance: the wallet must hold enough of the token to spend.
  const balance = await getErc20Balance(plan.token, wallet.address);
  if (balance < amountWei) {
    await fail(
      row,
      'balance_check',
      'INSUFFICIENT_BALANCE',
      'token balance is below the requested amount',
      { balance: balance.toString(), required: amountWei.toString(), token: plan.token },
      true,
    );
    return;
  }
  // Gas: the wallet needs some native PAX to broadcast at all.
  const native = await getNativeBalance(wallet.address).catch(() => 0n);
  if (native === 0n) {
    await fail(row, 'balance_check', 'INSUFFICIENT_GAS', 'wallet holds no native PAX for gas', {}, true);
    return;
  }

  // Live allowance decides whether the approve leg is needed at all.
  const allowance = await getErc20Allowance(plan.token, wallet.address, plan.spender);
  const next: ActionStatus = allowance >= amountWei ? 'simulating_call' : 'awaiting_approval';
  await updateAction(row.id, {
    status: next,
    phase: next === 'awaiting_approval' ? 'approval_broadcast' : 'call_simulation',
    error_code: null,
    error: null,
    wallet_id: wallet.id,
  });
}

// -----------------------------------------------------------------------------
// Phase 2: approval — broadcast approve(token → spender, amount), then confirm.
// -----------------------------------------------------------------------------

async function broadcastApproval(row: ActionRow): Promise<void> {
  const plan = row.plan as unknown as ActionPlan;
  const wallet = await requireWallet(row);
  if (!wallet) return;

  // Re-entrancy: if we already broadcast an approval, don't send another.
  if (row.approval_tx_hash) {
    await updateAction(row.id, { status: 'approval_pending', phase: 'approval_confirmation' });
    return;
  }

  const data = approvalCalldata(plan);
  const sent = await broadcastLeg(row, wallet, { to: plan.token, data, value: 0n });
  await updateAction(row.id, {
    status: 'approval_pending',
    phase: 'approval_confirmation',
    approval_tx_hash: sent.hash,
    approval_nonce: sent.nonce,
    error_code: null,
    error: null,
  });
}

// -----------------------------------------------------------------------------
// Phase 3: simulate + broadcast the primary call.
// -----------------------------------------------------------------------------

async function simulateAndBroadcastCall(row: ActionRow): Promise<void> {
  const plan = row.plan as unknown as ActionPlan;
  const wallet = await requireWallet(row);
  if (!wallet) return;

  // Re-entrancy: a resumed action that already broadcast the call just waits.
  if (row.call_tx_hash) {
    await updateAction(row.id, { status: 'call_pending', phase: 'call_confirmation' });
    return;
  }

  await updateAction(row.id, { status: 'simulating_call', phase: 'call_simulation' });

  // Confirmation-before-dependent-call: re-check the live allowance now covers
  // the spend (the approval must have actually taken effect on-chain).
  const amountWei = BigInt(plan.amountWei);
  const allowance = await getErc20Allowance(plan.token, wallet.address, plan.spender);
  if (allowance < amountWei) {
    // Approval isn't effective yet — go back and (re)establish it rather than
    // broadcasting a call that will revert.
    await updateAction(row.id, { status: 'awaiting_approval', phase: 'approval_broadcast' });
    return;
  }

  // Preflight simulation IMMEDIATELY before broadcast.
  const sim = await simulate({
    from: wallet.address,
    to: plan.contract,
    data: plan.callData,
    value: BigInt(plan.callValueWei),
  });
  if (!sim.ok) {
    await fail(
      row,
      'call_simulation',
      'SIMULATION_REVERTED',
      sim.error ?? 'call reverted in simulation',
      { detail: sim.error },
      true,
    );
    return;
  }

  const sent = await broadcastLeg(row, wallet, {
    to: plan.contract,
    data: plan.callData,
    value: BigInt(plan.callValueWei),
  });
  await updateAction(row.id, {
    status: 'call_pending',
    phase: 'call_confirmation',
    call_tx_hash: sent.hash,
    call_nonce: sent.nonce,
    error_code: null,
    error: null,
  });
}

// -----------------------------------------------------------------------------
// Confirmation (shared by approval + call legs) with fee-bump replacement.
// -----------------------------------------------------------------------------

async function confirmLeg(row: ActionRow, leg: 'approval' | 'call'): Promise<void> {
  const plan = row.plan as unknown as ActionPlan;
  const hash = (leg === 'approval' ? row.approval_tx_hash : row.call_tx_hash) as Hex | null;
  const nonce = leg === 'approval' ? row.approval_nonce : row.call_nonce;
  const phase: ActionPhase = leg === 'approval' ? 'approval_confirmation' : 'call_confirmation';

  if (!hash) {
    // Nothing broadcast for this leg — kick it back to the broadcast step.
    await updateAction(row.id, {
      status: leg === 'approval' ? 'awaiting_approval' : 'simulating_call',
    });
    return;
  }

  const outcome = await waitForReceipt(hash, { confirmations: CONFIRMATIONS });

  if (outcome.state === 'confirmed') {
    if (leg === 'approval') {
      await updateAction(row.id, { status: 'approval_confirmed', phase: 'call_simulation', error_code: null, error: null });
    } else {
      await finalizeCall(row, plan, outcome.receipt.tx_hash);
    }
    return;
  }

  if (outcome.state === 'reverted') {
    // A mined-but-reverted tx DID consume the nonce; never resend. Terminal.
    const code: ActionErrorCode = 'TRANSACTION_REVERTED';
    await fail(row, phase, code, `${leg} transaction reverted on-chain`, { tx_hash: hash, nonce }, false);
    return;
  }

  // Not yet mined. Decide between "keep polling" and "replace".
  const wallet = await requireWallet(row);
  if (!wallet) return;
  const known = await isTxKnown(hash);

  // Replacement authority is the WALLET's: only after enough waits AND once we
  // can act on the stored nonce. A still-known-but-underpriced tx is fee-bumped;
  // a dropped tx is re-sent at the SAME nonce. The agent never decides this.
  if (row.attempts >= REPLACE_AFTER_ATTEMPTS && nonce !== null) {
    await replaceLeg(row, wallet, leg, nonce, plan);
    return;
  }

  // Still pending: emit the POLL_ACTION envelope and stay in-flight.
  const code: ActionErrorCode = leg === 'approval' ? 'APPROVAL_PENDING' : 'CALL_PENDING';
  const envelope = buildErrorEnvelope({
    actionId: row.id,
    phase,
    code,
    message: `the ${leg} was broadcast but is not confirmed yet`,
    cause: { tx_hash: hash, nonce, still_in_mempool: known },
  });
  await updateAction(row.id, {
    status: known ? row.status : 'reconciling',
    phase,
    error_code: code,
    error: envelope,
  });
}

/** On-chain call success → attempt bounded credit verification, then terminal. */
async function finalizeCall(row: ActionRow, plan: ActionPlan, txHash: string): Promise<void> {
  if (plan.verify.type === 'layerx_credit' && layerxEnabled()) {
    const deposit = await getDepositByTx(txHash).catch(() => null);
    if (deposit) {
      await updateAction(row.id, {
        status: 'confirmed',
        phase: 'credit_verification',
        credit_verified: true,
        credit: {
          did: deposit.did,
          amount_usdx: deposit.amount_usdx,
          deposit_tx: deposit.deposit_tx,
          credited_at: deposit.created_at,
        },
        error_code: null,
        error: null,
      });
      return;
    }
    // On-chain deposit succeeded; the sequencer credit is eventual. Mark the
    // action confirmed (the deposit tx IS success) with credit_verified=false;
    // the LayerX sync worker backfills the credit when the row lands.
    await updateAction(row.id, {
      status: 'confirmed',
      phase: 'credit_verification',
      credit_verified: false,
      credit: { status: 'awaiting_sequencer', deposit_tx: txHash },
      error_code: null,
      error: null,
    });
    return;
  }
  await updateAction(row.id, {
    status: 'confirmed',
    phase: 'call_confirmation',
    credit_verified: true,
    error_code: null,
    error: null,
  });
}

// -----------------------------------------------------------------------------
// Reconciliation — an ambiguous leg (dropped/vanished) is re-examined here.
// -----------------------------------------------------------------------------

async function reconcile(row: ActionRow): Promise<void> {
  // Re-derive which leg is outstanding and re-run its confirmation. If the tx
  // has since mined this resolves cleanly; if still missing, confirmLeg will
  // decide to replace at the stored nonce (never a blind new send).
  if (row.call_tx_hash) {
    await updateAction(row.id, { status: 'call_pending' });
    return;
  }
  if (row.approval_tx_hash) {
    await updateAction(row.id, { status: 'approval_pending' });
    return;
  }
  // Nothing was ever broadcast — safe to restart from validation.
  await updateAction(row.id, { status: 'validating' });
}

// -----------------------------------------------------------------------------
// Broadcasting primitives (cross-process nonce lock + deterministic fee bump).
// -----------------------------------------------------------------------------

interface LegTx {
  to: `0x${string}`;
  data: Hex;
  value: bigint;
}

/** Sign + broadcast a leg under the distributed per-wallet nonce lock. */
async function broadcastLeg(
  row: ActionRow,
  wallet: WalletRow,
  tx: LegTx,
  opts?: { nonce?: number; feeBumpPercent?: number },
): Promise<{ hash: Hex; nonce: number }> {
  return withDistributedWalletLock(wallet.address, async () => {
    const account = await getSigningAccountForRow(wallet);
    const client = createWalletClient({
      chain: hyperPaxeer,
      transport: http(env.HYPERPAXEER_RPC_URL),
      account: {
        address: account.address,
        type: 'local',
        source: 'paxeer-embedded',
        publicKey: '0x' as Hex,
        signTransaction: account.signTransaction,
        signMessage: account.signMessage,
        signTypedData: account.signTypedData,
      },
    });

    const nonce = opts?.nonce ?? (await getNonce(wallet.address));
    const gas = await resolveGas({ from: account.address, to: tx.to, data: tx.data, value: tx.value });
    let { maxFeePerGas, maxPriorityFeePerGas } = gas;
    if (opts?.feeBumpPercent) {
      const mult = BigInt(100 + opts.feeBumpPercent);
      maxFeePerGas = (maxFeePerGas * mult) / 100n;
      maxPriorityFeePerGas = (maxPriorityFeePerGas * mult) / 100n;
    }

    const hash = await client.sendTransaction({
      to: tx.to,
      data: tx.data,
      value: tx.value,
      nonce,
      gas: gas.gas,
      maxFeePerGas,
      maxPriorityFeePerGas,
      chain: hyperPaxeer,
    });

    await logSignature({
      user_id: wallet.user_id,
      wallet_id: wallet.id,
      address: wallet.address,
      kind: 'transaction',
      to_address: tx.to,
      value_wei: tx.value,
      chain_id: env.HYPERPAXEER_CHAIN_ID,
      request_hash: `action:${row.id}:${opts?.nonce !== undefined ? 'replace' : 'send'}:${nonce}`,
      tx_hash: hash,
      principal_did: row.did,
    });

    return { hash, nonce };
  });
}

/** Deterministic replacement of a wedged leg at its EXISTING nonce. */
async function replaceLeg(
  row: ActionRow,
  wallet: WalletRow,
  leg: 'approval' | 'call',
  nonce: number,
  plan: ActionPlan,
): Promise<void> {
  const tx: LegTx =
    leg === 'approval'
      ? { to: plan.token, data: approvalCalldata(plan), value: 0n }
      : { to: plan.contract, data: plan.callData, value: BigInt(plan.callValueWei) };

  const sent = await broadcastLeg(row, wallet, tx, {
    nonce,
    feeBumpPercent: env.ACTION_FEE_BUMP_PERCENT,
  });
  // Point the action at the replacement hash and keep waiting on the same leg.
  await updateAction(
    row.id,
    leg === 'approval'
      ? { status: 'approval_pending', approval_tx_hash: sent.hash, error_code: null, error: null }
      : { status: 'call_pending', call_tx_hash: sent.hash, error_code: null, error: null },
  );
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

async function requireWallet(row: ActionRow): Promise<WalletRow | null> {
  const wallet = row.wallet_address ? await findWalletByAddress(row.wallet_address) : null;
  if (!wallet) {
    await fail(row, 'validation', 'INVALID_REQUEST', 'agent wallet not found', {}, true);
    return null;
  }
  return wallet;
}

/**
 * Record an unsuccessful step: persist the interpretation-free envelope and,
 * for terminal codes, the terminal status. Budget is released ONLY when nothing
 * was broadcast (no approval/call hash), honouring "budget rollback only when no
 * transaction was broadcast".
 */
async function fail(
  row: ActionRow,
  phase: ActionPhase,
  code: ActionErrorCode,
  message: string,
  cause: Record<string, unknown>,
  mayReleaseBudget: boolean,
): Promise<void> {
  const envelope = buildErrorEnvelope({ actionId: row.id, phase, code, message, cause });
  const terminal = terminalStatusFor(code) as ActionStatus | null;

  const nothingBroadcast = !row.approval_tx_hash && !row.call_tx_hash;
  if (mayReleaseBudget && nothingBroadcast && row.reserved_budget_id && BigInt(row.reserved_value_wei) > 0n) {
    await releaseBudget(row.reserved_budget_id, BigInt(row.reserved_value_wei)).catch(() => undefined);
  }

  await updateAction(row.id, {
    status: terminal ?? row.status,
    phase,
    error_code: code,
    error: envelope,
  });
}

/** Re-read + advance (used by tests + the reconciler for a single action). */
export async function advanceById(id: string): Promise<ActionRow | null> {
  const row = await getAction(id);
  if (!row) return null;
  await advanceAction(row);
  return getAction(id);
}
