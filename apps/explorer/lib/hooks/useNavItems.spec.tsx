// @vitest-environment jsdom

import type { NavItem, NavGroupItem } from 'types/client/navigation';

import { routerState } from 'ui/shared/layout/testWrapper';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { renderHook } from 'vitest/lib';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_NAVIGATION_LAYOUT: 'horizontal',
    NEXT_PUBLIC_PAXEER_X_ENABLED: 'true',
  };
});

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import useNavItems, { isGroupItem, isInternalItem } from './useNavItems';

const textsOf = (items: Array<NavItem | NavGroupItem>) => items.map((item) => item.text);

const groupOf = (items: Array<NavItem | NavGroupItem>, text: string): NavGroupItem => {
  const item = items.find((candidate) => candidate.text === text);

  if (!item || !isGroupItem(item)) {
    throw new Error(`${ text } is not a menu group`);
  }

  return item;
};

describe('useNavItems', () => {
  beforeEach(() => {
    routerState.pathname = '/';
    routerState.query = {};
  });

  it('opens the menu with home and carries the scan groups in order', () => {
    const { result } = renderHook(() => useNavItems());

    const texts = textsOf(result.current.mainNavItems);

    expect(texts.slice(0, 4)).toEqual([ 'Home', 'Blockchain', 'Tokens', 'Kernel' ]);
    expect(texts).toContain('Resources');
    expect(texts[texts.length - 1]).toBe('More');
  });

  it('points home at the root route and marks it on the home page', () => {
    const { result } = renderHook(() => useNavItems());

    const home = result.current.mainNavItems[0];

    if (isGroupItem(home) || !isInternalItem(home)) {
      throw new Error('home is a plain link, not a menu group');
    }

    expect(home.nextRoute).toEqual({ pathname: '/' });
    expect(home.isActive).toBe(true);
  });

  it('lists the pending transactions next to the transactions', () => {
    const { result } = renderHook(() => useNavItems());

    const blockchain = groupOf(result.current.mainNavItems, 'Blockchain');

    expect(textsOf(blockchain.subItems.flat()).slice(0, 2)).toEqual([ 'Transactions', 'Pending transactions' ]);
  });

  it('marks the pending transactions on the pending tab and leaves the transactions unmarked', () => {
    routerState.pathname = '/txs';
    routerState.query = { tab: 'pending' };

    const { result } = renderHook(() => useNavItems());

    const blockchain = groupOf(result.current.mainNavItems, 'Blockchain');
    const items = blockchain.subItems.flat();
    const transactions = items.find((item) => item.text === 'Transactions');
    const pending = items.find((item) => item.text === 'Pending transactions');

    expect(transactions && isInternalItem(transactions) && transactions.isActive).toBe(false);
    expect(pending && isInternalItem(pending) && pending.isActive).toBe(true);
    expect(blockchain.isActive).toBe(true);
  });

  it('collects the kernel pages in their own group', () => {
    const { result } = renderHook(() => useNavItems());

    const kernel = groupOf(result.current.mainNavItems, 'Kernel');

    expect(textsOf(kernel.subItems.flat())).toEqual([ 'Anchor checkpoints', 'Kernel receipts' ]);
  });

  it('gives the account pages their own column of the more group', () => {
    const { result } = renderHook(() => useNavItems());

    const more = groupOf(result.current.mainNavItems, 'More');
    const columns = (more.subItems as Array<NavItem | Array<NavItem>>)
      .filter((column): column is Array<NavItem> => Array.isArray(column));

    expect(columns).toHaveLength(2);
    expect(textsOf(columns[0])).toContain('Verify contract');
    expect(textsOf(columns[1])).toEqual(textsOf(result.current.accountNavItems));
  });
});
