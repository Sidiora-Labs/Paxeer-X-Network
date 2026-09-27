// @vitest-environment jsdom

import React from 'react';

import type { PaxeerXReceipt } from 'types/api/paxeerXLists';

import { PAXEER_X_RECEIPTS_ITEM } from 'stubs/paxeerXLists';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import PaxeerXReceiptPage from './PaxeerXReceipt';

const receipt: PaxeerXReceipt = {
  ...PAXEER_X_RECEIPTS_ITEM,
  verification_status: 'checkpoint_finalised',
  payload_hash: '0x3ed9d81e7c1001bdda1caa1dc62c0acbbe3d2c671cdc20dc1e65efdaa4186967',
  transaction_hash: '0x8f9e7d6c5b4a39281706f5e4d3c2b1a0998877665544332211ffeeddccbbaa99',
  timestamp: '2023-05-22T18:00:36.000000Z',
};

const renderPage = async() => {
  const result = render(<PaxeerXReceiptPage/>);

  await screen.findByText('Verification status');

  return result;
};

describe('PaxeerXReceiptPageContent', () => {
  beforeEach(() => {
    routerState.pathname = '/paxeer-x/receipts/[id]';
    routerState.query = { id: receipt.id };
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify(receipt), { headers: { 'Content-Type': 'application/json' } });
  });

  it('heads the page with the receipt label, its identifier and a copy control', async() => {
    const { container } = await renderPage();

    expect(screen.getByRole('heading', { level: 1 }).textContent).toContain('Kernel receipt');

    const identifier = container.querySelector('[data-receipt-identifier]') as HTMLElement;

    expect(identifier.textContent).toContain(receipt.id);
    expect(identifier.querySelector('[aria-label="copy"]')).toBeTruthy();
  });

  it('puts the api entry beside the section pill', async() => {
    const { container } = await renderPage();

    const tabs = container.querySelector('[data-scan-section-tabs]') as HTMLElement;

    expect(Array.from(tabs.querySelectorAll('[data-tab]')).map((tab) => tab.textContent)).toEqual([ 'Overview' ]);
    expect(tabs.querySelector('[data-right-slot] [data-api-entry]')?.textContent).toBe('API');
  });

  it('renders the receipt on the shared key-value card', async() => {
    const { container } = await renderPage();

    const card = container.querySelector('[data-receipt-details-card]') as HTMLElement;

    expect(card).toBeTruthy();
    expect(card.querySelector('[data-field="id"]')?.textContent).toBe(receipt.id);
    expect(card.querySelector('[data-field="verification_status"]')?.textContent).toBe('Checkpoint finalised');
    expect(card.querySelector(`a[href="/tx/${ receipt.transaction_hash }"]`)).toBeTruthy();
    expect(card.querySelector(`a[href="/block/${ receipt.block_number }"]`)).toBeTruthy();
  });

  it('holds the card back until the node answers', () => {
    const { container } = render(<PaxeerXReceiptPage/>);

    expect(container.querySelector('[data-receipt-details-card]')).toBeNull();
    expect(container.querySelector('[data-receipt-identifier]')?.textContent).toContain(receipt.id);
  });

  it('keeps the settlement rung of the receipt on the ladder', async() => {
    const { container } = await renderPage();

    await waitFor(() => {
      expect(container.querySelector(`[data-rung="${ receipt.status }"]`)).toBeTruthy();
    });
  });
});
