import { test } from 'node:test';
import assert from 'node:assert/strict';

process.env.NEXT_PUBLIC_PERPS_GATEWAY_URL ??= 'https://perps-gateway.invalid';

// Rounding rule: the converted side is floored (truncated toward zero) at its
// token's decimals, so a buy never spends more than the entered size implies.

test('baseTimesPriceIsExact', async () => {
  const { spotOrderUnits } = await import('../src/widgets/spot/SpotOrderForm');
  const { parseTokenAmount } = await import('../src/core/spot/slippage');
  assert.equal(parseTokenAmount((1.15 * 100).toString(), 6), 114999999n);
  const units = spotOrderUnits({ sizeMode: 'BASE', amount: '1.15', price: '100', baseDecimals: 18, quoteDecimals: 6 });
  assert.equal(units.quoteUnits, 115_000_000n);
  assert.equal(units.baseUnits, 1_150_000_000_000_000_000n);
});

test('quoteIsFlooredForBuys', async () => {
  const { spotOrderUnits } = await import('../src/widgets/spot/SpotOrderForm');
  const units = spotOrderUnits({ sizeMode: 'BASE', amount: '1.0000001', price: '0.999999', baseDecimals: 18, quoteDecimals: 6 });
  assert.equal(units.quoteUnits, 999_999n);
  const usd = spotOrderUnits({ sizeMode: 'USD', amount: '100', price: '3', baseDecimals: 18, quoteDecimals: 6 });
  assert.equal(usd.quoteUnits, 100_000_000n);
  assert.equal(usd.baseUnits, 33_333_333_333_333_333_333n);
});

test('exponentNotationIsParsedExactly', async () => {
  const { spotOrderUnits } = await import('../src/widgets/spot/SpotOrderForm');
  assert.equal(String(1e-7), '1e-7');
  assert.equal(String(1e21), '1e+21');
  const tiny = spotOrderUnits({ sizeMode: 'BASE', amount: String(1e-7), price: '10000000', baseDecimals: 18, quoteDecimals: 6 });
  assert.equal(tiny.baseUnits, 100_000_000_000n);
  assert.equal(tiny.quoteUnits, 1_000_000n);
  const huge = spotOrderUnits({ sizeMode: 'BASE', amount: String(1e21), price: '2', baseDecimals: 18, quoteDecimals: 6 });
  assert.equal(huge.quoteUnits, 2_000_000_000_000_000_000_000_000_000n);
  const tinyPrice = spotOrderUnits({ sizeMode: 'USD', amount: '1', price: String(2.5e-7), baseDecimals: 6, quoteDecimals: 6 });
  assert.equal(tinyPrice.baseUnits, 4_000_000_000_000n);
});

test('noPriceKeepsInputLiteral', async () => {
  const { spotOrderUnits } = await import('../src/widgets/spot/SpotOrderForm');
  assert.deepEqual(spotOrderUnits({ sizeMode: 'BASE', amount: '2', price: '', baseDecimals: 6, quoteDecimals: 6 }), { baseUnits: 2_000_000n, quoteUnits: 0n });
  assert.deepEqual(spotOrderUnits({ sizeMode: 'USD', amount: '2', price: '', baseDecimals: 6, quoteDecimals: 6 }), { baseUnits: 0n, quoteUnits: 2_000_000n });
  assert.throws(() => spotOrderUnits({ sizeMode: 'BASE', amount: 'abc', price: '1', baseDecimals: 6, quoteDecimals: 6 }));
});

test('plainDecimalRewritesScientificNotation', async () => {
  const { toPlainDecimal, parseWei } = await import('../src/ui/utils/format');
  assert.equal(toPlainDecimal('1000e+20'), '100000000000000000000000');
  assert.equal(toPlainDecimal('-5e-3'), '-0.005');
  assert.equal(toPlainDecimal('1.23E1'), '12.3');
  assert.equal(toPlainDecimal('.5'), '0.5');
  assert.equal(toPlainDecimal('-0'), '0');
  assert.equal(toPlainDecimal('00012.3400'), '12.34');
  for (const bad of ['', 'e5', '1e', '+', '1.2.3', '1e99999']) assert.throws(() => toPlainDecimal(bad), bad);
  assert.equal(parseWei('-5e-3'), 0n);
  assert.equal(parseWei('1.5e18'), 1_500_000_000_000_000_000n);
});
