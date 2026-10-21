import { test } from 'node:test';
import assert from 'node:assert/strict';
import { existsSync, statSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { MARKET_LOGO_PLACEHOLDER, MARKET_LOGO_SYMBOLS, coinLogoUrl } from '../src/core/perps/config';

const PUBLIC = fileURLToPath(new URL('../public', import.meta.url));

function assertOnDisk(url: string, label: string) {
  const file = path.join(PUBLIC, url);
  assert.ok(existsSync(file) && statSync(file).size > 0, `${label}: ${url} missing`);
}

test('every known symbol maps to a committed logo file', () => {
  assert.ok(MARKET_LOGO_SYMBOLS.size > 0);
  for (const symbol of MARKET_LOGO_SYMBOLS) {
    const url = coinLogoUrl(symbol.toUpperCase());
    assert.notEqual(url, MARKET_LOGO_PLACEHOLDER, symbol);
    assertOnDisk(url, symbol);
  }
});

test('an unknown symbol maps to the committed placeholder', () => {
  assert.equal(coinLogoUrl('NOPE'), MARKET_LOGO_PLACEHOLDER);
  assert.equal(coinLogoUrl(''), MARKET_LOGO_PLACEHOLDER);
  assert.equal(coinLogoUrl('constructor'), MARKET_LOGO_PLACEHOLDER);
  assertOnDisk(MARKET_LOGO_PLACEHOLDER, 'placeholder');
});
