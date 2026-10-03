// @vitest-environment jsdom

import React from 'react';

import { Provider } from 'toolkit/chakra/provider';
import { pillLineHeight } from 'toolkit/theme/recipes/pillSizing';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render } from 'vitest/lib';

import ScanMethodChip from './ScanMethodChip';

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

describe.each([ 'light', 'dark' ] as const)('ScanMethodChip in %s appearance', (appearance) => {
  it('carries the method name as its text and as its hook', () => {
    const { container } = render(
      <Provider forcedTheme={ appearance } enableSystem={ false }>
        <ScanMethodChip method="transfer"/>
      </Provider>,
    );

    const chip = container.querySelector('[data-scan-method="transfer"]');

    expect(chip).not.toBeNull();
    expect(chip?.textContent).toBe('transfer');
  });

  it('renders a bare selector the same way', () => {
    const { container } = render(
      <Provider forcedTheme={ appearance } enableSystem={ false }>
        <ScanMethodChip method="0xa9059cbb"/>
      </Provider>,
    );

    expect(container.querySelector('[data-scan-method="0xa9059cbb"]')?.textContent).toBe('0xa9059cbb');
  });

  it('stands as tall as its label, its padding and its border', () => {
    const { container } = render(
      <Provider forcedTheme={ appearance } enableSystem={ false }>
        <ScanMethodChip method="transfer"/>
      </Provider>,
    );

    const chip = container.querySelector('[data-scan-method="transfer"]') as Element;
    expect(document.documentElement.classList.contains(appearance)).toBe(true);
    const declarations = declarationsOf(chip);

    // the chip reads 14px text on a 20px line box, with 2px above and below it and a 1px border on each edge
    expect(declarations).toContain('min-height:26px');
    expect(declarations).toMatch(/padding-(?:block|top):2px/);
    expect(declarations).toMatch(/padding-(?:block|bottom):2px/);
    expect(declarations).toContain('border-width:1px');
    expect(declarations).toMatch(/(?:^|;)height:auto/);
    expect(declarations).not.toMatch(/(?:^|;)height:(?!auto(?:;|$))[^;]+/);
    expect(declarations).not.toMatch(/(?:^|;)max-height:/);
    expect(declarations).not.toMatch(/(?:^|;)height:[123]?\dpx/);
    expect(declarationsOf(chip.firstElementChild as Element)).toContain(`line-height:${ pillLineHeight('sm') }`);
  });

  it('truncates a long method to one line inside the chip', () => {
    const { container } = render(
      <Provider forcedTheme={ appearance } enableSystem={ false }>
        <ScanMethodChip method="setApprovalForAllWithDeadlineAndSignature"/>
      </Provider>,
    );

    const chip = container.querySelector('[data-scan-method="setApprovalForAllWithDeadlineAndSignature"]') as Element;
    const label = chip.firstElementChild as Element;
    const labelDeclarations = declarationsOf(label);

    expect(label.textContent).toBe('setApprovalForAllWithDeadlineAndSignature');
    expect(labelDeclarations).toContain('min-width:0');
    expect(labelDeclarations).toContain('text-overflow:ellipsis');
    expect(labelDeclarations).toContain('white-space:nowrap');
    expect(labelDeclarations).toContain('overflow:hidden');
    expect(declarationsOf(chip)).toContain('max-width:100%');
  });
});
