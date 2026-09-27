// @vitest-environment jsdom

import React from 'react';

import type { PaxeerXAnchorsItem } from 'types/api/paxeerXLists';

import { PAXEER_X_ANCHORS_ITEM } from 'stubs/paxeerXLists';
import { render } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import PaxeerXAnchorsTable from './PaxeerXAnchorsTable';
import { ANCHOR_SETTLEMENT_RUNG } from './PaxeerXAnchorsTableItem';

const sparse: PaxeerXAnchorsItem = {
  ...PAXEER_X_ANCHORS_ITEM,
  batch_number: PAXEER_X_ANCHORS_ITEM.batch_number - 1,
  checkpoint_id: '0x4b3c2d1e0f9a8b7c6d5e4f30211223344556677889900aabbccddeeff00112233',
  checkpoint_height: null,
  sealed_height: null,
  state_root: null,
  block_number: PAXEER_X_ANCHORS_ITEM.block_number - 1,
};

const items = [ PAXEER_X_ANCHORS_ITEM, sparse ];

const renderTable = () => render(<PaxeerXAnchorsTable items={ items }/>);

describe('PaxeerXAnchorsTable', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('heads the table with the anchor columns in order', () => {
    const { container } = renderTable();

    expect(Array.from(container.querySelectorAll('thead th')).map((cell) => cell.textContent))
      .toEqual([ 'Checkpoint height', 'Sealed height', 'State root', 'Block', 'Age', 'Settlement' ]);
  });

  it('renders one row per checkpoint, each marked by its checkpoint id', () => {
    const { container } = renderTable();

    expect(container.querySelector('[data-label="paxeer-x-anchors"]')).toBeTruthy();
    expect(Array.from(container.querySelectorAll('[data-anchor]')).map((row) => row.getAttribute('data-anchor')))
      .toEqual(items.map((item) => item.checkpoint_id));
  });

  it('spells the heights of a checkpoint and links its block', () => {
    const { container } = renderTable();

    const row = container.querySelector(`[data-anchor="${ PAXEER_X_ANCHORS_ITEM.checkpoint_id }"]`) as HTMLElement;

    expect(row.querySelector('[data-label="checkpoint-height"]')?.textContent).toBe(String(PAXEER_X_ANCHORS_ITEM.checkpoint_height));
    expect(row.querySelector('[data-label="sealed-height"]')?.textContent).toBe(String(PAXEER_X_ANCHORS_ITEM.sealed_height));
    expect(row.querySelector(`a[href="/block/${ PAXEER_X_ANCHORS_ITEM.block_number }"]`)?.textContent)
      .toBe(String(PAXEER_X_ANCHORS_ITEM.block_number));
  });

  it('marks the columns the anchor log leaves empty', () => {
    const { container } = renderTable();

    const row = container.querySelector(`[data-anchor="${ sparse.checkpoint_id }"]`) as HTMLElement;

    expect(row.querySelector('[data-label="checkpoint-height"]')?.textContent).toBe('—');
    expect(row.querySelector('[data-label="sealed-height"]')?.textContent).toBe('—');
    expect(row.querySelectorAll('td')[2]?.textContent).toBe('—');
  });

  it('puts every accepted checkpoint on the same rung of the settlement ladder', () => {
    const { container } = renderTable();

    expect(container.querySelectorAll(`[data-anchor] [data-rung="${ ANCHOR_SETTLEMENT_RUNG }"]`)).toHaveLength(items.length);
  });
});
