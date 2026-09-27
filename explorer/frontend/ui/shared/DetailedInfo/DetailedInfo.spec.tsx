// @vitest-environment jsdom

import React from 'react';

import { Provider } from 'toolkit/chakra/provider';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render } from 'vitest/lib';

import * as DetailedInfo from './DetailedInfo';

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

describe('DetailedInfo', () => {
  it('marks a value as the one that stacks under its label and wraps on a narrow screen', () => {
    const { container } = render(
      <Provider>
        <DetailedInfo.Container>
          <DetailedInfo.ItemLabel>Timestamp</DetailedInfo.ItemLabel>
          <DetailedInfo.ItemValue>Sep 27 2026 19:16:19 (+02:00 UTC)</DetailedInfo.ItemValue>
        </DetailedInfo.Container>
      </Provider>,
    );

    const value = container.querySelector('[data-detailed-info-value]');

    expect(value?.getAttribute('data-stack-below')).toBe('lg');
    expect(value?.textContent).toBe('Sep 27 2026 19:16:19 (+02:00 UTC)');
  });

  it('marks the grid so a page can style the detail block as a whole', () => {
    const { container } = render(
      <Provider>
        <DetailedInfo.Container>
          <DetailedInfo.ItemLabel>Transaction hash</DetailedInfo.ItemLabel>
          <DetailedInfo.ItemValue>0x01</DetailedInfo.ItemValue>
        </DetailedInfo.Container>
      </Provider>,
    );

    expect(container.querySelector('[data-detailed-info]')).not.toBeNull();
  });

  it('pairs every label with the value that follows it', () => {
    const { container } = render(
      <Provider>
        <DetailedInfo.Container>
          <DetailedInfo.ItemLabel>Transaction hash</DetailedInfo.ItemLabel>
          <DetailedInfo.ItemValue>0x01</DetailedInfo.ItemValue>
          <DetailedInfo.ItemLabel>Status</DetailedInfo.ItemLabel>
          <DetailedInfo.ItemValue>Success</DetailedInfo.ItemValue>
        </DetailedInfo.Container>
      </Provider>,
    );

    const labels = Array.from(container.querySelectorAll('[data-detailed-info-label]'));
    const values = Array.from(container.querySelectorAll('[data-detailed-info-value]'));

    expect(labels.map((label) => label.textContent)).toEqual([ 'Transaction hash', 'Status' ]);
    expect(values.map((value) => value.textContent)).toEqual([ '0x01', 'Success' ]);
  });

  it('explains a label through a hint when one is given', () => {
    const { container } = render(
      <Provider>
        <DetailedInfo.Container>
          <DetailedInfo.ItemLabel hint="The unique identifier of this transaction.">Transaction hash</DetailedInfo.ItemLabel>
          <DetailedInfo.ItemValue>0x01</DetailedInfo.ItemValue>
        </DetailedInfo.Container>
      </Provider>,
    );

    expect(container.querySelector('[data-detailed-info-label] [aria-label="hint"]')).not.toBeNull();
  });

  it('keeps the row height the detail pages share', () => {
    expect(DetailedInfo.ITEM_VALUE_LINE_HEIGHT).toEqual({ base: '30px', lg: '32px' });
  });

  it('spans the divider across both columns', () => {
    const { container } = render(
      <Provider>
        <DetailedInfo.Container>
          <DetailedInfo.ItemLabel>Status</DetailedInfo.ItemLabel>
          <DetailedInfo.ItemValue>Success</DetailedInfo.ItemValue>
          <DetailedInfo.ItemDivider data-divider/>
        </DetailedInfo.Container>
      </Provider>,
    );

    expect(container.querySelector('[data-divider]')).not.toBeNull();
  });
});
