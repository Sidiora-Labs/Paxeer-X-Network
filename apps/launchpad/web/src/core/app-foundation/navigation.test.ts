import { describe, it, expect } from 'vitest';
import { routeForPath, routes, isRouteActive, poolPath, safeExternalDestination } from './navigation';

const addr = '0x1234567890abcdef1234567890abcdef12345678';

describe('routeForPath', () => {
  it('keeps the existing routes', () => {
    expect(routeForPath('/')?.id).toBe('discovery');
    expect(routeForPath('/new')?.id).toBe('new');
    expect(routeForPath(`/token/${addr}`)?.id).toBe('token');
    expect(routeForPath(`/profile/${addr}`)?.id).toBe('profile');
    expect(routeForPath(`/tokens/${addr}/metadata`)?.id).toBe('token-metadata');
    expect(routeForPath('/tokens')).toBeUndefined();
    expect(routeForPath('/docs/terms')?.id).toBe('docs');
    expect(routeForPath('/docs/nope')).toBeUndefined();
    expect(routeForPath('/meta-ag/swap')?.id).toBe('swap');
    expect(routeForPath('/chat/abc')?.id).toBe('chat');
  });

  it('registers the remodel routes', () => {
    expect(routeForPath('/explore')?.id).toBe('explore');
    expect(routeForPath('/coin')?.id).toBe('coin');
    expect(routeForPath(`/coin/${addr}`)?.id).toBe('coin');
    expect(routeForPath('/coin/not-an-address')).toBeUndefined();
    expect(routeForPath('/leaderboard')?.id).toBe('leaderboard');
    expect(routeForPath('/holder-rewards')?.id).toBe('holder-rewards');
    expect(routeForPath('/bounties')?.id).toBe('bounties');
    expect(routeForPath('/bounties/new')?.id).toBe('bounties');
    expect(routeForPath('/bounties/bounty_01-A')?.id).toBe('bounties');
    expect(routeForPath('/bounties/has space')).toBeUndefined();
    expect(routeForPath('/bounties/' + 'a'.repeat(129))).toBeUndefined();
    expect(routeForPath('/live/all')?.id).toBe('live-all');
    expect(routeForPath('/live/other')).toBeUndefined();
    expect(routeForPath('/watchlist')?.id).toBe('watchlist');
    expect(routeForPath('/design')).toBeUndefined();
  });

  it('rejects malformed paths', () => {
    expect(routeForPath('explore')).toBeUndefined();
    expect(routeForPath('/explore//x')).toBeUndefined();
    expect(routeForPath('/explore?x=1')).toBeUndefined();
    expect(routeForPath('/coin/' + addr + '/extra')).toBeUndefined();
  });

  it('has unique route ids and pathnames', () => {
    expect(new Set(routes.map(r => r.id)).size).toBe(routes.length);
    expect(new Set(routes.map(r => r.pathname)).size).toBe(routes.length);
  });
});

describe('isRouteActive', () => {
  it('matches exact and nested paths but never the root for children', () => {
    expect(isRouteActive('/live/all', '/live')).toBe(true);
    expect(isRouteActive('/explore', '/')).toBe(false);
    expect(isRouteActive('/', '/')).toBe(true);
  });
});

describe('poolPath', () => {
  it('routes pools to the coin page with a lowercased address', () => {
    expect(poolPath(125, '0x1234567890ABCDEF1234567890ABCDEF12345678')).toBe(`/coin/${addr}`);
  });

  it('rejects other chains and malformed addresses', () => {
    expect(() => poolPath(1, addr)).toThrow('Unsupported chain');
    expect(() => poolPath(125, '0x1234')).toThrow('Invalid address');
  });
});

describe('safeExternalDestination', () => {
  it('allows path-only x.com and t.me links', () => {
    expect(safeExternalDestination('https://x.com/sidiora')).toBe('https://x.com/sidiora');
    expect(safeExternalDestination('https://x.com/search/live')).toBe('https://x.com/search/live');
    expect(safeExternalDestination('https://t.me/Sidiorafunsupport')).toBe('https://t.me/Sidiorafunsupport');
    expect(safeExternalDestination('https://t.me/joinchat/abc_DEF-123')).toBe('https://t.me/joinchat/abc_DEF-123');
    expect(safeExternalDestination('https://t.me/+AbC123')).toBe('https://t.me/+AbC123');
  });

  it('rejects queries, fragments, other schemes and other hosts', () => {
    expect(safeExternalDestination('https://x.com/search?q=sidiora')).toBeNull();
    expect(safeExternalDestination('https://x.com/sidiora#top')).toBeNull();
    expect(safeExternalDestination('http://x.com/sidiora')).toBeNull();
    expect(safeExternalDestination('https://x.com:8443/sidiora')).toBeNull();
    expect(safeExternalDestination('https://user@x.com/sidiora')).toBeNull();
    expect(safeExternalDestination('https://twitter.com/sidiora')).toBeNull();
    expect(safeExternalDestination('https://x.com/')).toBeNull();
    expect(safeExternalDestination('https://evil.example/x.com')).toBeNull();
  });

  it('keeps the explorer allowlist', () => {
    expect(safeExternalDestination(`https://paxscan.paxeer.app/address/${addr}`)).toBe(`https://paxscan.paxeer.app/address/${addr}`);
    expect(safeExternalDestination('https://paxscan.paxeer.app/token/abc')).toBeNull();
  });
});
