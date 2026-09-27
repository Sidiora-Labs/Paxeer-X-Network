// @vitest-environment jsdom

import React from 'react';

import * as blockMock from 'mocks/blocks/block';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import BlockDetails from './BlockDetails';
import useBlockQuery from './useBlockQuery';

const responseInit = {
  headers: {
    'Content-Type': 'application/json',
  },
};

// The page hands the details card the query the block endpoint fills, so the spec drives the real
// hook against the repository's own payload rather than a hand-built query object.
const BlockDetailsWithQuery = ({ heightOrHash }: { heightOrHash: string }) => {
  const query = useBlockQuery({ heightOrHash });

  return <BlockDetails query={ query }/>;
};

const renderDetails = async() => {
  render(<BlockDetailsWithQuery heightOrHash={ String(blockMock.base.height) }/>);

  await screen.findByText(blockMock.base.hash);
};

const card = () => document.querySelector('[data-block-details-card]') as HTMLElement;

const labelsOf = (root: HTMLElement) =>
  Array.from(root.querySelectorAll('[data-scan-key]')).map((item) => item.textContent);

const valuesOf = (root: HTMLElement) => Array.from(root.querySelectorAll('[data-scan-value]'));

describe('BlockDetails', () => {
  beforeEach(() => {
    routerState.pathname = '/block/[height_or_hash]';
    routerState.query = { height_or_hash: String(blockMock.base.height) };
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify(blockMock.base), responseInit);
  });

  it('carries the overview rows in the order the scan layout lists them', async() => {
    await renderDetails();

    expect(labelsOf(card())).toEqual([
      'Block height',
      'Timestamp',
      'Transactions',
      'Validator',
      'Hash',
      'Block reward',
      'POA Mania Reward',
      'Emission Reward',
      'Difficulty',
      'Total difficulty',
      'Size',
      'Gas used',
      'Gas limit',
      'Base fee per gas',
      'Burnt fees',
      'Priority fee / Tip',
      'Extra data',
    ]);
  });

  it('opens the height row with its value and its previous and next controls', async() => {
    await renderDetails();

    const value = valuesOf(card())[0];

    expect(value.textContent).toContain(String(blockMock.base.height));
    expect(value.querySelector('[data-prev-next]')).not.toBeNull();
  });

  it('prints the transactions sentence, the hash, the reward, the difficulty and the size', async() => {
    await renderDetails();

    const values = valuesOf(card());

    expect(values[2].textContent).toBe('5 txns in this block');
    expect(values[4].textContent).toContain(blockMock.base.hash);
    expect(values[5].textContent).toContain('1.02685360751');
    expect(values[8].textContent).toBe('340,282,366,920,938,463,463,374,607,431,768,211,454');
    expect(values[10].textContent).toBe('2,448 bytes');
  });

  it('gives the gas used its percentage and the burnt fees their flame', async() => {
    await renderDetails();

    const values = valuesOf(card());

    expect(values[11].textContent).toContain('544,920');
    expect(values[11].textContent).toContain('4.36%');
    expect(values[14].textContent).toContain('0.0054492');
    expect(values[14].querySelector('svg')).not.toBeNull();
  });

  it('breaks the card with a divider before the hash and before the gas used', async() => {
    await renderDetails();

    const cells = Array.from(card().querySelectorAll('[data-scan-key], [data-scan-divider]'));
    const dividers = cells
      .map((cell, index) => ({ cell, index }))
      .filter(({ cell }) => cell.hasAttribute('data-scan-divider'))
      .map(({ index }) => index);

    expect(dividers).toHaveLength(2);
    expect(cells[dividers[0] + 1].textContent).toBe('Hash');
    expect(cells[dividers[1] + 1].textContent).toBe('Gas used');
  });

  it('closes the card with the extra data as a read-only field', async() => {
    await renderDetails();

    const field = card().querySelector('[data-extra-data]') as HTMLTextAreaElement;

    expect(field.tagName).toBe('TEXTAREA');
    expect(field.readOnly).toBe(true);
    expect(field.value).toBe(blockMock.base.extra_data);
  });

  it('folds the rows the overview leaves out into the more details expander', async() => {
    await renderDetails();

    const expander = document.querySelector('[data-scan-expander]') as HTMLElement;

    expect(expander.getAttribute('data-open')).toBe('false');
    expect(expander.querySelector('[data-label]')?.textContent).toBe('More Details:');
    expect(expander.querySelector('[data-toggle]')?.textContent).toBe('+ Click to show more');

    fireEvent.click(expander.querySelector('[data-toggle]') as Element);

    await waitFor(() => {
      expect(expander.getAttribute('data-open')).toBe('true');
    });

    expect(labelsOf(expander)).toEqual([ 'Parent hash', 'Nonce' ]);
    expect(expander.querySelector('[data-content]')?.textContent).toContain(blockMock.base.nonce);
  });
});
