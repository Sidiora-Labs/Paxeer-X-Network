import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  PaxscanError,
  _resetPaxscanCache,
  getWalletEquityUsd,
} from '../src/treasury/paxscan.js';

/**
 * Paxscan client — covers the equity computation that replaced the
 * USDL-only RPC valuation in the funded evaluator. Bugs here would silently
 * over- or under-value funded accounts and trigger wrong breaches/payouts,
 * so every branch (good response, partial response, errors, caching) needs
 * explicit coverage.
 */

const ADDR = '0x036176ec56b34f783165f2a184bf2e89dd653a51' as const;

// Real-shaped stand-ins from `curl https://api.paxscan.io/api/v2/...` against
// the funded test address. Numbers chosen so the expected USD totals are easy
// to reason about by hand.

function makeAddressInfo(coinBalanceWei: string, exchangeRate: string) {
  return {
    coin_balance: coinBalanceWei,
    exchange_rate: exchangeRate,
    hash: ADDR,
    is_contract: false,
  };
}

function makeTokenBalance(opts: {
  value: string;
  decimals: string;
  exchange_rate: string | null;
  symbol?: string;
}) {
  return {
    value: opts.value,
    token_id: null,
    token_instance: null,
    token: {
      address_hash: '0x86949e4cdb89496490890b67c9cff63ed8efb4b1',
      decimals: opts.decimals,
      exchange_rate: opts.exchange_rate,
      symbol: opts.symbol ?? 'TKN',
      type: 'ERC-20',
    },
  };
}

/**
 * Build a fetch mock that returns different bodies based on URL substring.
 * Lets a single test stub both the address and token-balances endpoints.
 */
function mockFetch(map: Record<string, unknown | { error: number }>) {
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    for (const key of Object.keys(map)) {
      if (url.includes(key)) {
        const body = map[key];
        if (body && typeof body === 'object' && 'error' in body) {
          return new Response('upstream error', { status: body.error as number });
        }
        return new Response(JSON.stringify(body), {
          status: 200,
          headers: { 'content-type': 'application/json' },
        });
      }
    }
    throw new Error(`unmocked fetch URL: ${url}`);
  });
}

beforeEach(() => {
  _resetPaxscanCache();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

// -----------------------------------------------------------------------------
// Happy path
// -----------------------------------------------------------------------------

describe('getWalletEquityUsd — equity math', () => {
  it('sums native PAX + ERC-20 holdings into a single USD value', async () => {
    // Use round-number stand-ins so the expected total is verifiable by hand.
    // The shape mirrors a real Paxscan response (decimals as string, rates as
    // string, value in raw token units).
    const fetchMock = mockFetch({
      [`/addresses/${ADDR}/token-balances`]: [
        // 1,000.000000 SID at $3.50 = $3,500.00
        makeTokenBalance({
          value: '1000000000', // 1000 × 10^6
          decimals: '6',
          exchange_rate: '3.50',
          symbol: 'SID',
        }),
        // 25,000.000000 USDL at $1.00 = $25,000.00
        makeTokenBalance({
          value: '25000000000', // 25_000 × 10^6
          decimals: '6',
          exchange_rate: '1.00',
          symbol: 'USDL',
        }),
      ],
      [`/addresses/${ADDR}`]: makeAddressInfo(
        '15000000000000000000', // 15 PAX
        '12.25', //  × $12.25 = $183.75
      ),
    });
    vi.stubGlobal('fetch', fetchMock);

    const equity = await getWalletEquityUsd(ADDR);
    // 3,500.00 + 25,000.00 + 183.75 = 28,683.75
    expect(equity).toBeCloseTo(28_683.75, 2);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('treats a wallet with $0 native + no tokens as $0 equity', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetch({
        [`/addresses/${ADDR}/token-balances`]: [],
        [`/addresses/${ADDR}`]: makeAddressInfo('0', '12.25'),
      }),
    );
    expect(await getWalletEquityUsd(ADDR)).toBe(0);
  });
});

// -----------------------------------------------------------------------------
// Defensive parsing — Paxscan can return unpriced or malformed entries.
// We must never let those poison the equity sum (e.g. via NaN propagation).
// -----------------------------------------------------------------------------

describe('getWalletEquityUsd — defensive parsing', () => {
  it('skips tokens with null exchange_rate (unpriced / scam-flagged)', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetch({
        [`/addresses/${ADDR}/token-balances`]: [
          makeTokenBalance({
            value: '1000000000000000000000',
            decimals: '18',
            exchange_rate: null,
            symbol: 'NOPRICE',
          }),
          makeTokenBalance({
            value: '1000000',
            decimals: '6',
            exchange_rate: '1.00000000',
            symbol: 'USDC',
          }),
        ],
        [`/addresses/${ADDR}`]: makeAddressInfo('0', '0'),
      }),
    );
    // Only USDC contributes: 1.00 USDC × $1 = $1.00
    expect(await getWalletEquityUsd(ADDR)).toBeCloseTo(1.0, 6);
  });

  it('skips tokens with zero/non-numeric exchange_rate', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetch({
        [`/addresses/${ADDR}/token-balances`]: [
          makeTokenBalance({
            value: '1000000000',
            decimals: '6',
            exchange_rate: '0',
            symbol: 'ZERO',
          }),
          makeTokenBalance({
            value: '1000000000',
            decimals: '6',
            exchange_rate: 'not-a-number',
            symbol: 'GARBAGE',
          }),
        ],
        [`/addresses/${ADDR}`]: makeAddressInfo('0', '0'),
      }),
    );
    expect(await getWalletEquityUsd(ADDR)).toBe(0);
  });

  it('skips entries whose token field is null (NFT batch placeholders, etc.)', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetch({
        [`/addresses/${ADDR}/token-balances`]: [
          { value: '1', token_id: '5', token_instance: null, token: null },
        ],
        [`/addresses/${ADDR}`]: makeAddressInfo('0', '0'),
      }),
    );
    expect(await getWalletEquityUsd(ADDR)).toBe(0);
  });

  it('treats NFT-style entries (null exchange_rate) as $0', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetch({
        [`/addresses/${ADDR}/token-balances`]: [
          {
            value: '1',
            token_id: '42',
            token_instance: null,
            token: {
              address_hash: '0x' + '0'.repeat(40),
              decimals: null,
              exchange_rate: null,
              symbol: 'NFT',
              type: 'ERC-721',
            },
          },
        ],
        [`/addresses/${ADDR}`]: makeAddressInfo('0', '0'),
      }),
    );
    expect(await getWalletEquityUsd(ADDR)).toBe(0);
  });
});

