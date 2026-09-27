// @vitest-environment jsdom

import React from 'react';

import { Provider } from 'toolkit/chakra/provider';
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

describe('StatusTag', () => {
  it('names the status it carries and capitalises the text', () => {
    const { container } = render(
      <Provider>
        <StatusTag type="ok" text="success"/>
      </Provider>,
    );

    const tag = container.querySelector('[data-status="ok"]');

    expect(tag).not.toBeNull();
    expect(tag?.textContent).toBe('Success');
  });

  it('marks a failed status apart from a pending one', () => {
    const { container, rerender } = render(
      <Provider>
        <StatusTag type="error" text="failed"/>
      </Provider>,
    );

    expect(container.querySelector('[data-status="error"]')?.textContent).toBe('Failed');

    rerender(
      <Provider>
        <StatusTag type="pending" text="pending"/>
      </Provider>,
    );

    expect(container.querySelector('[data-status="pending"]')?.textContent).toBe('Pending');
  });

  it('keeps the icon alone in the compact mode a table row uses', () => {
    const { container } = render(
      <Provider>
        <StatusTag type="ok" text="success" mode="compact"/>
      </Provider>,
    );

    const tag = container.querySelector('[data-status="ok"]');

    expect(tag?.textContent).toBe('');
    expect(tag?.querySelector('use')?.getAttribute('href')?.endsWith('#status/success')).toBe(true);
  });

  it('carries the icon alone when there is no text to show', () => {
    const { container } = render(
      <Provider>
        <StatusTag type="error"/>
      </Provider>,
    );

    expect(container.querySelector('[data-status="error"]')?.querySelector('use')?.getAttribute('href')?.endsWith('#status/error')).toBe(true);
  });
});
