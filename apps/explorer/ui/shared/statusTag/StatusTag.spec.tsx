// @vitest-environment jsdom

import React from 'react';

import { Provider } from 'toolkit/chakra/provider';
import { pillLineHeight } from 'toolkit/theme/recipes/pillSizing';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render } from 'vitest/lib';

import StatusTag from './StatusTag';

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

describe.each([ 'light', 'dark' ] as const)('StatusTag in %s appearance', (appearance) => {
  it('names the status it carries and capitalises the text', () => {
    const { container } = render(
      <Provider forcedTheme={ appearance } enableSystem={ false }>
        <StatusTag type="ok" text="success"/>
      </Provider>,
    );

    const tag = container.querySelector('[data-status="ok"]');

    expect(tag).not.toBeNull();
    expect(tag?.textContent).toBe('Success');
  });

  it('marks a failed status apart from a pending one', () => {
    const { container, rerender } = render(
      <Provider forcedTheme={ appearance } enableSystem={ false }>
        <StatusTag type="error" text="failed"/>
      </Provider>,
    );

    expect(container.querySelector('[data-status="error"]')?.textContent).toBe('Failed');

    rerender(
      <Provider forcedTheme={ appearance } enableSystem={ false }>
        <StatusTag type="pending" text="pending"/>
      </Provider>,
    );

    expect(container.querySelector('[data-status="pending"]')?.textContent).toBe('Pending');
  });

  it('keeps the icon alone in the compact mode a table row uses', () => {
    const { container } = render(
      <Provider forcedTheme={ appearance } enableSystem={ false }>
        <StatusTag type="ok" text="success" mode="compact"/>
      </Provider>,
    );

    const tag = container.querySelector('[data-status="ok"]');

    expect(tag?.textContent).toBe('');
    expect(tag?.querySelector('use')?.getAttribute('href')?.endsWith('#status/success')).toBe(true);
  });

  it('carries the icon alone when there is no text to show', () => {
    const { container } = render(
      <Provider forcedTheme={ appearance } enableSystem={ false }>
        <StatusTag type="error"/>
      </Provider>,
    );

    expect(container.querySelector('[data-status="error"]')?.querySelector('use')?.getAttribute('href')?.endsWith('#status/error')).toBe(true);
  });

  it('stands as tall as its own line box and its padding on a title row', () => {
    const { container } = render(
      <Provider forcedTheme={ appearance } enableSystem={ false }>
        <StatusTag type="ok" text="success"/>
      </Provider>,
    );

    const tag = container.querySelector('[data-status="ok"]') as Element;
    expect(document.documentElement.classList.contains(appearance)).toBe(true);
    const declarations = declarationsOf(tag);

    // the chip reads 14px text on a 20px line box, with 2px above and below it
    expect(declarations).toContain(`line-height:${ pillLineHeight('sm') }`);
    expect(declarations).toMatch(/padding-(?:block|top):2px/);
    expect(declarations).toMatch(/padding-(?:block|bottom):2px/);
    expect(declarations).toContain('min-height:24px');
    expect(declarations).toMatch(/(?:^|;)height:auto/);
    expect(declarations).not.toMatch(/(?:^|;)height:(?!auto(?:;|$))[^;]+/);
    expect(declarations).not.toMatch(/(?:^|;)max-height:/);
    expect(declarations).not.toMatch(/(?:^|;)height:[123]?\dpx/);
  });

  it('holds the small chip a compact row uses on its own line box too', () => {
    const { container } = render(
      <Provider forcedTheme={ appearance } enableSystem={ false }>
        <StatusTag type="pending" text="pending" size="sm"/>
      </Provider>,
    );

    const tag = container.querySelector('[data-status="pending"]') as Element;
    expect(document.documentElement.classList.contains(appearance)).toBe(true);
    const declarations = declarationsOf(tag);

    // the small chip reads 12px text on a 16px line box, which the old fixed 18px height cut into
    expect(declarations).toContain(`line-height:${ pillLineHeight('xs') }`);
    expect(declarations).toContain('min-height:20px');
    expect(declarations).toMatch(/padding-(?:block|top):2px/);
    expect(declarations).toMatch(/padding-(?:block|bottom):2px/);
    expect(declarations).toMatch(/(?:^|;)height:auto/);
    expect(declarations).not.toMatch(/(?:^|;)height:(?!auto(?:;|$))[^;]+/);
    expect(declarations).not.toMatch(/(?:^|;)max-height:/);
    expect(declarations).not.toMatch(/(?:^|;)height:1?\dpx/);
  });

  it('keeps the status text on one line and the status icon at its own size', () => {
    const { container } = render(
      <Provider forcedTheme={ appearance } enableSystem={ false }>
        <StatusTag type="error" text="failed"/>
      </Provider>,
    );

    const tag = container.querySelector('[data-status="error"]') as Element;

    expect(declarationsOf(tag)).toContain('white-space:nowrap');
    expect(declarationsOf(tag, '>span')).toContain('min-width:0');
    expect(declarationsOf(tag, '>span')).toContain('text-overflow:ellipsis');
    expect(declarationsOf(tag, '>span')).toContain('white-space:nowrap');
    expect(declarationsOf(tag, '>span')).toContain('overflow:hidden');
    expect(declarationsOf(tag, '>svg')).toContain('flex-shrink:0');
  });
});
