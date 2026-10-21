import { describe, expect, it } from 'vitest';
import { routeForPath } from '@/core/app-foundation/navigation';
import { PoolNotFoundError, type PoolSnapshot } from '@/core/market/pool-resolver';
import { parseTerminalSlug, terminalPoolPath, terminalViewForPath, terminalViewPath } from './sub-nav';
import { pastedPoolAddress } from './frame';
import { coinTarget, heldCoinTarget } from './coin-view';

const pool = '0x' + 'ab'.repeat(20);
const mixed = '0x' + 'Ab'.repeat(20);

describe('routeForPath terminal cases', () => {
  it('accepts /terminal, the five named views and a pool address', () => {
    for (const path of ['/terminal', '/terminal/trending', '/terminal/portfolio', '/terminal/track', '/terminal/rewards', '/terminal/trenches', `/terminal/${pool}`, `/terminal/${mixed}`]) {
      expect(routeForPath(path)?.id, path).toBe('terminal');
    }
  });
  it('rejects unknown views, short addresses and deeper paths', () => {
    for (const path of ['/terminal/coin', '/terminal/0x1234', `/terminal/${pool}/chart`, '/terminal/', `/terminal/${pool}x`, '/terminal/Portfolio']) {
      expect(routeForPath(path), path).toBeUndefined();
    }
  });
});

describe('terminal paths and slugs', () => {
  it('builds view and pool paths', () => {
    expect(terminalViewPath('trending')).toBe('/terminal');
    expect(terminalViewPath('trenches')).toBe('/terminal/trenches');
    expect(terminalPoolPath(mixed)).toBe(`/terminal/${pool}`);
    expect(() => terminalPoolPath('0x12')).toThrow('Invalid address');
  });
  it('parses the slug the app route receives', () => {
    expect(parseTerminalSlug(undefined)).toEqual({ kind: 'view', view: 'trending' });
    expect(parseTerminalSlug('track')).toEqual({ kind: 'view', view: 'track' });
    expect(parseTerminalSlug(mixed)).toEqual({ kind: 'pool', pool });
    expect(parseTerminalSlug('trending')).toEqual({ kind: 'view', view: 'trending' });
    expect(parseTerminalSlug('0xzz')).toEqual({ kind: 'invalid' });
  });
  it('maps a pathname to the active view', () => {
    expect(terminalViewForPath('/terminal')).toBe('trending');
    expect(terminalViewForPath('/terminal/rewards')).toBe('rewards');
    expect(terminalViewForPath('/terminal/trending')).toBe('trending');
    expect(terminalViewForPath(`/terminal/${pool}`)).toBe('coin');
    expect(terminalViewForPath('/explore')).toBeNull();
    expect(terminalViewForPath(null)).toBeNull();
  });
});

describe('Paste CA address extraction', () => {
  it('takes a bare address or one inside a URL, lowercased', () => {
    expect(pastedPoolAddress(`  ${mixed}\n`)).toBe(pool);
    expect(pastedPoolAddress(`https://paxscan.paxeer.app/address/${mixed}`)).toBe(pool);
    expect(pastedPoolAddress(`https://example.org/coin/${pool}?x=1`)).toBe(pool);
  });
  it('refuses short addresses and longer hex such as transaction hashes', () => {
    expect(pastedPoolAddress('0x1234')).toBeNull();
    expect(pastedPoolAddress('0x' + 'a'.repeat(64))).toBeNull();
    expect(pastedPoolAddress('hello')).toBeNull();
  });
});

function snapshotFor(poolAddress: string): PoolSnapshot {
  return {
    pool: { chainId: '125', poolAddress, tokenAddress: '0x' + 'cd'.repeat(20) },
    token: null, metadata: null, stats: null, creator: null, createdAt: null,
    generatedAt: '2026-10-13T00:00:00Z', resolvedBy: 'detail',
    unavailable: { stats: null, metadata: null, registry: null },
  };
}

describe('coin view target', () => {
  it('waits for the resolver, renders a pool address and redirects a token address to its pool', () => {
    expect(coinTarget(pool, { status: 'pending' })).toEqual({ kind: 'pending' });
    expect(coinTarget(pool, { status: 'success', data: snapshotFor(pool) })).toEqual({ kind: 'render', pool });
    const token = '0x' + 'cd'.repeat(20);
    expect(coinTarget(token, { status: 'success', data: snapshotFor(mixed) })).toEqual({ kind: 'redirect', pool });
  });
  it('reports not-found only for the resolver PoolNotFoundError', () => {
    expect(coinTarget(pool, { status: 'error', error: new PoolNotFoundError(pool) })).toEqual({ kind: 'not-found' });
    expect(coinTarget(pool, { status: 'error', error: new Error('network down') })).toEqual({ kind: 'render', pool });
  });
  it('keeps a settled target while a refetch of a failed snapshot reports pending again', () => {
    const rendered = coinTarget(pool, { status: 'error', error: new Error('network down') });
    const refetching = coinTarget(pool, { status: 'pending' });
    expect(heldCoinTarget(rendered, refetching)).toEqual({ kind: 'render', pool });
    expect(heldCoinTarget({ kind: 'pending' }, refetching)).toEqual({ kind: 'pending' });
    expect(heldCoinTarget(rendered, { kind: 'not-found' })).toEqual({ kind: 'not-found' });
  });
});
