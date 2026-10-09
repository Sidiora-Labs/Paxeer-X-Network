// @vitest-environment jsdom

import React from 'react';

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { within } from 'vitest/lib';

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

import LayoutDefault from './Layout';
import { render, routerState } from './testWrapper';

const SHELL_MARKERS = '[data-label="utility-bar"], [data-label="brand-row"], [data-label="content"], [data-label="footer-top"]';

describe('LayoutDefault', () => {
  beforeEach(() => {
    routerState.pathname = '/blocks';
    routerState.query = {};
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('stacks the utility bar, the brand row, the page and the footer in that order', () => {
    const { container } = render(<LayoutDefault><div>page body</div></LayoutDefault>);

    expect(Array.from(container.querySelectorAll(SHELL_MARKERS)).map((item) => item.getAttribute('data-label')))
      .toEqual([ 'utility-bar', 'brand-row', 'content', 'footer-top' ]);
  });

  it('keeps the brand row inside the sticky header', () => {
    const { container } = render(<LayoutDefault><div>page body</div></LayoutDefault>);

    const header = container.querySelector('[data-label="sticky-header"]') as HTMLElement;

    expect(header).toBeTruthy();
    expect(header.querySelector('[data-label="brand-row"]')).toBeTruthy();
    expect(header.querySelector('[data-label="utility-bar"]')).toBeNull();
  });

  it('puts the page inside the centred content container', () => {
    const { container } = render(<LayoutDefault><div>page body</div></LayoutDefault>);

    const content = container.querySelector('[data-label="content"]') as HTMLElement;

    expect(within(content).getByText('page body')).toBeTruthy();
  });

  it('carries one search box only, the one in the utility bar', () => {
    const { container } = render(<LayoutDefault><div>page body</div></LayoutDefault>);

    expect(container.querySelector('[data-label="utility-bar-search"]')).toBeTruthy();
    expect(container.querySelector('[data-label="content-search"]')).toBeNull();
  });
});
