// @vitest-environment jsdom

import React from 'react';

import { Provider } from 'toolkit/chakra/provider';
import * as DetailedInfo from 'ui/shared/DetailedInfo/DetailedInfo';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render } from 'vitest/lib';

import ScanKeyValue from './ScanKeyValue';

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

describe('ScanKeyValue', () => {
  it('sets the label beside the value inside the detail grid', () => {
    const { container } = render(
      <Provider>
        <DetailedInfo.Container>
          <ScanKeyValue label="Block height">
            <span>25,712,900</span>
          </ScanKeyValue>
        </DetailedInfo.Container>
      </Provider>,
    );

    const grid = container.querySelector('[data-detailed-info]');

    expect(grid?.querySelector('[data-scan-key]')?.textContent).toBe('Block height');
    expect(grid?.querySelector('[data-scan-value]')?.textContent).toBe('25,712,900');
  });

  it('puts the label before the value in the document', () => {
    const { container } = render(
      <Provider>
        <DetailedInfo.Container>
          <ScanKeyValue label="Status">
            <span>Success</span>
          </ScanKeyValue>
        </DetailedInfo.Container>
      </Provider>,
    );

    const cells = Array.from(container.querySelectorAll('[data-scan-key], [data-scan-value]'));

    expect(cells[0].hasAttribute('data-scan-key')).toBe(true);
    expect(cells[1].hasAttribute('data-scan-value')).toBe(true);
  });

  it('explains the row through a hint when one is given', () => {
    const { container } = render(
      <Provider>
        <DetailedInfo.Container>
          <ScanKeyValue label="Nonce" hint="The number of transactions sent from this address.">
            <span>7</span>
          </ScanKeyValue>
        </DetailedInfo.Container>
      </Provider>,
    );

    expect(container.querySelector('[data-scan-key] [aria-label="hint"]')).not.toBeNull();
  });

  it('closes the row with a divider only when it is asked for one', () => {
    const { container, rerender } = render(
      <Provider>
        <DetailedInfo.Container>
          <ScanKeyValue label="Value">
            <span>1 PAX</span>
          </ScanKeyValue>
        </DetailedInfo.Container>
      </Provider>,
    );

    expect(container.querySelector('[data-scan-divider]')).toBeNull();

    rerender(
      <Provider>
        <DetailedInfo.Container>
          <ScanKeyValue label="Value" withDivider>
            <span>1 PAX</span>
          </ScanKeyValue>
        </DetailedInfo.Container>
      </Provider>,
    );

    expect(container.querySelector('[data-scan-divider]')).not.toBeNull();
  });
});
