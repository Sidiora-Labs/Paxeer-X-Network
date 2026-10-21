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

import LayoutHome from './LayoutHome';
import { render, routerState } from './testWrapper';

describe('LayoutHome', () => {
  beforeEach(() => {
    routerState.pathname = '/';
    routerState.query = {};
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('lays a hero band behind the top of the home page', () => {
    const { container } = render(<LayoutHome><div>home page</div></LayoutHome>);

    const band = container.querySelector('[data-label="hero-band"]') as HTMLElement;
    const page = within(container).getByText('home page');

    expect(band).toBeTruthy();
    expect(band.compareDocumentPosition(page) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('keeps the shell and the footer around the home page', () => {
    const { container } = render(<LayoutHome><div>home page</div></LayoutHome>);

    expect(Array.from(container.querySelectorAll('[data-label="utility-bar"], [data-label="brand-row"], [data-label="footer-top"]'))
      .map((item) => item.getAttribute('data-label')))
      .toEqual([ 'utility-bar', 'brand-row', 'footer-top' ]);
  });

  it('leaves the search box to the hero band on the home page', () => {
    const { container } = render(<LayoutHome><div>home page</div></LayoutHome>);

    expect(container.querySelector('[data-label="utility-bar-search"]')).toBeNull();
    expect(container.querySelector('[data-label="content-search"]')).toBeNull();
  });
});
