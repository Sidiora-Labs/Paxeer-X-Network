// @vitest-environment jsdom

import React from 'react';

import type { PaxeerXCapabilities, PaxeerXUnifiedAccount } from 'types/api/paxeerX';

import * as capabilitiesMock from 'mocks/paxeerX/capabilities';
import * as paxeerXMock from 'mocks/paxeerX/unifiedAccount';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from 'vitest/lib';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_PAXEER_X_ENABLED: 'true',
  };
});

const routerSpy = vi.hoisted(() => ({ push: vi.fn(() => Promise.resolve(true)) }));

vi.mock('next/router', async() => {
  const base = (await import('ui/shared/layout/testWrapper')).nextRouterModule();

  return {
    ...base,
    useRouter: () => ({ ...base.useRouter(), push: routerSpy.push }),
  };
});

import PaxeerXAccount from './PaxeerXAccount';

// The host runs the whole suite at once, and these trees mount real entities, so the first render of
// each of them reaches well past the default per-test budget.
vi.setConfig({ testTimeout: 60_000 });

const HASH = paxeerXMock.evmAddress;

const SECOND_PAGE = 'fixture-page-two';
const PREVIOUS_PAGE = 'fixture-page-previous';
const JSON_HEADERS = { 'Content-Type': 'application/json' };

const mockApi = (
  account: PaxeerXUnifiedAccount = paxeerXMock.unifiedAccount,
  capabilities: PaxeerXCapabilities = capabilitiesMock.allEnabled,
) => {
  fetchMock.mockResponse((request) => Promise.resolve({
    body: JSON.stringify(request.url.includes('/capabilities') ? capabilities : account),
    headers: { 'Content-Type': 'application/json' },
  }));
};

