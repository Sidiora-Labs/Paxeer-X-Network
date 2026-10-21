// @vitest-environment jsdom

import React from 'react';

import { Provider } from 'toolkit/chakra/provider';
import { pillLineHeight } from 'toolkit/theme/recipes/pillSizing';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render } from 'vitest/lib';

import ScanDirectionBadge from './ScanDirectionBadge';

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

describe.each([ 'light', 'dark' ] as const)('ScanDirectionBadge in %s appearance', (appearance) => {
  it('reads IN for an incoming transfer', () => {
    const { container } = render(
      <Provider forcedTheme={ appearance } enableSystem={ false }>
        <ScanDirectionBadge direction="in"/>
      </Provider>,
    );

    const badge = container.querySelector('[data-direction="in"]');

    expect(badge).not.toBeNull();
    expect(badge?.textContent).toBe('IN');
  });

  it('reads OUT for an outgoing transfer', () => {
    const { container } = render(
      <Provider forcedTheme={ appearance } enableSystem={ false }>
        <ScanDirectionBadge direction="out"/>
      </Provider>,
    );

    const badge = container.querySelector('[data-direction="out"]');

    expect(badge).not.toBeNull();
    expect(badge?.textContent).toBe('OUT');
  });

  it('stands as tall as its own line box and its padding, with no height its text can outgrow', () => {
    const { container } = render(
      <Provider forcedTheme={ appearance } enableSystem={ false }>
        <ScanDirectionBadge direction="out"/>
      </Provider>,
    );

    const badge = container.querySelector('[data-direction="out"]');
    expect(document.documentElement.classList.contains(appearance)).toBe(true);
    const declarations = declarationsOf(badge as Element);

    // the small badge reads 12px text on a 16px line box, with 2px above and below it
    expect(declarations).toContain(`line-height:${ pillLineHeight('xs') }`);
    expect(declarations).toMatch(/padding-(?:block|top):2px/);
    expect(declarations).toMatch(/padding-(?:block|bottom):2px/);
    expect(declarations).toContain('min-height:20px');
    expect(declarations).toMatch(/(?:^|;)height:auto/);
    expect(declarations).not.toMatch(/(?:^|;)height:(?!auto(?:;|$))[^;]+/);
    expect(declarations).not.toMatch(/(?:^|;)max-height:/);
    expect(declarations).not.toMatch(/(?:^|;)height:1?\dpx/);
  });

  it('keeps IN and OUT on one line and lets only the label give way', () => {
    const { container } = render(
      <Provider forcedTheme={ appearance } enableSystem={ false }>
        <ScanDirectionBadge direction="in"/>
      </Provider>,
    );

    const badge = container.querySelector('[data-direction="in"]') as Element;

    expect(declarationsOf(badge)).toContain('white-space:nowrap');
    expect(declarationsOf(badge)).toContain('overflow:hidden');
    expect(declarationsOf(badge, '>span')).toContain('min-width:0');
    expect(declarationsOf(badge, '>span')).toContain('text-overflow:ellipsis');
    expect(declarationsOf(badge, '>span')).toContain('white-space:nowrap');
    expect(declarationsOf(badge, '>span')).toContain('overflow:hidden');
    expect(declarationsOf(badge, '>svg')).toContain('flex-shrink:0');
    expect(badge.firstElementChild?.textContent).toBe('IN');
  });
});
