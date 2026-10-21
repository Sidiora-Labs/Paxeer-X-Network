// @vitest-environment jsdom

import React from 'react';

import { tokenInfo } from 'mocks/tokens/tokenInfo';
import { Provider } from 'toolkit/chakra/provider';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render } from 'vitest/lib';

import TokenEntity from './TokenEntity';

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

describe('TokenEntity', () => {
  it('names the kind of entity the row carries', () => {
    const { container } = render(
      <Provider>
        <TokenEntity token={ tokenInfo }/>
      </Provider>,
    );

    expect(container.querySelector('[data-entity-kind="token"]')).not.toBeNull();
  });

  it('sets the symbol beside the name', () => {
    const { container } = render(
      <Provider>
        <TokenEntity token={ tokenInfo }/>
      </Provider>,
    );

    expect(container.querySelector('[data-entity-content]')?.textContent).toBe(tokenInfo.name);
    expect(container.querySelector('[data-token-symbol]')?.textContent).toBe(`(${ tokenInfo.symbol })`);
  });

  it('joins the symbol into the name when the caller asks for one string', () => {
    const { container } = render(
      <Provider>
        <TokenEntity token={ tokenInfo } jointSymbol/>
      </Provider>,
    );

    expect(container.querySelector('[data-entity-content]')?.textContent).toBe(`${ tokenInfo.name } (${ tokenInfo.symbol })`);
    expect(container.querySelector('[data-token-symbol]')).toBeNull();
  });

  it('leads to the token through its own anchor', () => {
    const { container } = render(
      <Provider>
        <TokenEntity token={ tokenInfo }/>
      </Provider>,
    );

    expect(container.querySelector('[data-entity-link]')?.getAttribute('href')?.endsWith(`/token/${ tokenInfo.address_hash }`)).toBe(true);
  });
});
