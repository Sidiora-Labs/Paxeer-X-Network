import { describe, expect, it } from 'vitest';
import { parseAtomicAmount, parseEip681 } from './eip681';

const RECIPIENT = '0xf8850b62AE017c55be7f571BBad840b4f3DA7D49';
const TOKEN = '0x4b29871681c95DFB2c7824BC4b0326B80217bCe8';

describe('EIP-681 parser', () => {
  it('parses plain addresses and native payment requests', () => {
    expect(parseEip681(RECIPIENT)).toEqual({
      address: RECIPIENT.toLowerCase(),
    });
    expect(
      parseEip681(`ethereum:${RECIPIENT}@125?value=1500000000000000000`),
    ).toEqual({
      address: RECIPIENT.toLowerCase(),
      chainId: 125,
      value: '1500000000000000000',
    });
  });

  it('parses ERC-20 transfers without replacing token and recipient identity', () => {
    expect(
      parseEip681(
        `ethereum:${TOKEN}@125/transfer?address=${RECIPIENT}&uint256=1250000`,
      ),
    ).toEqual({
      address: RECIPIENT.toLowerCase(),
      tokenAddress: TOKEN.toLowerCase(),
      functionName: 'transfer',
      chainId: 125,
      uint256: '1250000',
    });
  });

  it('rejects malformed chains, addresses, functions, and numeric values', () => {
    expect(parseEip681(`ethereum:${RECIPIENT}@0`)).toBeNull();
    expect(parseEip681('ethereum:0x1234@125')).toBeNull();
    expect(
      parseEip681(`ethereum:${TOKEN}@125/approve?address=${RECIPIENT}&uint256=1`),
    ).toBeNull();
    expect(parseEip681(`ethereum:${RECIPIENT}@125?value=-1`)).toBeNull();
  });

  it('converts bounded scientific atomic amounts exactly', () => {
    expect(parseAtomicAmount('1.5e18')).toBe(1_500_000_000_000_000_000n);
    expect(parseAtomicAmount('1.25e2')).toBe(125n);
    expect(parseAtomicAmount('1.25e1')).toBeNull();
    expect(parseAtomicAmount('1e101')).toBeNull();
  });
});
