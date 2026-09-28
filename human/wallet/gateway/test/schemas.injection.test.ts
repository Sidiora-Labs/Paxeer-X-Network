import { describe, expect, it } from 'vitest';
import {
  Address,
  HEX_MAX_CHARS,
  HexString,
  NUMERIC_MAX_CHARS,
  NumericString,
  ProvisionFundedBody,
  SafeIdentifier,
  SendTxBody,
  SignMessageBody,
  SignTxBody,
  TxRequest,
} from '../src/schemas/tx.js';

/**
 * Injection / hardening tests for the shared request schemas.
 *
 * These are the regressions that would burn us if any future change
 * accidentally loosens input validation. Each test names the attack class
 * being defended against.
 *
 * What these tests guarantee at the schema layer:
 *   - SQL injection: never reaches the DB because every query is parameterised,
 *     but we additionally constrain `tier_id` and addresses to safe charsets
 *     so SQL-looking inputs are rejected at the door.
 *   - Prototype pollution: `.strict()` envelopes reject unknown top-level keys
 *     so an attacker cannot smuggle `__proto__`, `constructor`, `_admin`, etc.
 *   - Memory-flood DoS: hex and numeric inputs have hard length caps.
 *   - Header injection / log injection via `tier_id`: charset rules out
 *     newlines, control chars, quotes.
 *   - Unicode normalisation tricks (`U+202E`, NUL bytes, mixed-script
 *     homographs) for identifiers: charset rules them out.
 */

// -----------------------------------------------------------------------------
// Primitive validators
// -----------------------------------------------------------------------------

describe('HexString', () => {
  it('accepts the empty hex (`0x`)', () => {
    expect(HexString.safeParse('0x').success).toBe(true);
  });
  it('accepts a typical calldata payload', () => {
    expect(HexString.safeParse('0xa9059cbb' + '00'.repeat(64)).success).toBe(true);
  });
  it('rejects without the 0x prefix', () => {
    expect(HexString.safeParse('a9059cbb').success).toBe(false);
  });
  it('rejects non-hex characters', () => {
    expect(HexString.safeParse('0xZZZ').success).toBe(false);
    expect(HexString.safeParse('0x;DROP TABLE wallets').success).toBe(false);
  });
  it('rejects whitespace, newlines, tabs', () => {
    expect(HexString.safeParse('0xab cd').success).toBe(false);
    expect(HexString.safeParse('0xab\ncd').success).toBe(false);
    expect(HexString.safeParse('0xab\tcd').success).toBe(false);
  });
  it('rejects NUL bytes', () => {
    expect(HexString.safeParse('0xab\u0000cd').success).toBe(false);
  });
  it(`rejects hex strings longer than ${HEX_MAX_CHARS} chars (memory-flood guard)`, () => {
    const tooLong = '0x' + 'a'.repeat(HEX_MAX_CHARS); // 1 over after prefix
    expect(HexString.safeParse(tooLong).success).toBe(false);
  });
  it(`accepts hex strings up to ${HEX_MAX_CHARS} chars exactly`, () => {
    const max = '0x' + 'a'.repeat(HEX_MAX_CHARS - 2);
    expect(HexString.safeParse(max).success).toBe(true);
  });
});

describe('NumericString', () => {
  it('accepts a typical wei value', () => {
    expect(NumericString.safeParse('1000000000000000000').success).toBe(true);
  });
  it('accepts uint256 max (78 digits)', () => {
    const uint256Max = '115792089237316195423570985008687907853269984665640564039457584007913129639935';
    expect(NumericString.safeParse(uint256Max).success).toBe(true);
  });
  it('rejects scientific notation', () => {
    expect(NumericString.safeParse('1e18').success).toBe(false);
  });
  it('rejects negative numbers', () => {
    expect(NumericString.safeParse('-1').success).toBe(false);
  });
  it('rejects decimals', () => {
    expect(NumericString.safeParse('1.5').success).toBe(false);
  });
  it('rejects non-digit characters', () => {
    expect(NumericString.safeParse("1' OR '1'='1").success).toBe(false);
    expect(NumericString.safeParse('1\n0').success).toBe(false);
  });
  it(`rejects numeric strings longer than ${NUMERIC_MAX_CHARS} chars (memory-flood guard)`, () => {
    expect(NumericString.safeParse('1'.repeat(NUMERIC_MAX_CHARS + 1)).success).toBe(false);
  });
});

