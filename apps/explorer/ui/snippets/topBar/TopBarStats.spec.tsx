// @vitest-environment jsdom

import React from 'react';

import * as statsMock from 'mocks/stats/index';
import { render } from 'ui/shared/layout/testWrapper';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { screen } from 'vitest/lib';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_NAVIGATION_LAYOUT: 'horizontal',
    NEXT_PUBLIC_FEATURED_NETWORKS: 'https://localhost:3000/featured-networks.json',
  };
});

import TopBarStats from './TopBarStats';

const responseInit = {
  headers: {
    'Content-Type': 'application/json',
  },
};

describe('TopBarStats', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
  });

  it('names the native coin price and the gas price and prints their values', async() => {
    fetchMock.mockResponse(JSON.stringify(statsMock.base), responseInit);

    render(<TopBarStats/>);

    expect(await screen.findByText('$0.001997')).toBeTruthy();
    expect(screen.getByText('ETH Price:')).toBeTruthy();
    expect(screen.getByText('-7.42%')).toBeTruthy();
    expect(screen.getByText('Gas:')).toBeTruthy();
  });

  it('opens with the price rather than a separator when the network menu is configured', async() => {
    fetchMock.mockResponse(JSON.stringify(statsMock.base), responseInit);

    render(<TopBarStats/>);

    await screen.findByText('$0.001997');

    expect(screen.getAllByText('|')).toHaveLength(1);
  });
});
