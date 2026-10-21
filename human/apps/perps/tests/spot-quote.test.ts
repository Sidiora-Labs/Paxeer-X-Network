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

const level = (priceText: string) => ({ price: Number(priceText), priceText, size: 1 });
const book = { bids: [level('0.9')], asks: [level('1.1')] };

test('marketConvertsAtTheSideBestPriceNotTheMid', async () => {
  const { spotConversionPrice, spotOrderUnits } = await import('../src/widgets/spot/SpotOrderForm');
  const buy = spotConversionPrice({ orderType: 'market', side: 'BUY', limitPrice: '', book });
  assert.deepEqual(buy, { price: '1.1', reason: null });
  // 11 USD at the 1.1 ask buys 10 base; the 1.0 mid would have said 11.
  const bought = spotOrderUnits({ sizeMode: 'USD', amount: '11', price: buy.price, baseDecimals: 18, quoteDecimals: 6 });
  assert.equal(bought.baseUnits, 10_000_000_000_000_000_000n);
  const sell = spotConversionPrice({ orderType: 'market', side: 'SELL', limitPrice: '7', book });
  assert.deepEqual(sell, { price: '0.9', reason: null });
  // 10 base at the 0.9 bid is 9 USD; the 1.0 mid would have said 10.
  const sold = spotOrderUnits({ sizeMode: 'BASE', amount: '10', price: sell.price, baseDecimals: 18, quoteDecimals: 6 });
  assert.equal(sold.quoteUnits, 9_000_000n);
});

test('marketUsesTheBookPriceTextExactly', async () => {
  const { spotConversionPrice, spotOrderUnits } = await import('../src/widgets/spot/SpotOrderForm');
  const exact = { bids: [level('0.123456789123456789')], asks: [level('0.2')] };
  const sell = spotConversionPrice({ orderType: 'market', side: 'SELL', limitPrice: '', book: exact });
  assert.equal(sell.price, '0.123456789123456789');
  const units = spotOrderUnits({ sizeMode: 'BASE', amount: '1', price: sell.price, baseDecimals: 18, quoteDecimals: 18 });
  assert.equal(units.quoteUnits, 123_456_789_123_456_789n);
  const viaFloat = spotOrderUnits({ sizeMode: 'BASE', amount: '1', price: String(exact.bids[0].price), baseDecimals: 18, quoteDecimals: 18 });
  assert.equal(viaFloat.quoteUnits, 123_456_789_123_456_780n);
});

test('emptyBookDisablesMarketWithAReason', async () => {
  const { spotConversionPrice } = await import('../src/widgets/spot/SpotOrderForm');
  const noAsks = spotConversionPrice({ orderType: 'market', side: 'BUY', limitPrice: '', book: { bids: [level('1')], asks: [] } });
  assert.equal(noAsks.price, '');
  assert.match(noAsks.reason ?? '', /No asks/);
  const noBook = spotConversionPrice({ orderType: 'market', side: 'SELL', limitPrice: '', book: null });
  assert.equal(noBook.price, '');
  assert.match(noBook.reason ?? '', /No bids/);
  const zeroBid = spotConversionPrice({ orderType: 'market', side: 'SELL', limitPrice: '', book: { bids: [level('0')], asks: [] } });
  assert.notEqual(zeroBid.reason, null);
});

test('stopLimitConvertsAtItsOwnLimitPrice', async () => {
  const { spotConversionPrice, spotOrderUnits } = await import('../src/widgets/spot/SpotOrderForm');
  const far = { bids: [level('3')], asks: [level('4')] };
  const buy = spotConversionPrice({ orderType: 'stop-limit', side: 'BUY', limitPrice: ' 2.5 ', book: far });
  assert.deepEqual(buy, { price: '2.5', reason: null });
  // 10 USD at the 2.5 limit is 4 base (the 4 ask would say 2.5, the 3.5 mid 2.857...).
  assert.equal(spotOrderUnits({ sizeMode: 'USD', amount: '10', price: buy.price, baseDecimals: 18, quoteDecimals: 6 }).baseUnits, 4_000_000_000_000_000_000n);
  const sell = spotConversionPrice({ orderType: 'stop-limit', side: 'SELL', limitPrice: '2.5', book: far });
  assert.equal(spotOrderUnits({ sizeMode: 'BASE', amount: '2', price: sell.price, baseDecimals: 18, quoteDecimals: 6 }).quoteUnits, 5_000_000n);
  const blank = spotConversionPrice({ orderType: 'stop-limit', side: 'BUY', limitPrice: '', book: far });
  assert.deepEqual(blank, { price: '', reason: 'Enter a limit price' });
});

test('sellBalanceCheckIsBigint', async () => {
  const { spotSellExceedsBalance, spotOrderUnits } = await import('../src/widgets/spot/SpotOrderForm');
  const sell = (amount: string, baseDecimals: number) =>
    spotOrderUnits({ sizeMode: 'BASE', amount, price: '', baseDecimals, quoteDecimals: 6 }).baseUnits;
  const held = { raw: '1000000000000000001', decimals: 18 };
  assert.equal(spotSellExceedsBalance(sell('1.000000000000000002', 18), held, 18), true);
  assert.equal(1.000000000000000002 > 1.000000000000000001 + 1e-9, false);
  assert.equal(spotSellExceedsBalance(sell('1.000000000000000001', 18), held, 18), false);
  // Holding at 6 decimals, market at 18: 1.5 held is 1_500_000_000_000_000_000 base units.
  assert.equal(spotSellExceedsBalance(sell('1.5', 18), { raw: '1500000', decimals: 6 }, 18), false);
  assert.equal(spotSellExceedsBalance(sell('1.500000000000000001', 18), { raw: '1500000', decimals: 6 }, 18), true);
  // Holding at 18 decimals, market at 6: floored to 1_999_999 units.
  assert.equal(spotSellExceedsBalance(sell('2', 6), { raw: '1999999999999999999', decimals: 18 }, 6), true);
  assert.equal(spotSellExceedsBalance(sell('1.999999', 6), { raw: '1999999999999999999', decimals: 18 }, 6), false);
  assert.equal(spotSellExceedsBalance(1n, null, 18), true);
  assert.equal(spotSellExceedsBalance(0n, null, 18), false);
  assert.equal(spotSellExceedsBalance(1n, { raw: 'abc', decimals: 18 }, 18), true);
});
