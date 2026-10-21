// @vitest-environment jsdom

import React from 'react';

import * as statsMock from 'mocks/stats/index';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import BlocksTabSlot from './BlocksTabSlot';

describe('BlocksTabSlot', () => {
  beforeEach(() => {
    routerState.pathname = '/blocks';
    routerState.query = {};
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify(statsMock.base), { headers: { 'Content-Type': 'application/json' } });
  });

  it('carries the network utilisation and the block countdown', () => {
    const { container } = render(<BlocksTabSlot/>);

    expect(container.textContent).toContain('Network utilization (last 50 blocks):');
    expect(container.querySelector('a[href^="/block/countdown"]')).not.toBeNull();
  });

  it('leaves the pagination to the table card', () => {
    const { container } = render(<BlocksTabSlot/>);

    expect(container.querySelector('[data-scan-pagination]')).toBeNull();
    expect(container.querySelector('[data-pagination]')).toBeNull();
  });
});
