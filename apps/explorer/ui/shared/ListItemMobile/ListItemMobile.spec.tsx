// @vitest-environment jsdom

import React from 'react';

import { Provider } from 'toolkit/chakra/provider';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render } from 'vitest/lib';

import ListItemMobile from './ListItemMobile';

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

describe('ListItemMobile', () => {
  it('marks the row so the card around it can give it a gutter', () => {
    const { container } = render(
      <Provider>
        <ListItemMobile>
          <span>row</span>
        </ListItemMobile>
      </Provider>,
    );

    const row = container.querySelector('[data-list-item-mobile]');

    expect(row).not.toBeNull();
    expect(row?.textContent).toBe('row');
  });

  it('stacks the fields a list row passes it in the order they are given', () => {
    const { container } = render(
      <Provider>
        <ListItemMobile rowGap={ 3 } py={ 3 }>
          <span data-field="hash">0x12</span>
          <span data-field="age">1s ago</span>
        </ListItemMobile>
      </Provider>,
    );

    const fields = Array.from(container.querySelectorAll('[data-list-item-mobile] [data-field]'));

    expect(fields.map((field) => field.getAttribute('data-field'))).toEqual([ 'hash', 'age' ]);
  });
});
