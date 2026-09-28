import { z } from 'zod';
import { Address, HexString, NumericString, TxRequest } from './tx.js';

/**
 * Validators for the agent-native lane (auth, capabilities, precompiles) and
 * the owner control plane. Reuses the hardened primitives from schemas/tx.ts
 * (length caps, charsets) so limits can't drift between lanes.
 */

// -----------------------------------------------------------------------------
// Primitives
// -----------------------------------------------------------------------------

export const DidString = z
  .string()
  .max(256)
  .regex(/^did:matrix:[A-Za-z0-9_-]{1,128}:[0-9a-f]{16}$/, 'must be a did:matrix:<label>:<keyfp>');

/** 0x + 64 hex (32-byte ed25519 public key). */
export const Ed25519PublicKey = z
  .string()
  .regex(/^(0x)?[0-9a-fA-F]{64}$/, 'must be a 32-byte hex ed25519 public key');

/** 0x + 128 hex (64-byte ed25519 signature). */
export const Ed25519Signature = z
  .string()
  .regex(/^(0x)?[0-9a-fA-F]{128}$/, 'must be a 64-byte hex ed25519 signature');

export const Bytes32 = z.string().regex(/^0x[0-9a-fA-F]{64}$/, 'must be a 0x 32-byte hex value');

const AgentMode = z.enum(['read_only', 'trade_only', 'full']);

// -----------------------------------------------------------------------------
// Auth
// -----------------------------------------------------------------------------

export const ChallengeBody = z.object({ did: DidString }).strict();

export const VerifyBody = z
  .object({
    did: DidString,
    public_key: Ed25519PublicKey,
    nonce: z.string().min(8).max(128),
    signature: Ed25519Signature,
  })
  .strict();

// -----------------------------------------------------------------------------
// Agent capability surface
// -----------------------------------------------------------------------------

export const AgentSignTxBody = z.object({ tx: TxRequest }).strict();
export const AgentSendTxBody = z.object({ tx: TxRequest }).strict();
export const AgentSimulateBody = z.object({ tx: TxRequest }).strict();

export const AgentSignMessageBody = z.object({ message: z.string().min(1).max(10_000) }).strict();

/** EIP-712 typed-data payload, passed through to viem.signTypedData. */
export const AgentSignTypedDataBody = z
  .object({
    typedData: z
      .object({
        domain: z.record(z.unknown()).optional(),
        types: z.record(z.unknown()),
        primaryType: z.string().min(1),
        message: z.record(z.unknown()),
      })
      .passthrough(),
  })
  .strict();

/** High-level transfer: token=null/absent → native PAX; else ERC-20. */
export const AgentTransferBody = z
  .object({
    token: Address.nullish(),
    to: Address,
    amount: NumericString,
  })
  .strict();

export const AgentApproveBody = z
  .object({
    token: Address,
    spender: Address,
    amount: NumericString,
  })
  .strict();

export const AllowanceQuery = z
  .object({
    token: Address,
    spender: Address,
  })
  .strict();

// -----------------------------------------------------------------------------
// Durable actions (high-level intent lane)
// -----------------------------------------------------------------------------

/** Human decimal amount, e.g. "250" or "250.5". ≤ 80 chars. */
export const DecimalString = z
  .string()
  .max(80)
  .regex(/^\d+(\.\d+)?$/, 'must be a decimal amount string');

/** Client-supplied idempotency key: safe charset, bounded length. */
export const IdempotencyKey = z
  .string()
  .min(8)
  .max(128)
  .regex(/^[A-Za-z0-9_.:-]+$/, 'idempotency_key may only contain [A-Za-z0-9_.:-]');

/** Solidity method signature, e.g. "depositUSDL(uint256,bytes32)". */
export const MethodSignature = z
  .string()
  .min(3)
  .max(256)
  .regex(/^[a-zA-Z_$][a-zA-Z0-9_$]*\((|[a-zA-Z0-9_,\[\] ]*)\)$/, 'must be a Solidity function signature');

export const LayerxDepositBody = z
  .object({
    asset: z.literal('USDL'),
    amount: DecimalString,
    did_claim: Bytes32,
    idempotency_key: IdempotencyKey,
  })
  .strict();

