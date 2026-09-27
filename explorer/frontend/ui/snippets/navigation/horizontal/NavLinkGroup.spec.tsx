// @vitest-environment jsdom

import React from 'react';

import type { NavGroupItem } from 'types/client/navigation';

import useNavItems, { isGroupItem } from 'lib/hooks/useNavItems';
import { render } from 'ui/shared/layout/testWrapper';
import { describe, it, expect, vi } from 'vitest';
import { fireEvent, renderHook } from 'vitest/lib';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_NAVIGATION_LAYOUT: 'horizontal',
    NEXT_PUBLIC_PAXEER_X_ENABLED: 'true',
  };
});

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import NavLinkGroup from './NavLinkGroup';

const groupItem = (text: string): NavGroupItem => {
  const { result } = renderHook(() => useNavItems());
  const item = result.current.mainNavItems.find((candidate) => candidate.text === text);

  if (!item || !isGroupItem(item)) {
    throw new Error(`${ text } is not a menu group`);
  }

  return item;
};

describe('NavLinkGroup', () => {
  it('names the group and keeps its panel closed until it is asked for', () => {
    const { container } = render(<NavLinkGroup item={ groupItem('Kernel') }/>);

    const trigger = container.querySelector('[data-label="nav-group"]');

    expect(trigger?.textContent?.trim()).toBe('Kernel');
    expect(trigger?.hasAttribute('data-active')).toBe(false);
  });

  it('opens the panel on a click and lists the pages of the group', () => {
    const { container } = render(<NavLinkGroup item={ groupItem('Kernel') }/>);

    const trigger = container.querySelector('[data-label="nav-group"]') as HTMLElement;

    fireEvent.click(trigger);

    expect(trigger.hasAttribute('data-active')).toBe(true);

    const panel = document.querySelector('[data-scope="tooltip"][data-part="content"]');

    expect(Array.from(panel?.querySelectorAll('li') ?? []).map((item) => item.textContent?.trim()))
      .toEqual([ 'Anchor checkpoints', 'Kernel receipts' ]);
  });

  it('closes the panel on a second click', () => {
    const { container } = render(<NavLinkGroup item={ groupItem('Kernel') }/>);

    const trigger = container.querySelector('[data-label="nav-group"]') as HTMLElement;

    fireEvent.click(trigger);
    fireEvent.click(trigger);

    expect(trigger.hasAttribute('data-active')).toBe(false);
  });
});
