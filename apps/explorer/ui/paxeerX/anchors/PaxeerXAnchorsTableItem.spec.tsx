// @vitest-environment jsdom

import React from 'react';

import { PAXEER_X_ANCHORS_ITEM } from 'stubs/paxeerXLists';
import { TableBody, TableRoot } from 'toolkit/chakra/table';
import { render } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { screen } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import PaxeerXAnchorsTableItem, { ANCHOR_SETTLEMENT_RUNG } from './PaxeerXAnchorsTableItem';

const renderItem = (item = PAXEER_X_ANCHORS_ITEM) => render(
  <TableRoot variant="scan">
    <TableBody>
      <PaxeerXAnchorsTableItem item={ item }/>
    </TableBody>
  </TableRoot>,
);

describe('PaxeerXAnchorsTableItem', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('marks the row with the checkpoint it carries', () => {
    const { container } = renderItem();

    expect(container.querySelector(`[data-anchor="${ PAXEER_X_ANCHORS_ITEM.checkpoint_id }"]`)).toBeTruthy();
  });

  it('offers the state root with a copy control', () => {
    const { container } = renderItem();

    expect(screen.getAllByLabelText('copy')).toHaveLength(1);
    expect(container.querySelectorAll('td')[2]?.textContent).not.toBe('—');
  });

  it('settles the row on the rung the ladder spells for a submitted checkpoint', () => {
    const { container } = renderItem();

    expect(ANCHOR_SETTLEMENT_RUNG).toBe('sealed');
    expect(container.querySelector(`[data-rung="${ ANCHOR_SETTLEMENT_RUNG }"]`)).toBeTruthy();
  });

  it('drops the copy control when the log carries no state root', () => {
    const { container } = renderItem({ ...PAXEER_X_ANCHORS_ITEM, state_root: null });

    expect(screen.queryByLabelText('copy')).toBeNull();
    expect(container.querySelectorAll('td')[2]?.textContent).toBe('—');
  });
});
