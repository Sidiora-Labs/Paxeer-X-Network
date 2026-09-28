import {
  type FundedAccountRow,
  TRADEABLE_STATUSES,
  whitelistAllows,
  whitelistAllowsNativeValue,
} from '../db/fundedAccounts.js';

/**
 * Server-side gating for funded ("prop-firm tier") wallets.
 *
 * Defence-in-depth, evaluated top-to-bottom. ANY failure short-circuits with
 * a structured deny so the SDK can surface a precise error to the user
 * before they think a tx is in flight.
 *
 *   0. Account status must be tradeable.
 *      → ACCOUNT_BREACHED / ACCOUNT_CLOSED
 *
 *   1. No bare native transfers (`value > 0 AND data === '0x'`).
 *      A funded wallet can NEVER send PAX directly. Gas is consumed by
 *      whitelisted contract calls; sending PAX to an EOA is a withdrawal.
 *      → WITHDRAWAL_BLOCKED
 *
 *   2. No contract creation (`to === undefined`).
 *      Funded wallets do not deploy code.
 *      → CONTRACT_CREATION_BLOCKED
 *
 *   3. Contract whitelist match on (to, selector) for this tier.
 *      Exact match wins, contract-wildcard is fallback.
 *      → CONTRACT_NOT_WHITELISTED
 *
 *   4. msg.value gate. If tx.value > 0, the matched whitelist entry MUST
 *      have allow_native_value=true. Today no entries set this, so any
 *      contract call with value > 0 is denied (the perps stack pulls USDL,
 *      it doesn't receive PAX).
 *      → NATIVE_VALUE_NOT_ALLOWED
 *
 *   5. Approve sub-rule. If selector is ERC-20 approve(address,uint256)
 *      = 0x095ea7b3, decode the `spender` arg from data and verify it's
 *      also whitelisted for this tier (any selector).
 *      → APPROVE_SPENDER_NOT_WHITELISTED
 *
 *   (5a. The increaseAllowance / decreaseAllowance selectors are also covered
 *        by the same spender sub-rule. v1 only enforces approve. If users
 *        complain, extend with selectors 0x39509351 / 0xa457c2d7.)
 *
 *   6. transferFrom + transfer selectors are intentionally NOT in the seed
 *      whitelist for the USDL token row. They'll fail rule 3 with
 *      CONTRACT_NOT_WHITELISTED before getting here. This is the kill switch
 *      against direct ERC-20 withdrawals.
 *
 * Returns:
 *   - { allow: true }                       — proceed to standard policy
 *   - { allow: false, code, message, ...metadata } — 4xx the request
 */

// -----------------------------------------------------------------------------
// Selector table — kept tiny on purpose. Add here, then add to whitelist.
// -----------------------------------------------------------------------------

export const ERC20_APPROVE_SELECTOR = '0x095ea7b3';
export const ERC20_TRANSFER_SELECTOR = '0xa9059cbb';
export const ERC20_TRANSFER_FROM_SELECTOR = '0x23b872dd';

// -----------------------------------------------------------------------------
// Types
// -----------------------------------------------------------------------------

export type FundedDecision =
  | { allow: true }
  | {
      allow: false;
      code: FundedDenyCode;
      message: string;
      /** Surfaced to the SDK so the UI can highlight the offending address. */
      contract?: string;
      selector?: string;
      spender?: string;
      status?: string;
    };

export type FundedDenyCode =
  | 'ACCOUNT_BREACHED'
  | 'ACCOUNT_CLOSED'
  | 'WITHDRAWAL_BLOCKED'
  | 'CONTRACT_CREATION_BLOCKED'
  | 'CONTRACT_NOT_WHITELISTED'
  | 'NATIVE_VALUE_NOT_ALLOWED'
  | 'APPROVE_SPENDER_NOT_WHITELISTED'
  | 'INVALID_TX_SHAPE';

export interface FundedPolicyInput {
  account: FundedAccountRow;
  tx: {
    to?: string;          // 0x… address or undefined (contract creation)
    value?: bigint;       // wei
    data?: string;        // 0x-prefixed hex or undefined / '0x'
  };
}

// -----------------------------------------------------------------------------
// Engine
// -----------------------------------------------------------------------------

