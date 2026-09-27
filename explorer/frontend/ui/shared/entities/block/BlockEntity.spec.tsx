// @vitest-environment jsdom

import React from 'react';

import { Provider } from 'toolkit/chakra/provider';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render } from 'vitest/lib';

import BlockEntity from './BlockEntity';

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

describe('BlockEntity', () => {
  it('names the kind of entity the row carries', () => {
    const { container } = render(
      <Provider>
        <BlockEntity number={ 25712900 }/>
      </Provider>,
    );

    expect(container.querySelector('[data-entity-kind="block"]')).not.toBeNull();
  });

  it('keeps the whole height, because a height is short enough to read', () => {
    const { container } = render(
      <Provider>
        <BlockEntity number={ 25712900 }/>
      </Provider>,
    );

    expect(container.querySelector('[data-entity-content]')?.textContent).toBe('25712900');
  });

  it('leads to the block through its own anchor', () => {
    const { container } = render(
      <Provider>
        <BlockEntity number={ 25712900 }/>
      </Provider>,
    );

    expect(container.querySelector('[data-entity-link]')?.getAttribute('href')?.endsWith('/block/25712900')).toBe(true);
  });

  it('shortens the height only when a caller asks it to', () => {
    const { container } = render(
      <Provider>
        <BlockEntity number={ 257129001234 } truncation="constant"/>
      </Provider>,
    );

    expect(container.querySelector('[data-entity-content]')?.textContent).not.toBe('257129001234');
  });
});
