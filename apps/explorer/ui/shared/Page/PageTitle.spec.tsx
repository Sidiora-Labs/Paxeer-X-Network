// @vitest-environment jsdom

import React from 'react';

import { Provider } from 'toolkit/chakra/provider';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render } from 'vitest/lib';

import PageTitle from './PageTitle';

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

describe('PageTitle', () => {
  it('keeps the icon, the heading and the identifier in one wrapping row', () => {
    const { container } = render(
      <Provider>
        <PageTitle
          title="Address"
          beforeTitle={ <span data-icon>icon</span> }
          afterTitle={ <span>0x1255d84066f579E7B7A3df4296e960d59fc05b32</span> }
        />
      </Provider>,
    );

    const row = container.querySelector('[data-title-row]');

    expect(row).not.toBeNull();
    expect(row?.querySelector('[data-icon]')).not.toBeNull();
    expect(row?.textContent).toContain('Address');
  });

  it('gives what follows the heading its own slot so it can drop onto the next line', () => {
    const { container } = render(
      <Provider>
        <PageTitle title="Unified account" afterTitle={ <span>0x1255d84066f579E7B7A3df4296e960d59fc05b32</span> }/>
      </Provider>,
    );

    const slot = container.querySelector('[data-title-row] [data-title-after]');

    expect(slot?.textContent).toBe('0x1255d84066f579E7B7A3df4296e960d59fc05b32');
  });

  it('leaves the slot out when nothing follows the heading', () => {
    const { container } = render(
      <Provider>
        <PageTitle title="Transactions"/>
      </Provider>,
    );

    expect(container.querySelector('[data-title-after]')).toBeNull();
    expect(container.querySelector('[data-title-row]')?.textContent).toContain('Transactions');
  });

  it('holds the second row in a row of its own that wraps', () => {
    const { container } = render(
      <Provider>
        <PageTitle title="Block" secondRow={ <span>Validator 0x88Ca6ad07Cc8b9b0c09b18A6E</span> }/>
      </Provider>,
    );

    const secondRow = container.querySelector('[data-title-second-row]');

    expect(secondRow?.textContent).toContain('Validator 0x88Ca6ad07Cc8b9b0c09b18A6E');
  });
});
