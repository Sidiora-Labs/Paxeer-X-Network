import { z } from 'zod';

/**
 * Shared input validators for the signing endpoints.
 *
 * Defined ONCE here and reused by every route so length caps, charsets, and
 * unknown-key policy can't drift between `/v1/wallet/*` and `/v1/funded/*`.
 *
 * Hardening rules baked in:
 *
 *   - HexString — must match `0x[hex]*` AND be ≤ 32 KB of payload (= 64 KB
 *     hex chars + 2 prefix chars). Real EVM calldata is ≤ a few KB; the cap
 *     blocks memory-flood DoS via a 1 MB body field while leaving generous
 *     headroom for legitimate large encodings.
 *
 *   - NumericString — `^\d+$` AND ≤ 80 chars. A uint256 is at most 78 decimal
 *     digits; we permit 80 for safety. Blocks pathological `BigInt('1' × 1e6)`
 *     allocations that could pin a worker.
 *
 *   - Address — already strict (`0x` + 40 hex). No length cap needed.
 *
 *   - SafeIdentifier — `[a-z0-9_-]`, 1–64 chars. Used for tier IDs and any
 *     other identifier that may end up in error messages, logs, or future
 *     templated contexts. Refuses whitespace, quotes, control chars, slashes.
 *
 *   - All body schemas use `.strict()` so unknown top-level keys cause a
 *     400 instead of being silently stripped. This catches both client bugs
 *     and the (extremely unlikely) case of someone shipping a future feature
 *     that reads `req.body` directly instead of `parsed.data`.
 *
 *   - The inner `TxRequest` shape uses `.strip()` (Zod default) so that we
 *     remain forward-compatible with new EVM tx fields (accessList, type,
 *     blobVersionedHashes, etc.) that the SDK may emit; only the known fields
 *     are propagated to viem.
 */

// -----------------------------------------------------------------------------
// Primitives
// -----------------------------------------------------------------------------

/** Calldata / arbitrary hex blob. Body limit caps total request at 1 MB. */
export const HEX_MAX_CHARS = 65_538; // 0x + 32 KB hex
export const HexString = z
  .string()
  .max(HEX_MAX_CHARS, `hex string exceeds ${HEX_MAX_CHARS} chars`)
  .regex(/^0x[0-9a-fA-F]*$/, 'must be a 0x-prefixed hex string');

/** uint256-shaped decimal string. */
export const NUMERIC_MAX_CHARS = 80;
export const NumericString = z
  .string()
  .max(NUMERIC_MAX_CHARS, `numeric string exceeds ${NUMERIC_MAX_CHARS} digits`)
  .regex(/^\d+$/, 'must be a decimal integer string');

/** EVM 20-byte address. */
export const Address = z
  .string()
  .regex(/^0x[0-9a-fA-F]{40}$/, 'must be a 0x-prefixed 20-byte address');

/**
 * Safe identifier — alphanumeric + `_` + `-`. Use for any value that we hold
 * in code paths beyond a single SQL-parameterised query (error messages,
 * audit logs, future URL fragments, etc).
 */
export const SafeIdentifier = z
  .string()
  .min(1)
  .max(64)
  .regex(/^[a-zA-Z0-9_-]+$/, 'identifier may only contain [A-Za-z0-9_-]');

// -----------------------------------------------------------------------------
// Tx shape — shared by both wallet and funded sign/send endpoints
// -----------------------------------------------------------------------------

/**
 * EVM transaction request — a deliberately small subset of fields. Server
 * fills in nonce / gas / chainId from chain state where the client omits them.
 *
 * Forward-compatible: extra unknown fields are stripped by Zod's default
 * behaviour, so a newer SDK shipping `accessList` etc. won't be rejected;
 * those fields just don't flow into viem until we explicitly extend this.
 */
export const TxRequest = z.object({
  to: Address.optional(),
  value: NumericString.optional(),
  data: HexString.optional(),
  gas: NumericString.optional(),
  maxFeePerGas: NumericString.optional(),
  maxPriorityFeePerGas: NumericString.optional(),
  nonce: z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER).optional(),
  chainId: z.number().int().positive().max(Number.MAX_SAFE_INTEGER).optional(),
});
export type TxRequest = z.infer<typeof TxRequest>;

// -----------------------------------------------------------------------------
// Outer body envelopes — strict so unknown top-level keys reject with 400
// -----------------------------------------------------------------------------

export const SignTxBody = z.object({ tx: TxRequest }).strict();
export const SendTxBody = z.object({ tx: TxRequest }).strict();

export const SignMessageBody = z
  .object({
    message: z.string().min(1).max(10_000),
  })
  .strict();

export const ProvisionFundedBody = z
  .object({
    tier_id: SafeIdentifier.default('starter_25k'),
  })
  .strict();
