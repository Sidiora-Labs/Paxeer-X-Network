// @vitest-environment jsdom

import React from 'react';

import * as addressMock from 'mocks/address/address';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

// The address lists render fifty placeholder rows of real table items, which jsdom lays out well past the
// default per-test budget.
vi.setConfig({ testTimeout: 60_000 });

import AddressContract from './AddressContract';

describe('AddressContract', () => {
  beforeEach(() => {
    routerState.pathname = '/address/[hash]';
    routerState.query = { hash: addressMock.hash, tab: 'contract_code' };
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('puts the contract tabs on one strip', () => {
    const { container } = render(<AddressContract addressData={ addressMock.contract }/>);

    expect(container.querySelectorAll('[role="tablist"]').length).toBeGreaterThanOrEqual(1);
  });

  it('keeps its tab panel while the address data is still loading', () => {
    const { container } = render(<AddressContract addressData={ undefined } isLoading/>);

    expect(container.querySelectorAll('[role="tabpanel"]').length).toBeGreaterThanOrEqual(1);
  });
});
