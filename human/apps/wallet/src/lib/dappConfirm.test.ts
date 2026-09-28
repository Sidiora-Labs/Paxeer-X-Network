import { describe, expect, it, vi } from 'vitest';
import { ethers } from 'ethers';
import {
  confirmDAppRequest,
  decodePersonalSignMessage,
  isLikelyAddress,
  needsDAppConfirmation,
  parseSignRequest,
  toSignableBytes,
} from './dappConfirm';

const ADDR = '0xf8850b62AE017c55be7f571BBad840b4f3DA7D49';

describe('DApp confirmation helpers', () => {
  it('requires confirmations for all signing and transaction methods', () => {
    expect(needsDAppConfirmation('personal_sign')).toBe(true);
    expect(needsDAppConfirmation('eth_sign')).toBe(true);
    expect(needsDAppConfirmation('eth_signTypedData')).toBe(true);
    expect(needsDAppConfirmation('eth_signTypedData_v3')).toBe(true);
    expect(needsDAppConfirmation('eth_signTypedData_v4')).toBe(true);
    expect(needsDAppConfirmation('eth_sendTransaction')).toBe(true);
    expect(needsDAppConfirmation('eth_chainId')).toBe(false);
  });

  it('decodes hex personal_sign payloads for display', () => {
    expect(decodePersonalSignMessage('0x48656c6c6f')).toBe('Hello');
    expect(decodePersonalSignMessage('plain text')).toBe('plain text');
  });

  it('detects address-shaped params', () => {
    expect(isLikelyAddress(ADDR)).toBe(true);
    expect(isLikelyAddress('0x1234')).toBe(false);
    expect(isLikelyAddress('Upload metadata')).toBe(false);
  });

  it('parses personal_sign regardless of argument order', () => {
    // MetaMask / viem order: [message, address]
    expect(parseSignRequest('personal_sign', ['0x48656c6c6f', ADDR])).toEqual({
      address: ADDR,
      message: '0x48656c6c6f',
    });
    // Flipped order: [address, message]
    expect(parseSignRequest('personal_sign', [ADDR, '0x48656c6c6f'])).toEqual({
      address: ADDR,
      message: '0x48656c6c6f',
    });
    // eth_sign order: [address, message]
    expect(parseSignRequest('eth_sign', [ADDR, 'hi'])).toEqual({
      address: ADDR,
      message: 'hi',
    });
  });

  it('parses typed data params and stringifies object payloads', () => {
    const td = { domain: {}, types: {}, message: { a: 1 } };
    expect(parseSignRequest('eth_signTypedData_v4', [ADDR, td])).toEqual({
      address: ADDR,
      message: JSON.stringify(td),
    });
    expect(parseSignRequest('eth_signTypedData_v4', [ADDR, JSON.stringify(td)])).toEqual({
      address: ADDR,
      message: JSON.stringify(td),
    });
  });

  it('converts both hex and plain-text messages to signable bytes', () => {
    // Hex is decoded to its raw bytes ("Hello").
    expect(ethers.toUtf8String(toSignableBytes('0x48656c6c6f'))).toBe('Hello');
    // Non-hex text is UTF-8 encoded rather than throwing.
    const msg = 'Upload metadata for 0xabc at 1700000000';
    expect(ethers.toUtf8String(toSignableBytes(msg))).toBe(msg);
  });

  it('rejects RPC execution when the user denies confirmation', async () => {
    await expect(
      confirmDAppRequest('personal_sign', ['0x48656c6c6f', ADDR], {
        origin: 'https://example.com',
        activeAddress: ADDR,
        confirm: vi.fn().mockResolvedValue(false),
      }),
    ).rejects.toMatchObject({ code: 4001 });
  });
});
