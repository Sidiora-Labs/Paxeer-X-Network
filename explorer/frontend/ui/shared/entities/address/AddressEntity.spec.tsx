// @vitest-environment jsdom

import React from 'react';

import * as addressMock from 'mocks/address/address';
import { Provider } from 'toolkit/chakra/provider';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render } from 'vitest/lib';

import AddressEntity from './AddressEntity';

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

describe('AddressEntity', () => {
  it('names the kind of entity the row carries', () => {
    const { container } = render(
      <Provider>
        <AddressEntity address={ addressMock.withoutName }/>
      </Provider>,
    );

    expect(container.querySelector('[data-entity-kind="address"]')).not.toBeNull();
  });

  it('leads to the address through its own anchor', () => {
    const { container } = render(
      <Provider>
        <AddressEntity address={ addressMock.withoutName }/>
      </Provider>,
    );

    expect(container.querySelector('[data-entity-link]')?.getAttribute('href')?.endsWith(`/address/${ addressMock.withoutName.hash }`)).toBe(true);
  });

  it('shows the whole hash when nothing is truncated', () => {
    const { container } = render(
      <Provider>
        <AddressEntity address={ addressMock.withoutName } truncation="none"/>
      </Provider>,
    );

    expect(container.querySelector('[data-entity-content]')?.textContent).toBe(addressMock.withoutName.hash);
  });

  it('shows the name of a named address through the same content hook', () => {
    const { container } = render(
      <Provider>
        <AddressEntity address={ addressMock.withName }/>
      </Provider>,
    );

    expect(container.querySelector('[data-entity-content]')?.textContent).toBe(addressMock.withName.name);
  });

  it('closes the row with the copy control unless the caller drops it', () => {
    const { container, rerender } = render(
      <Provider>
        <AddressEntity address={ addressMock.withoutName }/>
      </Provider>,
    );

    expect(container.querySelector('[data-entity-kind="address"]')?.lastElementChild?.getAttribute('aria-label')).toBe('copy');

    rerender(
      <Provider>
        <AddressEntity address={ addressMock.withoutName } noCopy/>
      </Provider>,
    );

    expect(container.querySelector('[aria-label="copy"]')).toBeNull();
  });
});
