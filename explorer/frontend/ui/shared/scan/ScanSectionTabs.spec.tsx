// @vitest-environment jsdom

import React from 'react';

import { Provider } from 'toolkit/chakra/provider';
import { pillLineHeight } from 'toolkit/theme/recipes/pillSizing';
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

describe.each([ 'light', 'dark' ] as const)('ScanSectionTabs in %s appearance', (appearance) => {
  it('keeps the tabs in the order it is given', () => {
    const { container } = render(
      <Provider forcedTheme={ appearance } enableSystem={ false }>
        <ScanSectionTabs items={ items } value="transactions" onValueChange={ noop }/>
      </Provider>,
    );

    const tabs = Array.from(container.querySelectorAll('[data-tab]'));

    expect(tabs.map((tab) => tab.getAttribute('data-tab'))).toEqual([ 'transactions', 'transfers', 'logs' ]);
  });

  it('puts a count in parentheses after the title and leaves it out when there is none', () => {
    const { container } = render(
      <Provider forcedTheme={ appearance } enableSystem={ false }>
        <ScanSectionTabs items={ items } value="transactions" onValueChange={ noop }/>
      </Provider>,
    );

    expect(container.querySelector('[data-tab="transactions"]')?.textContent).toBe('Transactions(1,234)');
    expect(container.querySelector('[data-tab="logs"]')?.textContent).toBe('Logs');
    expect(container.querySelector('[data-tab="logs"] [data-count]')).toBeNull();
  });

  it('marks the tab in force as the selected one', () => {
    const { container } = render(
      <Provider forcedTheme={ appearance } enableSystem={ false }>
        <ScanSectionTabs items={ items } value="transfers" onValueChange={ noop }/>
      </Provider>,
    );

    expect(container.querySelector('[data-tab="transfers"]')?.getAttribute('aria-selected')).toBe('true');
    expect(container.querySelector('[data-tab="transactions"]')?.getAttribute('aria-selected')).toBe('false');
  });

  it('reports the tab a reader picks', async() => {
    const onValueChange = vi.fn();
    const { container } = render(
      <Provider forcedTheme={ appearance } enableSystem={ false }>
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
      <Provider forcedTheme={ appearance } enableSystem={ false }>
        <ScanSectionTabs items={ items } value="transactions" onValueChange={ noop } rightSlot={ <span>Download Page Data</span> }/>
      </Provider>,
    );

    expect(container.querySelector('[data-right-slot]')?.textContent).toBe('Download Page Data');
  });

  it('lets a pill grow with its label instead of holding a height that clips it', () => {
    const { container } = render(
      <Provider forcedTheme={ appearance } enableSystem={ false }>
        <ScanSectionTabs items={ items } value="transactions" onValueChange={ noop }/>
      </Provider>,
    );

    const pill = container.querySelector('[data-tab="transfers"]') as Element;
    expect(document.documentElement.classList.contains(appearance)).toBe(true);
    const declarations = declarationsOf(pill);

    expect(declarations).toMatch(/(?:^|;)height:auto/);
    expect(declarations).not.toMatch(/(?:^|;)height:(?!auto(?:;|$))[^;]+/);
    expect(declarations).not.toMatch(/(?:^|;)max-height:/);
    expect(declarations).toContain(`line-height:${ pillLineHeight('sm') }`);
    expect(declarations).toMatch(/padding-(?:block|top):var\(--chakra-spacing-1\)/);
    expect(declarations).toMatch(/padding-(?:block|bottom):var\(--chakra-spacing-1\)/);
    expect(declarations).toContain('border-width:1px');
    expect(declarations).toContain('min-height:var(--tabs-height)');
    expect(declarations).toContain('white-space:nowrap');
    expect(declarations).not.toMatch(/(?:^|;)height:[123]?\dpx/);
  });

  it('gives way on the title and never on the count', () => {
    const { container } = render(
      <Provider forcedTheme={ appearance } enableSystem={ false }>
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
    expect(declarationsOf(title)).toContain('white-space:nowrap');
    expect(declarationsOf(title)).toContain('overflow:hidden');
    expect(declarationsOf(count)).toContain('flex-shrink:0');
  });

  it('bounds a long title inside its strip while preserving its count', () => {
    const title = 'Token transfers with a deliberately long section title';
    const { container } = render(
      <Provider forcedTheme={ appearance } enableSystem={ false }>
        <ScanSectionTabs items={ [ { id: 'long', title, count: 1234 } ] } value="long" onValueChange={ noop }/>
      </Provider>,
    );
    const pill = container.querySelector('[data-tab="long"]') as Element;
    const strip = container.querySelector('[data-scope="tabs"][data-part="root"]') as Element;
    const list = container.querySelector('[role="tablist"]') as Element;
    const row = container.querySelector('[data-scan-section-tabs]') as Element;

    expect(declarationsOf(pill)).toContain('max-width:100%');
    expect(declarationsOf(pill)).toContain('min-width:0');
    expect(declarationsOf(strip)).toContain('max-width:100%');
    expect(declarationsOf(strip)).toContain('min-width:0');
    expect(declarationsOf(list)).toContain('flex-wrap:wrap');
    expect(declarationsOf(row)).toContain('width:100%');
    expect(pill.querySelector('[data-tab-title]')?.textContent).toBe(title);
    expect(pill.querySelector('[data-count]')?.textContent).toBe('(1,234)');
    expect(declarationsOf(pill.querySelector('[data-count]') as Element)).toContain('flex-shrink:0');
  });
});