export const AllowanceAndCallBody = z
  .object({
    token: Address,
    amount: NumericString, // RAW base units (advanced route does no decimal conversion)
    spender: Address,
    contract: Address,
    method: MethodSignature,
    args: z.array(z.string().max(2_048)).max(32),
    idempotency_key: IdempotencyKey,
  })
  .strict();

export const ActionIdParam = z
  .object({ action_id: z.string().regex(/^act_[0-9a-f]{24}$/, 'malformed action_id') })
  .strict();

// -----------------------------------------------------------------------------
// Precompiles
// -----------------------------------------------------------------------------

export const ScheduleBody = z
  .object({
    target: Address,
    call_data: HexString.optional(),
    execute_at_block: NumericString,
    gas_limit: NumericString,
    deposit_wei: NumericString.optional(),
  })
  .strict();

export const SchedulerJobIdBody = z.object({ job_id: NumericString }).strict();
export const RescheduleBody = z.object({ job_id: NumericString, new_block: NumericString }).strict();

export const StreamOpenBody = z
  .object({
    payee: Address,
    token: Address,
    rate_per_second: NumericString,
    start_time: NumericString.optional(),
    stop_time: NumericString.optional(),
    cap: NumericString,
  })
  .strict();

export const StreamIdBody = z.object({ stream_id: NumericString }).strict();
export const StreamUpdateRateBody = z
  .object({ stream_id: NumericString, new_rate: NumericString })
  .strict();

export const Eip712DomainSeparatorBody = z
  .object({
    name: z.string().min(1).max(128),
    version: z.string().min(1).max(64),
    chain_id: NumericString.optional(),
    verifying_contract: Address,
  })
  .strict();

export const Eip712HashBody = z
  .object({ domain_separator: Bytes32, struct_hash: Bytes32 })
  .strict();

export const Eip712RecoverBody = z
  .object({ domain_separator: Bytes32, struct_hash: Bytes32, signature: HexString })
  .strict();

export const TeeFamily = z.union([
  z.enum(['intel_tdx', 'amd_sev_snp', 'nvidia_h100', 'intel_sgx']),
  z.number().int().min(0).max(3),
]);

export const TeeVerifyBody = z
  .object({
    family: TeeFamily,
    quote: HexString,
    expected_report_data: Bytes32.optional(),
  })
  .strict();

export const TeeRootBody = z
  .object({ family: TeeFamily, index: NumericString.optional() })
  .strict();

// -----------------------------------------------------------------------------
// Owner control plane
// -----------------------------------------------------------------------------

export const PolicyPatchBody = z
  .object({
    mode: AgentMode.optional(),
    max_tx_value_wei: NumericString.nullable().optional(),
    max_daily_value_wei: NumericString.nullable().optional(),
    rate_limit_per_min: z.number().int().positive().max(10_000).nullable().optional(),
    max_approve_wei: NumericString.nullable().optional(),
    allow_native_transfer: z.boolean().optional(),
    withdrawal_allowlist_only: z.boolean().optional(),
    daily_reset_utc_hour: z.number().int().min(0).max(23).optional(),
  })
  .strict()
  .refine((o) => Object.keys(o).length > 0, { message: 'at least one field required' });

export const RuleBody = z
  .object({
    effect: z.enum(['allow', 'deny']),
    subject: z.enum(['contract', 'selector', 'token', 'address', 'withdrawal']),
    value: z.string().min(3).max(66),
    max_value_wei: NumericString.nullish(),
    note: z.string().max(256).nullish(),
  })
  .strict();

export const BudgetBody = z
  .object({
    target_contract: Address.nullish(),
    token: Address.nullish(),
    cap_wei: NumericString,
    expires_in_seconds: z.number().int().positive().max(31_536_000), // ≤ 1 year
  })
  .strict();

export const FreezeBody = z.object({ frozen: z.boolean() }).strict();

export const FundAgentBody = z
  .object({
    token: Address.nullish(), // null → native PAX
    amount: NumericString,
  })
  .strict();

export const SweepBody = z
  .object({
    token: Address.nullish(), // null → native PAX
    amount: NumericString,
    to: Address.optional(), // defaults to the owner's standard wallet
  })
  .strict();
