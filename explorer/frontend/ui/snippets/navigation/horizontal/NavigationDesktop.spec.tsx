// @vitest-environment jsdom

import React from 'react';

import { render, routerState } from 'ui/shared/layout/testWrapper';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { screen, within } from 'vitest/lib';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_NAVIGATION_LAYOUT: 'horizontal',
    NEXT_PUBLIC_PAXEER_X_ENABLED: 'true',
    NEXT_PUBLIC_IS_ACCOUNT_SUPPORTED: 'false',
    NEXT_PUBLIC_MARKETPLACE_CONFIG_URL: '',
  };
});

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import NavigationDesktop from './NavigationDesktop';

const menuTexts = () => {
  const menu = screen.getByLabelText('Main navigation');

  const list = menu.querySelector('ul');

  return Array.from(list?.children ?? []).map((item) => item.textContent?.trim());
};

describe('NavigationDesktop', () => {
  beforeEach(() => {
    routerState.pathname = '/';
    routerState.query = {};
  });

  it('carries the network mark and the menu in one row', () => {
    const { container } = render(<NavigationDesktop/>);

    const row = container.querySelector('[data-label="brand-row"]');

    expect(row).toBeTruthy();
    expect(row?.querySelector('svg')).toBeTruthy();
    expect(within(row as HTMLElement).getByLabelText('Main navigation')).toBeTruthy();
  });

  it('groups the menu the way the scan layout orders it', () => {
    render(<NavigationDesktop/>);

    expect(menuTexts()).toEqual([ 'Home', 'Blockchain', 'Tokens', 'Kernel', 'Resources', 'API', 'More' ]);
  });

  it('marks the group the current page belongs to', () => {
    routerState.pathname = '/blocks';

    const { container } = render(<NavigationDesktop/>);

    const groups = Array.from(container.querySelectorAll('[data-label="nav-group"]'));
    const selected = groups.filter((group) => group.hasAttribute('data-selected'));

    expect(selected.map((group) => group.textContent?.trim())).toEqual([ 'Blockchain' ]);
  });

  it('leaves the sign in out while the account feature is off', () => {
    render(<NavigationDesktop/>);

    expect(screen.queryByText('Sign in')).toBeNull();
  });
});