// -----------------------------------------------------------------------------
// Failure modes — must throw, never silently return $0.
// Returning 0 on error would let a Paxscan outage breach every funded account
// in the system within one tick. The evaluator catches the throw and skips.
// -----------------------------------------------------------------------------

describe('getWalletEquityUsd — error handling', () => {
  it('throws PaxscanError on a 500-level upstream error', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetch({
        [`/addresses/${ADDR}/token-balances`]: { error: 503 },
        [`/addresses/${ADDR}`]: makeAddressInfo('0', '0'),
      }),
    );
    await expect(getWalletEquityUsd(ADDR)).rejects.toBeInstanceOf(PaxscanError);
  });

  it('throws PaxscanError on a schema-shape mismatch', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetch({
        // token-balances must be an array; an object should fail the schema
        [`/addresses/${ADDR}/token-balances`]: { unexpected: true },
        [`/addresses/${ADDR}`]: makeAddressInfo('0', '0'),
      }),
    );
    await expect(getWalletEquityUsd(ADDR)).rejects.toBeInstanceOf(PaxscanError);
  });

  it('throws PaxscanError when fetch throws (network/DNS failure)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        throw new Error('ENETUNREACH');
      }),
    );
    await expect(getWalletEquityUsd(ADDR)).rejects.toBeInstanceOf(PaxscanError);
  });
});

// -----------------------------------------------------------------------------
// Caching + concurrency — one HTTP per (wallet, TTL window) under load.
// -----------------------------------------------------------------------------

describe('getWalletEquityUsd — cache + coalescing', () => {
  it('serves cached results within TTL without re-fetching', async () => {
    const fetchMock = mockFetch({
      [`/addresses/${ADDR}/token-balances`]: [],
      [`/addresses/${ADDR}`]: makeAddressInfo('1000000000000000000', '10'),
    });
    vi.stubGlobal('fetch', fetchMock);

    const a = await getWalletEquityUsd(ADDR);
    const b = await getWalletEquityUsd(ADDR);
    expect(a).toBe(b);
    // 2 calls for the first lookup (one per endpoint), 0 for the second.
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('coalesces concurrent first-misses into a single round-trip pair', async () => {
    const fetchMock = mockFetch({
      [`/addresses/${ADDR}/token-balances`]: [],
      [`/addresses/${ADDR}`]: makeAddressInfo('1000000000000000000', '10'),
    });
    vi.stubGlobal('fetch', fetchMock);

    const [a, b, c] = await Promise.all([
      getWalletEquityUsd(ADDR),
      getWalletEquityUsd(ADDR),
      getWalletEquityUsd(ADDR),
    ]);
    expect(a).toBe(b);
    expect(b).toBe(c);
    // Still just 2 calls (one per endpoint), not 6.
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