export async function evaluateFunded(input: FundedPolicyInput): Promise<FundedDecision> {
  const { account, tx } = input;

  // 0. Status gate.
  if (!TRADEABLE_STATUSES.has(account.status)) {
    if (account.status === 'closed') {
      return {
        allow: false,
        code: 'ACCOUNT_CLOSED',
        message: 'funded account is closed and cannot trade',
        status: account.status,
      };
    }
    return {
      allow: false,
      code: 'ACCOUNT_BREACHED',
      message: `funded account is in non-tradeable state: ${account.status}${
        account.breached_reason ? ` (${account.breached_reason})` : ''
      }`,
      status: account.status,
    };
  }

  const data = (tx.data ?? '0x').toLowerCase();
  const value = tx.value ?? 0n;
  const isEmptyData = data === '0x' || data === '';

  // 1. Bare native transfer = withdrawal. Always denied.
  if (value > 0n && isEmptyData) {
    return {
      allow: false,
      code: 'WITHDRAWAL_BLOCKED',
      message:
        'native PAX transfers are not allowed on funded accounts; trade through whitelisted protocols only',
    };
  }

  // 2. Contract creation. Always denied.
  if (!tx.to) {
    return {
      allow: false,
      code: 'CONTRACT_CREATION_BLOCKED',
      message: 'contract creation is not allowed on funded accounts',
    };
  }

  // If data is empty AND value is zero, that's a 0-value PAX self-call;
  // permit it (no-op, harmless, gas-burning). Not whitelisted-checked.
  if (isEmptyData && value === 0n) {
    return { allow: true };
  }

  // 3. Whitelist lookup on (tier, contract, selector).
  if (data.length < 10) {
    // Has value > 0 OR non-empty data but too short for a selector. Reject.
    return {
      allow: false,
      code: 'INVALID_TX_SHAPE',
      message: `tx.data must be empty or contain a 4-byte selector; got ${data.length} chars`,
    };
  }
  const contract = tx.to.toLowerCase();
  const selector = data.slice(0, 10); // 0x + 8 hex
  const matched = await whitelistAllows(account.tier_id, contract, selector);
  if (!matched) {
    return {
      allow: false,
      code: 'CONTRACT_NOT_WHITELISTED',
      message: `target (contract, selector) is not on the funded-tier whitelist`,
      contract,
      selector,
    };
  }

  // 4. msg.value gate — currently every whitelisted entry has allow_native_value=false.
  //    Re-enable per-row by setting allow_native_value=true in whitelist_entries.
  if (value > 0n) {
    const nativeOk = await whitelistAllowsNativeValue(account.tier_id, contract, selector);
    if (!nativeOk) {
      return {
        allow: false,
        code: 'NATIVE_VALUE_NOT_ALLOWED',
        message:
          'this whitelisted call does not accept native PAX as value; pass value=0',
        contract,
        selector,
      };
    }
  }

  // 5. Approve sub-rule — decode spender and verify it's whitelisted.
  if (selector === ERC20_APPROVE_SELECTOR) {
    const spender = decodeAddressArg(data, 0);
    if (!spender) {
      return {
        allow: false,
        code: 'INVALID_TX_SHAPE',
        message: 'could not decode spender argument from approve() call',
      };
    }
    const spenderOk = await whitelistAllows(account.tier_id, spender, null);
    if (!spenderOk) {
      return {
        allow: false,
        code: 'APPROVE_SPENDER_NOT_WHITELISTED',
        message: `approve() spender is not on the funded-tier whitelist`,
        contract,
        selector,
        spender,
      };
    }
  }

  return { allow: true };
}

// -----------------------------------------------------------------------------
// Internal — minimal ABI-decode helper. No viem dependency to keep this hot path
// allocation-light and trivially testable.
//
// EVM ABI:
//   - selector:           bytes 0..4   (chars 2..10 in 0x-prefixed string)
//   - arg N (32 bytes):   bytes 4+32N..4+32(N+1)
//   - address arg is right-padded in a 32-byte slot: 12 bytes 0x00 then 20 bytes
//     of the address. In a hex string this is 24 chars of '0' then 40 hex chars.
// -----------------------------------------------------------------------------

/**
 * Decode the N-th address argument (0-indexed) from a 0x-prefixed calldata
 * string. Returns the address in lowercase 0x-prefixed form, or null if the
 * input is malformed.
 */
export function decodeAddressArg(data: string, argIndex: number): string | null {
  // Each arg is 32 bytes = 64 hex chars. Selector consumes 4 bytes = 8 hex.
  // Offset in the hex string (with 0x prefix): 2 + 8 + 64 * argIndex.
  const start = 2 + 8 + 64 * argIndex;
  const end = start + 64;
  if (data.length < end) return null;
  const slot = data.slice(start, end);
  // 12 leading zero bytes = 24 hex zeros, then 40-hex address.
  if (!/^0{24}[0-9a-fA-F]{40}$/.test(slot)) return null;
  return `0x${slot.slice(24).toLowerCase()}`;
}
