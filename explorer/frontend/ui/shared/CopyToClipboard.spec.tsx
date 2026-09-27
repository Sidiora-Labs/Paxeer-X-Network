// @vitest-environment jsdom

import React from 'react';

import { Provider } from 'toolkit/chakra/provider';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render } from 'vitest/lib';

import CopyToClipboard from './CopyToClipboard';

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

describe('CopyToClipboard', () => {
  it('offers one labelled control carrying the copy icon', () => {
    const { container } = render(
      <Provider>
        <CopyToClipboard text="0x01"/>
      </Provider>,
    );

    const button = container.querySelector('[aria-label="copy"]');

    expect(button).not.toBeNull();
    expect(button?.querySelector('use')?.getAttribute('href')?.endsWith('#copy')).toBe(true);
  });

  it('carries the link icon when it copies a link', () => {
    const { container } = render(
      <Provider>
        <CopyToClipboard text="https://example.test" type="link"/>
      </Provider>,
    );

    expect(container.querySelector('use')?.getAttribute('href')?.endsWith('#link')).toBe(true);
  });

  it('renders the bare control when the caller suppresses the tooltip', () => {
    const { container } = render(
      <Provider>
        <CopyToClipboard text="0x01" noTooltip/>
      </Provider>,
    );

    expect(container.querySelectorAll('[aria-label="copy"]')).toHaveLength(1);
  });

  it('stands in with a placeholder while the value it copies loads', () => {
    const { container } = render(
      <Provider>
        <CopyToClipboard text="0x01" isLoading/>
      </Provider>,
    );

    expect(container.querySelector('[data-loading-skeleton]')).not.toBeNull();
  });
});
