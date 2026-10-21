import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { afterAll, describe, expect, it } from 'vitest';
import type { ExplorerTokenTransfer } from '@/core/clients/explorer-api';
import { CORE_DEPLOYMENT } from '@/core/network/deployment';

const WALLET = '0x1111111111111111111111111111111111111111';
const POOL = '0x2222222222222222222222222222222222222222';
const TOKEN = '0x3333333333333333333333333333333333333333';
const OTHER = '0x4444444444444444444444444444444444444444';
const STRANGER = '0x5555555555555555555555555555555555555555';
const DOWN = '0x6666666666666666666666666666666666666666';
const USDL = CORE_DEPLOYMENT.usdl.toLowerCase();
const tx = (n: number) => `0x${n.toString(16).padStart(64, '0')}`;

function transfer(seconds: number, from: string, to: string, token: string, opts: { hash?: string; log?: number; decimals?: string | null; rate?: string | null; name?: string | null } = {}): ExplorerTokenTransfer {
  return {
    block_number: seconds,
    log_index: opts.log ?? 0,
    from: { hash: from },
    to: { hash: to },
    timestamp: new Date(seconds * 1000).toISOString(),
    token: { address: token, decimals: opts.decimals === undefined ? '18' : opts.decimals, exchange_rate: opts.rate ?? null, icon_url: null, name: opts.name === undefined ? null : opts.name, symbol: null, type: 'ERC-20' },
    total: { decimals: opts.decimals === undefined ? '18' : opts.decimals, value: '1000000000000000000000' },
    tx_hash: opts.hash ?? tx(seconds + 1_000_000),
    type: 'token_transfer',
  };
}

/** Current Blockscout row shape (transaction_hash, token.address_hash) as Paxscan answers it. */
function blockscoutRow(row: ExplorerTokenTransfer) {
  const { tx_hash: transactionHash, token, ...rest } = row;
  const { address, ...tokenRest } = token;
  return { ...rest, transaction_hash: transactionHash, token: { ...tokenRest, address_hash: address } };
}

const swap = tx(42);
const firstPage = [
  transfer(300, OTHER, WALLET, OTHER, { log: 0, rate: '0.5' }),
  transfer(200, POOL, WALLET, TOKEN, { hash: swap, log: 1 }),
  transfer(200, WALLET, POOL, USDL, { hash: swap, log: 2, decimals: '6' }),
];
const secondPage = [transfer(100, WALLET, STRANGER, TOKEN, { log: 5 })];

const paths: string[] = [];
const explorer: Server = createServer((request, response) => {
  const url = request.url ?? '';
  paths.push(url);
  response.setHeader('Content-Type', 'application/json');
  if (url.startsWith(`/addresses/${DOWN}/token-transfers`)) { response.statusCode = 503; response.end(JSON.stringify({ message: 'upstream unavailable' })); return; }
  if (url === `/addresses/${WALLET}/token-transfers?type=ERC-20`) {
    response.end(JSON.stringify({ items: firstPage.map(blockscoutRow), next_page_params: { block_number: 200, index: 2 } }));
    return;
  }
  if (url.startsWith(`/addresses/${WALLET}/token-transfers?`) && url.includes('block_number=200')) {
    response.end(JSON.stringify({ items: secondPage.map(blockscoutRow), next_page_params: null }));
    return;
  }
  response.statusCode = 404;
  response.end(JSON.stringify({ message: 'Not found' }));
});
const origin = await new Promise<string>(resolve => explorer.listen(0, '127.0.0.1', () => resolve(`http://127.0.0.1:${(explorer.address() as AddressInfo).port}`)));
process.env.NEXT_PUBLIC_EXPLORER_API = origin;
const { activityValueText, signedTokenAmount } = await import('./activity-row');
const { ActivityUnavailableError, activityKinds, fetchActivityPage, filterActivity, firstActivityCursor, mergeActivity, tradesUnavailableNotice, transferToEvent } = await import('./activity-transport');

afterAll(() => { explorer.close(); });

