// @vitest-environment jsdom

import React from 'react';

import { Provider } from 'toolkit/chakra/provider';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from 'vitest/lib';

import ScanSectionTabs from './ScanSectionTabs';

// jsdom implements no CSS media queries, and the Chakra provider reads window.matchMedia on mount
const createMediaQueryList = (query: string): MediaQueryList => ({
  matches: false,
  media: query,
  onchange: null,
  addListener: () => undefined,
  removeListener: () => undefined,
  addEventListener: () => undefined,
  removeEventListener: () => undefined,
  dispatchEvent: () => false,
});

beforeAll(() => {
  Object.defineProperty(window, 'matchMedia', { writable: true, value: createMediaQueryList });
});

// the suite runs without vitest globals, so the testing library cannot register its own teardown
afterEach(cleanup);

// Chakra writes the pill rules into the document when it renders, so the spec reads the declarations
// back off the style elements instead of off a layout jsdom never performs
const collectCss = (): string => Array.from(document.querySelectorAll('style'))
  .map((tag) => {
    if (tag.textContent) {
      return tag.textContent;
    }

    try {
      return tag.sheet ? Array.from(tag.sheet.cssRules).map((rule) => rule.cssText).join('') : '';
    } catch {
      return '';
    }
  })
  .join('')
  .replace(/\s+/g, '');

const declarationsOf = (element: Element, suffix = ''): string => {
  const css = collectCss();

  return Array.from(element.classList)
    .flatMap((name) => Array.from(css.matchAll(new RegExp(`\\.${ name }${ suffix }\\{([^}]*)\\}`, 'g'))).map((match) => match[1]))
    .join(';');
};

const items = [
  { id: 'transactions', title: 'Transactions', count: 1234 },
  { id: 'transfers', title: 'Token transfers', count: 56 },
  { id: 'logs', title: 'Logs' },
];

const noop = vi.fn();

describe('ScanSectionTabs', () => {
  it('keeps the tabs in the order it is given', () => {
    const { container } = render(
      <Provider>
        <ScanSectionTabs items={ items } value="transactions" onValueChange={ noop }/>
      </Provider>,
    );

    const tabs = Array.from(container.querySelectorAll('[data-tab]'));

    expect(tabs.map((tab) => tab.getAttribute('data-tab'))).toEqual([ 'transactions', 'transfers', 'logs' ]);
  });

  it('puts a count in parentheses after the title and leaves it out when there is none', () => {
    const { container } = render(
      <Provider>
        <ScanSectionTabs items={ items } value="transactions" onValueChange={ noop }/>
      </Provider>,
    );

    expect(container.querySelector('[data-tab="transactions"]')?.textContent).toBe('Transactions(1,234)');
    expect(container.querySelector('[data-tab="logs"]')?.textContent).toBe('Logs');
    expect(container.querySelector('[data-tab="logs"] [data-count]')).toBeNull();
  });

  it('marks the tab in force as the selected one', () => {
    const { container } = render(
      <Provider>
        <ScanSectionTabs items={ items } value="transfers" onValueChange={ noop }/>
      </Provider>,
    );

    expect(container.querySelector('[data-tab="transfers"]')?.getAttribute('aria-selected')).toBe('true');
    expect(container.querySelector('[data-tab="transactions"]')?.getAttribute('aria-selected')).toBe('false');
  });

  it('reports the tab a reader picks', async() => {
    const onValueChange = vi.fn();
    const { container } = render(
      <Provider>
        <ScanSectionTabs items={ items } value="transactions" onValueChange={ onValueChange }/>
      </Provider>,
    );

    fireEvent.click(container.querySelector('[data-tab="transfers"]') as Element);

    await waitFor(() => {
      expect(onValueChange).toHaveBeenCalledWith('transfers');
    });
  });

  it('hands the right of the row to the slot it is given', () => {
    const { container } = render(
      <Provider>
        <ScanSectionTabs items={ items } value="transactions" onValueChange={ noop } rightSlot={ <span>Download Page Data</span> }/>
      </Provider>,
    );

    expect(container.querySelector('[data-right-slot]')?.textContent).toBe('Download Page Data');
  });

  it('lets a pill grow with its label instead of holding a height that clips it', () => {
    const { container } = render(
      <Provider>
        <ScanSectionTabs items={ items } value="transactions" onValueChange={ noop }/>
      </Provider>,
    );

    const pill = container.querySelector('[data-tab="transfers"]') as Element;
    const declarations = declarationsOf(pill);

    expect(declarations).toMatch(/(?:^|;)height:auto/);
    expect(declarations).toContain('min-height:var(--tabs-height)');
    expect(declarations).toContain('white-space:nowrap');
    expect(declarations).not.toMatch(/(?:^|;)height:[123]?\dpx/);
  });

  it('gives way on the title and never on the count', () => {
    const { container } = render(
      <Provider>
        <ScanSectionTabs items={ items } value="transactions" onValueChange={ noop }/>
      </Provider>,
    );

    const pill = container.querySelector('[data-tab="transactions"]') as Element;
    const title = pill.querySelector('[data-tab-title]') as Element;
    const count = pill.querySelector('[data-count]') as Element;

    expect(title.textContent).toBe('Transactions');
    expect(count.textContent).toBe('(1,234)');
    expect(declarationsOf(title)).toContain('text-overflow:ellipsis');
    expect(declarationsOf(title)).toContain('min-width:0');
    expect(declarationsOf(count)).toContain('flex-shrink:0');
  });
});
