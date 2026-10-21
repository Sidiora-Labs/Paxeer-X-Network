// @vitest-environment jsdom

import React from 'react';

import type { PaxeerXReceipt } from 'types/api/paxeerXLists';

import { PAXEER_X_RECEIPTS_ITEM } from 'stubs/paxeerXLists';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, screen, waitFor } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

// The host runs the whole suite at once, and these trees mount real entities, so the first render of
// each of them reaches well past the default per-test budget.
vi.setConfig({ testTimeout: 60_000 });

import PaxeerXReceiptPage from './PaxeerXReceipt';

const receipt: PaxeerXReceipt = {
  ...PAXEER_X_RECEIPTS_ITEM,
  verification_status: 'checkpoint_finalised',
  payload_hash: '0x3ed9d81e7c1001bdda1caa1dc62c0acbbe3d2c671cdc20dc1e65efdaa4186967',
  transaction_hash: '0x8f9e7d6c5b4a39281706f5e4d3c2b1a0998877665544332211ffeeddccbbaa99',
  timestamp: '2023-05-22T18:00:36.000000Z',
};

const JSON_HEADERS = { 'Content-Type': 'application/json' };

const setVisibility = (state: DocumentVisibilityState) => {
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state });
  document.dispatchEvent(new Event('visibilitychange'));
};

const setOnline = (online: boolean) => {
  Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => online });
  window.dispatchEvent(new Event(online ? 'online' : 'offline'));
};

const freshnessOf = (container: HTMLElement) => container.querySelector('[data-receipt-freshness]');

const renderPage = async() => {
  const result = render(<PaxeerXReceiptPage/>);

  await screen.findByText('Verification status');

  return result;
};

// A responsive style prop reaches the document as one rule per breakpoint, each of them inserted
// into a style element of its own, and jsdom performs no layout, so the widths a declaration belongs
// to are read from the rules that name the element's own class.
const ruleTexts = () => {
  const inline = Array.from(document.querySelectorAll('style')).map((node) => node.textContent ?? '');
  const parsed = Array.from(document.styleSheets).flatMap((sheet) => {
    try {
      return Array.from(sheet.cssRules).map((rule) => rule.cssText);
    } catch {
      return [];
    }
  });

  return [ ...inline, ...parsed ].filter(Boolean);
};

const declarationsOf = (element: Element, property: string) => ruleTexts()
  .filter((text) => Array.from(element.classList).some((name) => new RegExp(`\\.${ name }(?![\\w-])`).test(text)))
  .map((text) => ({ text, value: new RegExp(`(?:^|[;{\\s])${ property }\\s*:\\s*([^;}]+)`).exec(text)?.[1]?.trim() }))
  .filter((rule): rule is { text: string; value: string } => rule.value !== undefined);

// Each rule is inserted on its own, so a declaration belongs to the wide layout when the rule that
// carries it is a breakpoint rule and not the base rule, which declares its own minimum width.
const isWide = (text: string) => /^\s*@media[^{]*\(min-width/.test(text);

const LEFT_INSET = 'margin-(?:left|inline-start)';

// The scale resolves a zero inset either to the plain length or to the token of the same name.
const isSpace = (value: string) => !/^0[a-z%]*$/.test(value) && !/spacing-0\b/.test(value);

describe('PaxeerXReceiptPageContent', () => {
  beforeEach(() => {
    routerState.pathname = '/paxeer-x/receipts/[id]';
    routerState.query = { id: receipt.id };
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify(receipt), { headers: JSON_HEADERS });
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

  it('lets the receipt identifier wrap and keeps its inset for the wide layout only', () => {
    const { container } = render(<PaxeerXReceiptPage/>);

    const identifier = container.querySelector('[data-receipt-identifier]') as HTMLElement;
    const insets = declarationsOf(identifier, LEFT_INSET);
    const spaced = insets.filter((rule) => isSpace(rule.value));

    expect(declarationsOf(identifier, 'flex-wrap').some((rule) => rule.value === 'wrap' && !isWide(rule.text))).toBe(true);
    expect(insets.length).toBeGreaterThan(0);
    expect(spaced.length).toBeGreaterThan(0);
    expect(spaced.every((rule) => isWide(rule.text))).toBe(true);
  });

  describe('freshness of the evidence', () => {
    afterEach(() => {
      Reflect.deleteProperty(document, 'visibilityState');
      Reflect.deleteProperty(navigator, 'onLine');
    });

    it('reports the evidence current once the node has answered', async() => {
      const { container } = await renderPage();

      await waitFor(() => {
        expect(freshnessOf(container)?.getAttribute('data-receipt-freshness')).toBe('current');
      });
      expect(freshnessOf(container)?.textContent).toContain('Latest indexed response received. Checking for updates.');
      expect(container.querySelector('[data-receipt-checked-at]')).toBeTruthy();
    });

    it('reports the evidence complete once the receipt is final and anchored', async() => {
      fetchMock.mockResponse(
        JSON.stringify({ ...receipt, status: 'final', verification_status: 'settlement_anchored' }),
        { headers: JSON_HEADERS },
      );

      const { container } = await renderPage();

      await waitFor(() => {
        expect(freshnessOf(container)?.getAttribute('data-receipt-freshness')).toBe('complete');
      });
      expect(freshnessOf(container)?.textContent)
        .toContain('Final settlement and anchored verification reported. Automatic refresh stopped.');
    });

    it('reports the refresh paused while the page is hidden', async() => {
      const { container } = await renderPage();

      act(() => {
        setVisibility('hidden');
      });

      await waitFor(() => {
        expect(freshnessOf(container)?.getAttribute('data-receipt-freshness')).toBe('paused');
      });
      expect(freshnessOf(container)?.textContent).toContain('Refresh paused while this page is hidden.');
    });

    it('reports a refresh in flight when the page comes back into view', async() => {
      let receiptRequests = 0;
      fetchMock.mockResponse((request) => {
        if (!request.url.includes(`/paxeer-x/receipts/${ receipt.id }`)) {
          return Promise.resolve({ body: JSON.stringify(receipt), headers: JSON_HEADERS });
        }
        receiptRequests += 1;
        if (receiptRequests === 1) {
          return Promise.resolve({ body: JSON.stringify(receipt), headers: JSON_HEADERS });
        }
        return new Promise<string>(() => undefined);
      });

      const { container } = await renderPage();

      act(() => {
        setVisibility('hidden');
      });
      await waitFor(() => {
        expect(freshnessOf(container)?.getAttribute('data-receipt-freshness')).toBe('paused');
      });

      act(() => {
        setVisibility('visible');
      });

      await waitFor(() => {
        expect(freshnessOf(container)?.getAttribute('data-receipt-freshness')).toBe('refreshing');
      });
      expect(freshnessOf(container)?.textContent).toContain('Checking for newer indexed evidence.');
      expect(receiptRequests).toBe(2);
    });

    it('reports the evidence stale and keeps it when the connection drops', async() => {
      const { container } = await renderPage();

      act(() => {
        setOnline(false);
      });

      await waitFor(() => {
        expect(freshnessOf(container)?.getAttribute('data-receipt-freshness')).toBe('stale');
      });
      expect(freshnessOf(container)?.textContent).toContain('Offline. Retaining the last indexed evidence.');
      expect(container.querySelector('[data-field="id"]')?.textContent).toBe(receipt.id);
    });
  });
});