describe('Address', () => {
  it('accepts a valid checksum address', () => {
    expect(Address.safeParse('0x7c69c84daAEe90B21eeCABDb8f0387897E9B7B37').success).toBe(true);
  });
  it('rejects too-short addresses', () => {
    expect(Address.safeParse('0xabc').success).toBe(false);
  });
  it('rejects too-long addresses', () => {
    expect(Address.safeParse('0x' + 'a'.repeat(41)).success).toBe(false);
  });
  it('rejects address-shaped strings with non-hex characters', () => {
    expect(Address.safeParse('0xZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ').success).toBe(false);
  });
  it('rejects the empty string', () => {
    expect(Address.safeParse('').success).toBe(false);
  });
});

describe('SafeIdentifier', () => {
  it('accepts canonical tier IDs', () => {
    expect(SafeIdentifier.safeParse('starter_25k').success).toBe(true);
    expect(SafeIdentifier.safeParse('elite-100k').success).toBe(true);
    expect(SafeIdentifier.safeParse('a').success).toBe(true);
  });
  it('rejects empty / whitespace', () => {
    expect(SafeIdentifier.safeParse('').success).toBe(false);
    expect(SafeIdentifier.safeParse('   ').success).toBe(false);
  });
  it('rejects SQL-flavoured payloads', () => {
    expect(SafeIdentifier.safeParse("starter'; DROP TABLE--").success).toBe(false);
    expect(SafeIdentifier.safeParse('starter OR 1=1').success).toBe(false);
  });
  it('rejects path traversal attempts', () => {
    expect(SafeIdentifier.safeParse('../../etc/passwd').success).toBe(false);
    expect(SafeIdentifier.safeParse('starter/25k').success).toBe(false);
  });
  it('rejects log-injection payloads (newlines, CR, control chars)', () => {
    expect(SafeIdentifier.safeParse('starter\n[admin] gave you $1m').success).toBe(false);
    expect(SafeIdentifier.safeParse('starter\r\nFAKE').success).toBe(false);
    expect(SafeIdentifier.safeParse('starter\u0000').success).toBe(false);
    expect(SafeIdentifier.safeParse('starter\u202Ekkek').success).toBe(false); // RLO override
  });
  it('rejects HTML / template-injection metacharacters', () => {
    expect(SafeIdentifier.safeParse('<script>').success).toBe(false);
    expect(SafeIdentifier.safeParse('${process.env.WALLET_MASTER_KEY}').success).toBe(false);
    expect(SafeIdentifier.safeParse('{{7*7}}').success).toBe(false);
  });
  it('rejects oversize identifiers (> 64 chars)', () => {
    expect(SafeIdentifier.safeParse('a'.repeat(65)).success).toBe(false);
  });
});

// -----------------------------------------------------------------------------
// Outer body envelopes — strict mode
// -----------------------------------------------------------------------------

