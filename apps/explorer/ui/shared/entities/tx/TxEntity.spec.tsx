// @vitest-environment jsdom

import React from 'react';

import { Provider } from 'toolkit/chakra/provider';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render } from 'vitest/lib';

import TxEntity from './TxEntity';

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

// jsdom implements no layout observation, and the hash the row renders measures itself against its box
class ResizeObservation implements ResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

beforeAll(() => {
  Object.defineProperty(window, 'matchMedia', { writable: true, value: createMediaQueryList });
  Object.defineProperty(window, 'ResizeObserver', { writable: true, value: ResizeObservation });
});

// the suite runs without vitest globals, so the testing library cannot register its own teardown
afterEach(cleanup);

const HASH = '0x9e2e3a1d1ca9d8cbcb0f0b5c8a6bd0e1f3c2a4b5c6d7e8f90a1b2c3d4e5f6a7b';

describe('TxEntity', () => {
  it('names the kind of entity the row carries', () => {
    const { container } = render(
      <Provider>
        <TxEntity hash={ HASH }/>
      </Provider>,
    );

    expect(container.querySelector('[data-entity-kind="tx"]')).not.toBeNull();
  });

  it('leads to the transaction through its own anchor', () => {
    const { container } = render(
      <Provider>
        <TxEntity hash={ HASH }/>
      </Provider>,
    );

    expect(container.querySelector('[data-entity-link]')?.getAttribute('href')?.endsWith(`/tx/${ HASH }`)).toBe(true);
  });

  it('keeps the icon, the value and the copy control in that order', () => {
    const { container } = render(
      <Provider>
        <TxEntity hash={ HASH } truncation="none"/>
      </Provider>,
    );

    const row = container.querySelector('[data-entity-kind="tx"]');

    expect(row?.querySelector('[data-entity-content]')?.textContent).toBe(HASH);
    expect(row?.lastElementChild?.getAttribute('aria-label')).toBe('copy');
  });

  it('drops the copy control when the caller asks for none', () => {
    const { container } = render(
      <Provider>
        <TxEntity hash={ HASH } noCopy/>
      </Provider>,
    );

    expect(container.querySelector('[aria-label="copy"]')).toBeNull();
  });

  it('shows the text a caller gives instead of the hash', () => {
    const { container } = render(
      <Provider>
        <TxEntity hash={ HASH } text="Latest transaction" truncation="none"/>
      </Provider>,
    );

    expect(container.querySelector('[data-entity-content]')?.textContent).toBe('Latest transaction');
  });
});