const renderPage = async() => {
  const result = render(<PaxeerXAccount/>);

  await screen.findByText(paxeerXMock.paxAddress);

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

describe('PaxeerXAccountPageContent', () => {
  beforeEach(() => {
    routerState.pathname = '/paxeer-x/account/[hash]';
    routerState.query = { hash: HASH };
    routerSpy.push.mockClear();
    sessionStorage.clear();
    fetchMock.resetMocks();
    mockApi();
  });

  it('heads the page with the account label, its identifier and a copy control', async() => {
    const { container } = await renderPage();

    expect(screen.getByRole('heading', { level: 1 }).textContent).toContain('Unified account');

    const identifier = container.querySelector('[data-account-identifier]') as HTMLElement;

    expect(identifier.textContent).toContain(HASH);
    expect(identifier.querySelector('[aria-label="copy"]')).toBeTruthy();
  });

  it('summarizes the account on three cards above the tab strip', async() => {
    const { container } = await renderPage();

    const details = container.querySelector('[data-account-details]') as HTMLElement;
    const cards = Array.from(details.querySelectorAll('[data-account-card]'));

    expect(cards.map((card) => card.querySelector('[data-card-title]')?.textContent))
      .toEqual([ 'Overview', 'More info', 'Node capabilities' ]);

    expect(details.querySelector('[data-field="assets-count"]')?.textContent).toBe('3');
    expect(details.querySelector('[data-field="activity-count"]')?.textContent).toBe('3');
    expect(details.querySelector('[data-field="identities-count"]')?.textContent).toBe('4');
  });

  it('answers the capability card from the node probe', async() => {
    const { container } = await renderPage();

    await waitFor(() => {
      expect(container.querySelector('[data-capability="addr"]')?.textContent).toBe('Available');
    });
    expect(container.querySelector('[data-capability="custody"]')?.textContent).toBe('Available');
    expect(container.querySelector('[data-capability="anchor"]')?.textContent).toBe('Available');
  });

  it('marks a surface the node does not answer for', async() => {
    fetchMock.resetMocks();
    mockApi(paxeerXMock.unifiedAccount, capabilitiesMock.addrOnly);

    const { container } = await renderPage();

    await waitFor(() => {
      expect(container.querySelector('[data-capability="custody"]')?.textContent).toBe('Not available');
    });
    expect(container.querySelector('[data-capability="addr"]')?.textContent).toBe('Available');
  });

  it('lists the sections of the account as pills with their counts', async() => {
    const { container } = await renderPage();

    const tabs = container.querySelector('[data-scan-section-tabs]') as HTMLElement;

    expect(Array.from(tabs.querySelectorAll('[data-tab]')).map((tab) => tab.textContent))
      .toEqual([ 'Identities(4)', 'Assets(3)', 'Activity(3)' ]);
    expect(tabs.querySelector('[data-right-slot] [data-api-entry]')?.textContent).toBe('API');
  });

  it('opens on the identities of the account inside the scan table card', async() => {
    const { container } = await renderPage();

    expect(container.querySelector('[data-scan-table-card] [data-label="paxeer-x-identities"]')).toBeTruthy();
    expect(container.querySelectorAll('[data-identity]')).toHaveLength(4);
  });

  it('routes to the section a pill names, keeping the account out of the query string', async() => {
    const { container } = await renderPage();

    (container.querySelector('[data-tab="assets"]') as HTMLElement).click();

    await waitFor(() => {
      expect(routerSpy.push).toHaveBeenCalledWith(
        { pathname: '/paxeer-x/account/[hash]', query: { hash: HASH, tab: 'assets' } },
        undefined,
        { shallow: true },
      );
    });
  });

  it('shows the assets of the account when the route names that section', async() => {
    routerState.query = { hash: HASH, tab: 'assets' };

    const { container } = render(<PaxeerXAccount/>);

    await waitFor(() => {
      expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
        .toBe('A total of 3 assets found');
    });
    expect(container.querySelectorAll('[data-asset]')).toHaveLength(3);
  });

  it('shows the activity of the account when the route names that section', async() => {
    routerState.query = { hash: HASH, tab: 'activity' };

    const { container } = render(<PaxeerXAccount/>);

    await waitFor(() => {
      expect(container.querySelectorAll('[data-activity]')).toHaveLength(3);
    });
    expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
      .toBe('A total of 3 activity entries found');
  });

  it('says so when the node does not answer the account binding surface', async() => {
    fetchMock.resetMocks();
    mockApi(paxeerXMock.unifiedAccount, capabilitiesMock.none);

    const { container } = render(<PaxeerXAccount/>);

    await waitFor(() => {
      expect(container.querySelector('[data-capability-notice="addr"]')).toBeTruthy();
    });
    expect(container.querySelector('[data-label="paxeer-x-identities"]')).toBeNull();
  });

  it('lets the account identifier wrap and keeps its inset for the wide layout only', async() => {
    const { container } = await renderPage();

    const identifier = container.querySelector('[data-account-identifier]') as HTMLElement;
    const insets = declarationsOf(identifier, LEFT_INSET);
    const spaced = insets.filter((rule) => isSpace(rule.value));

    expect(declarationsOf(identifier, 'flex-wrap').some((rule) => rule.value === 'wrap' && !isWide(rule.text))).toBe(true);
    expect(insets.length).toBeGreaterThan(0);
    expect(spaced.length).toBeGreaterThan(0);
    expect(spaced.every((rule) => isWide(rule.text))).toBe(true);
  });

  it('goes back to the cursor the session kept for the page before this one', async() => {
    routerState.query = { hash: HASH, tab: 'activity', cursor: SECOND_PAGE };
    sessionStorage.setItem(`paxeer-x-history:${ HASH.toLowerCase() }:${ SECOND_PAGE }`, PREVIOUS_PAGE);
    fetchMock.resetMocks();
    mockApi({ ...paxeerXMock.unifiedAccount, page_cursor: SECOND_PAGE, page_number: 2 });

    const { container } = render(<PaxeerXAccount/>);

    await waitFor(() => {
      expect(container.querySelector<HTMLButtonElement>('[data-control="prev"]')?.disabled).toBe(false);
    });
    container.querySelector<HTMLButtonElement>('[data-control="prev"]')?.click();

    await waitFor(() => {
      expect(routerSpy.push).toHaveBeenCalledWith(
        { pathname: '/paxeer-x/account/[hash]', query: { hash: HASH, tab: 'activity', cursor: PREVIOUS_PAGE } },
        undefined,
        { shallow: true },
      );
    });
  });

  it('returns to the first page cursor of the account from a later page', async() => {
    routerState.query = { hash: HASH, tab: 'activity', cursor: SECOND_PAGE };
    fetchMock.resetMocks();
    mockApi({ ...paxeerXMock.unifiedAccount, page_cursor: SECOND_PAGE, page_number: 2 });

    const { container } = render(<PaxeerXAccount/>);

    await waitFor(() => {
      expect(container.querySelector<HTMLButtonElement>('[data-control="first"]')?.disabled).toBe(false);
    });
    expect(container.querySelector<HTMLButtonElement>('[data-control="prev"]')?.disabled).toBe(true);
    container.querySelector<HTMLButtonElement>('[data-control="first"]')?.click();

    await waitFor(() => {
      expect(routerSpy.push).toHaveBeenCalledWith(
        { pathname: '/paxeer-x/account/[hash]', query: { hash: HASH, tab: 'activity', cursor: paxeerXMock.unifiedAccount.first_page_cursor } },
        undefined,
        { shallow: true },
      );
    });
  });

  it('asks the node for the same page again when retry is pressed after a failed answer', async() => {
    let accountRequests = 0;
    fetchMock.resetMocks();
    fetchMock.mockResponse((request) => {
      if (request.url.includes('/capabilities')) {
        return Promise.resolve({ body: JSON.stringify(capabilitiesMock.allEnabled), headers: JSON_HEADERS });
      }
      accountRequests += 1;
      if (accountRequests === 1) {
        return Promise.resolve({ status: 404, body: JSON.stringify({ message: 'Not found' }), headers: JSON_HEADERS });
      }
      return Promise.resolve({ body: JSON.stringify(paxeerXMock.unifiedAccount), headers: JSON_HEADERS });
    });

    render(<PaxeerXAccount/>);

    const retry = await screen.findByRole<HTMLButtonElement>('button', { name: 'Retry this page' });

    await waitFor(() => {
      expect(retry.disabled).toBe(false);
    });
    retry.click();

    await screen.findByText(paxeerXMock.paxAddress);
    expect(accountRequests).toBe(2);
    expect(screen.queryByRole('alert')).toBeNull();
    expect(routerSpy.push).not.toHaveBeenCalled();
  });

  it('starts a new history view without the cursor the route carried', async() => {
    routerState.query = { hash: HASH, tab: 'activity', cursor: '' };

    render(<PaxeerXAccount/>);

    const startOver = await screen.findByRole<HTMLButtonElement>('button', { name: 'Start a new history view' });

    expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Retry this page' }).disabled).toBe(true);
    startOver.click();

    await waitFor(() => {
      expect(routerSpy.push).toHaveBeenCalledWith(
        { pathname: '/paxeer-x/account/[hash]', query: { hash: HASH, tab: 'activity' } },
        undefined,
        { shallow: true },
      );
    });
  });
});