describe('activity mapping', () => {
  it('maps explorer transfers to send and receive by direction, with an explorer-rate value only when known', () => {
    const received = transferToEvent(transfer(10, OTHER, WALLET, OTHER, { rate: '0.5' }), WALLET, 0);
    const sent = transferToEvent(transfer(11, WALLET, OTHER, OTHER), WALLET, 1);
    expect(received?.kind).toBe('receive');
    expect(received?.amountRaw).toBe(1_000_000_000_000_000_000_000n);
    expect(received?.value).toEqual({ unit: 'usd', value: 500 });
    expect(sent?.kind).toBe('send');
    expect(sent?.amountRaw).toBe(-1_000_000_000_000_000_000_000n);
    expect(sent?.value).toBeNull();
    expect(transferToEvent(transfer(12, OTHER, STRANGER, OTHER), WALLET, 2)).toBeNull();
  });

  it('values USDL transfers exactly in USDL and refuses a USDL row with the wrong scale', () => {
    expect(transferToEvent(transfer(13, WALLET, POOL, USDL, { decimals: '6' }), WALLET, 0)?.value).toEqual({ unit: 'usdl', raw: 1_000_000_000_000_000_000_000n });
    expect(transferToEvent(transfer(13, WALLET, POOL, USDL, { decimals: '18' }), WALLET, 0)).toBeNull();
  });

  it('formats the signed amount, raw units when decimals are unknown, and an em dash for unknown value', () => {
    expect(signedTokenAmount(-4_000_000_000_000_000_000_000_000n, 18, 'SID')).toBe('-4M SID');
    expect(signedTokenAmount(1_000n, null, null)).toBe('+1000 raw');
    expect(activityValueText(null)).toBe('—');
    expect(activityValueText({ unit: 'usdl', raw: 1_840_000_000n })).toBe('1.84K USDL');
  });

  it('offers only the transfer actions and states why buys and sells are absent', () => {
    expect(activityKinds).toEqual(['send', 'receive']);
    expect(tradesUnavailableNotice).toMatch(/Buys and sells don't show here yet/);
  });
});

describe('activity ledger against a Blockscout-shaped explorer origin', () => {
  it('reads the first page, keeps both legs of a swap as transfers, and follows the keyset cursor', async () => {
    const first = await fetchActivityPage(WALLET, firstActivityCursor);
    const ledger = mergeActivity([first], WALLET);
    expect(ledger.events.map(event => event.kind)).toEqual(['receive', 'receive', 'send']);
    expect(ledger.events.filter(event => event.txHash === swap).map(event => event.tokenAddress).sort()).toEqual([TOKEN, USDL].sort());
    expect(ledger.hasMore).toBe(true);
    expect(ledger.next).toEqual({ block_number: '200', index: '2' });

    const second = await fetchActivityPage(WALLET, ledger.next ?? firstActivityCursor);
    const all = mergeActivity([first, second], WALLET);
    expect(all.hasMore).toBe(false);
    expect(all.next).toBeUndefined();
    expect(all.events.map(event => event.occurredAt)).toEqual([300_000, 200_000, 200_000, 100_000]);
    expect(mergeActivity([first, first], WALLET).events).toHaveLength(3);
    expect(paths.every(path => path.startsWith(`/addresses/${WALLET}/token-transfers?type=ERC-20`))).toBe(true);
  });

  it('turns a first-page 503 into an unavailable marker with the real status', async () => {
    const failure = await fetchActivityPage(DOWN, firstActivityCursor).catch((error: unknown) => error);
    expect(failure).toBeInstanceOf(ActivityUnavailableError);
    expect((failure as InstanceType<typeof ActivityUnavailableError>).marker).toMatchObject({ state: 'unsupported', status: 503 });
  });
});

describe('activity filters', () => {
  it('narrows by action and by name, symbol, address or transaction hash', () => {
    const events = [transferToEvent(transfer(100, POOL, WALLET, TOKEN), WALLET, 0), transferToEvent(transfer(90, WALLET, POOL, TOKEN, { log: 1 }), WALLET, 1), transferToEvent(transfer(80, OTHER, WALLET, OTHER), WALLET, 2)].filter(e => e !== null);
    const identity = (event: { tokenAddress: string }) => (event.tokenAddress === TOKEN ? { name: 'Sidiora Markets', symbol: 'SID' } : { name: null, symbol: null });
    expect(filterActivity(events, 'send', '', identity).map(e => e.kind)).toEqual(['send']);
    expect(filterActivity(events, 'all', 'sid', identity)).toHaveLength(2);
    expect(filterActivity(events, 'all', OTHER.slice(0, 10), identity).map(e => e.kind)).toEqual(['receive']);
    expect(filterActivity(events, 'send', 'markets', identity)).toHaveLength(1);
  });
});
