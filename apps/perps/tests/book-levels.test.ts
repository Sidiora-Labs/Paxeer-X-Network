import { test } from 'node:test';
import assert from 'node:assert/strict';

process.env.NEXT_PUBLIC_PERPS_GATEWAY_URL ??= 'https://perps-gateway.invalid';

const load = () => import('../src/core/perps/bookLevels');

test('zeroSizeLevelIgnored', async () => {
  const { normalizeLevels } = await load();
  const asks = normalizeLevels([['100', '0'], ['101', '0.000'], ['102', '2']], 'asks');
  assert.deepEqual(asks.map(l => l.priceText), ['102']);
  const bids = normalizeLevels([{ price: '99', size: '0' }, { price: '98', quantity: '3' }], 'bids');
  assert.deepEqual(bids.map(l => l.priceText), ['98']);
});

test('zeroPriceLevelIgnored', async () => {
  const { normalizeLevels } = await load();
  const asks = normalizeLevels([['0', '5'], ['0.0', '1'], ['100.5', '1']], 'asks');
  assert.equal(asks.length, 1);
  assert.equal(asks[0].priceText, '100.5');
  assert.equal(asks[0].price, 100.5);
  assert.equal(asks[0].size, 1);
});

test('unsortedBookYieldsRightBest', async () => {
  const { normalizeLevels } = await load();
  const raw = [['100.10', '1'], ['100.0999999999999999999', '1'], ['99', '1'], ['100.1', '2'], [1e-7, '1']];
  const asks = normalizeLevels(raw, 'asks');
  assert.deepEqual(asks.map(l => l.priceText), ['1e-7', '99', '100.0999999999999999999', '100.10', '100.1']);
  const bids = normalizeLevels(raw, 'bids');
  assert.equal(bids[0].priceText, '100.10');
  assert.equal(bids[2].priceText, '100.0999999999999999999');
  assert.equal(bids[bids.length - 1].priceText, '1e-7');
});

test('malformedPriceTextDropped', async () => {
  const { normalizeLevels } = await load();
  const raw = [['abc', '1'], ['-5', '1'], ['1,000', '1'], ['', '1'], [null, '1'], ['Infinity', '1'], ['NaN', '1'],
    ['1e400', '1'], ['0x10', '1'], ['.', '1'], ['7', 'x'], ['7', '-1'], ['8', '1']];
  assert.deepEqual(normalizeLevels(raw, 'asks').map(l => l.priceText), ['8']);
  assert.deepEqual(normalizeLevels('not-a-book', 'bids'), []);
});

test('sidPaxStreamUsesSharedLevels', async () => {
  const { readFileSync } = await import('node:fs');
  const src = readFileSync(new URL('../src/hooks/perps/useSidPaxMarketStream.ts', import.meta.url), 'utf8');
  assert.match(src, /import \{ normalizeLevels \} from "@\/core\/perps\/bookLevels";/);
  assert.doesNotMatch(src, /function normalizeLevels\b/);
  assert.match(src, /normalizeLevels\(msg\.data\.asks, "asks"\)/);
  assert.match(src, /normalizeLevels\(msg\.data\.bids, "bids"\)/);
});

test('venueBookResyncUsesSharedLevels', async () => {
  const { parseVenueBook } = await import('../src/core/net/venueStream');
  const book = parseVenueBook({
    asks: [{ price: '1.0', quantity: '0' }, { price: '1.01', quantity: '5' }, { price: '1.0000000000000000001', quantity: '1' }],
    bids: [['0.99', '0'], ['0.98', '2'], ['0.9899999999999999999', '1']],
  }, 1);
  assert.ok(book);
  assert.deepEqual(book.asks.map(l => [l.priceText, l.size]), [['1.0000000000000000001', 1], ['1.01', 5]]);
  assert.deepEqual(book.bids.map(l => [l.priceText, l.size]), [['0.9899999999999999999', 1], ['0.98', 2]]);
  const { readFileSync } = await import('node:fs');
  const src = readFileSync(new URL('../src/core/net/venueStream.ts', import.meta.url), 'utf8');
  assert.match(src, /import \{ normalizeLevels, type SpotBookLevel \} from "@\/core\/perps\/bookLevels";/);
  assert.doesNotMatch(src, /function parseLevels\b/);
  const spot = readFileSync(new URL('../src/hooks/spot/useSpotMarketStream.ts', import.meta.url), 'utf8');
  assert.doesNotMatch(spot, /function normalizeLevels\b/);
});