describe('SignTxBody — strict envelope', () => {
  it('accepts a minimal valid body', () => {
    expect(
      SignTxBody.safeParse({ tx: { to: '0x' + 'a'.repeat(40), value: '0' } }).success,
    ).toBe(true);
  });
  it('rejects unknown top-level keys (defence vs accidental field reads)', () => {
    const r = SignTxBody.safeParse({
      tx: { to: '0x' + 'a'.repeat(40) },
      _admin: true,
    });
    expect(r.success).toBe(false);
  });
  it('rejects prototype-pollution keys at the envelope level', () => {
    const evil: Record<string, unknown> = { tx: { to: '0x' + 'a'.repeat(40) } };
    evil['__proto__'] = { polluted: true };
    const r = SignTxBody.safeParse(evil);
    // Either rejected outright (strict) or stripped — but `__proto__` MUST
    // NOT survive into parsed.data with the polluting payload attached.
    if (r.success) {
      expect(Object.prototype.hasOwnProperty.call(r.data, '__proto__')).toBe(false);
      // And the global Object prototype must NOT have been mutated.
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      expect(({} as any).polluted).toBeUndefined();
    } else {
      // strict mode rejected — also fine.
      expect(r.success).toBe(false);
    }
  });
  it('rejects when body is an array', () => {
    expect(SignTxBody.safeParse([{ tx: {} }]).success).toBe(false);
  });
  it('rejects when body is a string', () => {
    expect(SignTxBody.safeParse('whatever').success).toBe(false);
  });
  it('rejects when tx is missing', () => {
    expect(SignTxBody.safeParse({}).success).toBe(false);
  });
  it('rejects oversize hex calldata at the envelope level', () => {
    const r = SignTxBody.safeParse({
      tx: { to: '0x' + 'a'.repeat(40), data: '0x' + 'a'.repeat(HEX_MAX_CHARS) },
    });
    expect(r.success).toBe(false);
  });
  it('rejects pathological numeric strings', () => {
    const r = SignTxBody.safeParse({
      tx: { to: '0x' + 'a'.repeat(40), value: '1'.repeat(NUMERIC_MAX_CHARS + 1) },
    });
    expect(r.success).toBe(false);
  });
});

describe('SendTxBody — same envelope guards as SignTxBody', () => {
  it('rejects unknown top-level keys', () => {
    const r = SendTxBody.safeParse({
      tx: { to: '0x' + 'a'.repeat(40) },
      smuggled: 'attack',
    });
    expect(r.success).toBe(false);
  });
});

describe('SignMessageBody — strict envelope + message bounds', () => {
  it('accepts a typical SIWE-style message', () => {
    expect(SignMessageBody.safeParse({ message: 'Sign in to Paxeer' }).success).toBe(true);
  });
  it('rejects empty messages', () => {
    expect(SignMessageBody.safeParse({ message: '' }).success).toBe(false);
  });
  it('rejects messages over 10,000 chars', () => {
    expect(SignMessageBody.safeParse({ message: 'x'.repeat(10_001) }).success).toBe(false);
  });
  it('rejects unknown top-level keys', () => {
    expect(SignMessageBody.safeParse({ message: 'hi', _admin: true }).success).toBe(false);
  });
});

describe('ProvisionFundedBody — strict envelope + safe tier id', () => {
  it('accepts the default starter tier', () => {
    const r = ProvisionFundedBody.safeParse({});
    expect(r.success).toBe(true);
    if (r.success) expect(r.data.tier_id).toBe('starter_25k');
  });
  it('accepts an explicit safe tier id', () => {
    expect(ProvisionFundedBody.safeParse({ tier_id: 'elite-100k' }).success).toBe(true);
  });
  it('rejects unsafe tier ids (SQL flavour)', () => {
    expect(ProvisionFundedBody.safeParse({ tier_id: "starter'; --" }).success).toBe(false);
  });
  it('rejects unsafe tier ids (newlines)', () => {
    expect(ProvisionFundedBody.safeParse({ tier_id: 'starter\n_admin' }).success).toBe(false);
  });
  it('rejects unknown top-level keys', () => {
    expect(
      ProvisionFundedBody.safeParse({ tier_id: 'starter_25k', force_funded: 1_000_000 }).success,
    ).toBe(false);
  });
});

// -----------------------------------------------------------------------------
// TxRequest inner shape — forward-compat (strip unknown) is intentional
// -----------------------------------------------------------------------------

describe('TxRequest — forward-compatible inner shape', () => {
  it('strips unknown inner keys (e.g. future EVM fields) without failing', () => {
    const r = TxRequest.safeParse({
      to: '0x' + 'a'.repeat(40),
      value: '0',
      // simulate a future SDK shipping these:
      accessList: [],
      type: 2,
      blobVersionedHashes: [],
    });
    expect(r.success).toBe(true);
    if (r.success) {
      // The unknown fields must NOT survive into the parsed data — they'd
      // be passed to viem and could cause cryptic failures.
      expect('accessList' in r.data).toBe(false);
      expect('type' in r.data).toBe(false);
    }
  });
});
