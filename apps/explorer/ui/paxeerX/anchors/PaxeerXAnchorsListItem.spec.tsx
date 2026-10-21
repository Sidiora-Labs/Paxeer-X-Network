// @vitest-environment jsdom

import React from 'react';

import { PAXEER_X_ANCHORS_ITEM } from 'stubs/paxeerXLists';
import { render } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { screen } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import PaxeerXAnchorsListItem from './PaxeerXAnchorsListItem';
import { ANCHOR_SETTLEMENT_RUNG } from './PaxeerXAnchorsTableItem';

const renderItem = (item = PAXEER_X_ANCHORS_ITEM) => render(<PaxeerXAnchorsListItem item={ item }/>);

describe('PaxeerXAnchorsListItem', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('carries the same fields as the table row, one label per line', () => {
    renderItem();

    expect(screen.getByText('Checkpoint height')).toBeTruthy();
    expect(screen.getByText('Sealed height')).toBeTruthy();
    expect(screen.getByText('State root')).toBeTruthy();
    expect(screen.getByText('Block')).toBeTruthy();
    expect(screen.getByText('Age')).toBeTruthy();
    expect(screen.getByText('Settlement')).toBeTruthy();
  });

  it('spells the heights and links the block', () => {
    const { container } = renderItem();

    expect(container.querySelector('[data-label="checkpoint-height"]')?.textContent).toBe(String(PAXEER_X_ANCHORS_ITEM.checkpoint_height));
    expect(container.querySelector('[data-label="sealed-height"]')?.textContent).toBe(String(PAXEER_X_ANCHORS_ITEM.sealed_height));
    expect(container.querySelector(`a[href="/block/${ PAXEER_X_ANCHORS_ITEM.block_number }"]`)).toBeTruthy();
  });

  it('settles the entry on the same rung the table row uses', () => {
    const { container } = renderItem();

    expect(container.querySelector(`[data-rung="${ ANCHOR_SETTLEMENT_RUNG }"]`)).toBeTruthy();
  });

  it('marks a checkpoint the log left without heights or a state root', () => {
    const { container } = renderItem({
      ...PAXEER_X_ANCHORS_ITEM,
      checkpoint_height: null,
      sealed_height: null,
      state_root: null,
    });

    expect(container.querySelector('[data-label="checkpoint-height"]')?.textContent).toBe('—');
    expect(container.querySelector('[data-label="sealed-height"]')?.textContent).toBe('—');
    expect(screen.queryByLabelText('copy')).toBeNull();
  });
});
