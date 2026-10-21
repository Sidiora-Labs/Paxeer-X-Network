import { describe, expect, it } from 'vitest';
import {
  validateEvmAddress,
  validateSlippageBps,
  validateSpendableBalance,
  validateSwapQuote,
  validateTokenAmount,
} from './txValidation';

describe('transaction validation', () => {
  it('normalizes valid EVM addresses and rejects invalid addresses', () => {
    expect(validateEvmAddress('0xf8850b62AE017c55be7f571BBad840b4f3DA7D49')).toBe(
      '0xf8850b62AE017c55be7f571BBad840b4f3DA7D49',
    );
    expect(() => validateEvmAddress('not-an-address')).toThrow('valid recipient');
  });

  it('parses positive decimal token amounts', () => {
    expect(validateTokenAmount('1.5', 6)).toBe(BigInt(1_500_000));
    expect(() => validateTokenAmount('0', 18)).toThrow('greater than zero');
    expect(() => validateTokenAmount('1.123', 2)).toThrow('up to 2 decimals');
  });

  it('rejects spends that exceed loaded balance', () => {
    expect(() => validateSpendableBalance(BigInt(10), '9')).toThrow('Insufficient balance');
    expect(() => validateSpendableBalance(BigInt(10), null)).toThrow('still loading');
    expect(() => validateSpendableBalance(BigInt(10), '10')).not.toThrow();
  });

  it('enforces bounded swap slippage and viable quotes', () => {
    expect(() => validateSlippageBps(501)).toThrow('cannot exceed 5%');
    expect(() =>
      validateSwapQuote({
        amountIn: '100',
        amountOut: '0',
        sufficient: true,
        priceImpact: 0.2,
      }),
    ).toThrow('No output liquidity');
  });
});
