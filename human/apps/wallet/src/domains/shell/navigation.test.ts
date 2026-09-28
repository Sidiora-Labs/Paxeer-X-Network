import { describe, expect, it } from 'vitest';
import {
  parseRouteUrl,
  routeGuard,
  ROUTE_POLICIES,
  serializeRoute,
  SHELL_ROUTE_NAMES,
  type ShellRoute,
} from '.';

const ROUTES: ShellRoute[] = [
  { name: 'portfolio' },
  { name: 'send' },
  { name: 'send', token: '0x1111111111111111111111111111111111111111' },
  { name: 'receive' },
  { name: 'transactions' },
  { name: 'swap' },
  { name: 'discover' },
  { name: 'dex' },
  { name: 'colosseum' },
  { name: 'dao' },
  {
    name: 'paxfun',
    pool: '0x1111111111111111111111111111111111111111',
    symbol: 'PAX',
  },
  { name: 'wormhole' },
  { name: 'points' },
  { name: 'paxscan', path: '/tx/0x1234' },
  { name: 'pns' },
  { name: 'sidiora-fun' },
  { name: 'browser', url: 'https://example.com/path' },
  { name: 'settings' },
  { name: 'contacts' },
  { name: 'ramp' },
  {
    name: 'token-detail',
    token: '0x1111111111111111111111111111111111111111',
    symbol: 'TEST',
  },
  {
    name: 'tx-detail',
    hash: `0x${'1'.repeat(64)}`,
  },
];

describe('canonical shell navigation', () => {
  it('round-trips every user-addressable route through one URL schema', () => {
    expect(new Set(ROUTES.map((route) => route.name))).toEqual(
      new Set(SHELL_ROUTE_NAMES),
    );
    for (const route of ROUTES) {
      const parsed = parseRouteUrl(serializeRoute(route));
      expect(parsed).toEqual({ ok: true, value: route });
    }
  });

  it('rejects incomplete, hostile, ambiguous, and credential-bearing routes', () => {
    for (const route of [
      '/?screen=tx-detail&hash=0x1234',
      '/?screen=token-detail&token=unknown',
      '/?screen=browser&url=http%3A%2F%2Fexample.com',
      '/?screen=browser&url=https%3A%2F%2Fuser%3Apass%40example.com',
      '/?screen=portfolio&unknown=value',
      '/?screen=portfolio&token=pax',
      '/?screen=send&screen=swap',
      '/privacy?screen=settings',
    ]) {
      expect(parseRouteUrl(route).ok).toBe(false);
    }
  });

  it('preserves dApp query and fragment data required by the destination', () => {
    const parsed = parseRouteUrl(
      '/?screen=browser&url=https%3A%2F%2Fexample.com%2Fapp%3Ftoken%3Dsecret%23fragment',
    );
    expect(parsed).toEqual({
      ok: true,
      value: {
        name: 'browser',
        url: 'https://example.com/app?token=secret#fragment',
      },
    });
  });

  it('defines complete state behavior and custody guards for every route', () => {
    expect(Object.keys(ROUTE_POLICIES).sort()).toEqual(
      [...SHELL_ROUTE_NAMES].sort(),
    );
    for (const policy of Object.values(ROUTE_POLICIES)) {
      expect(Object.keys(policy.states)).toHaveLength(9);
      expect(Object.values(policy.states).every((state) => state === 'render')).toBe(
        true,
      );
    }
    expect(
      routeGuard(
        { name: 'send' },
        {
          custody: 'funded',
          unlocked: true,
          hasAccount: true,
          features: new Set(['dapp-browser']),
        },
      ),
    ).toEqual({ allowed: false, recovery: { name: 'portfolio' } });
    expect(
      routeGuard(
        { name: 'send' },
        {
          custody: 'self-custody',
          unlocked: true,
          hasAccount: true,
          features: new Set(['dapp-browser']),
        },
      ),
    ).toEqual({ allowed: true });
  });
});
