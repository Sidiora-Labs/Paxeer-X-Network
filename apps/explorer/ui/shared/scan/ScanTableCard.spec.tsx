// @vitest-environment jsdom

import React from 'react';

import { Provider } from 'toolkit/chakra/provider';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render } from 'vitest/lib';

import ScanTableCard, { formatScanTableCount } from './ScanTableCard';

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

describe('formatScanTableCount', () => {
  it('reads "More than" for a count the indexer has not finished', () => {
    expect(formatScanTableCount({ kind: 'more_than', value: 1234567, itemsName: 'transactions' }))
      .toBe('More than 1,234,567 transactions found');
  });

  it('reads "A total of" for a settled count', () => {
    expect(formatScanTableCount({ kind: 'total', value: 4321, itemsName: 'blocks' }))
      .toBe('A total of 4,321 blocks found');
  });

  it('reads the shown slice against the total for a capped list', () => {
    expect(formatScanTableCount({ kind: 'latest', value: 1000000, itemsName: 'transfers', shownValue: 50 }))
      .toBe('Latest 50 from a total of 1,000,000 transfers');
  });
});

describe('ScanTableCard', () => {
  it('heads the card with the count line and the optional note', () => {
    const { container } = render(
      <Provider>
        <ScanTableCard title="A total of 4,321 blocks found" note="Showing the latest 50">
          <div>rows</div>
        </ScanTableCard>
      </Provider>,
    );

    const card = container.querySelector('[data-scan-table-card]');

    expect(card?.querySelector('[data-title]')?.textContent).toBe('A total of 4,321 blocks found');
    expect(card?.querySelector('[data-note]')?.textContent).toBe('Showing the latest 50');
    expect(card?.querySelector('[data-body]')?.textContent).toBe('rows');
  });

  it('puts the actions and the pagination in the header in that order', () => {
    const { container } = render(
      <Provider>
        <ScanTableCard
          title="A total of 4,321 blocks found"
          actions={ <button type="button">Download Page Data</button> }
          pagination={ <span>Page 1 of 9</span> }
        >
          <div>rows</div>
        </ScanTableCard>
      </Provider>,
    );

    const actions = container.querySelector('[data-actions]');

    expect(actions?.textContent).toBe('Download Page DataPage 1 of 9');
  });

  it('repeats the pagination beside the show-rows select in the footer', () => {
    const { container } = render(
      <Provider>
        <ScanTableCard
          title="A total of 4,321 blocks found"
          pagination={ <span>Page 1 of 9</span> }
          showRows={ <span>Show rows: 25</span> }
        >
          <div>rows</div>
        </ScanTableCard>
      </Provider>,
    );

    expect(container.querySelector('[data-footer-rows]')?.textContent).toBe('Show rows: 25');
    expect(container.querySelector('[data-footer-pagination]')?.textContent).toBe('Page 1 of 9');
  });

  it('holds the header pagination in a slot of its own so a narrow screen can leave the footer copy alone', () => {
    const { container } = render(
      <Provider>
        <ScanTableCard
          title="A total of 4,321 blocks found"
          pagination={ <span>Page 1 of 9</span> }
          showRows={ <span>Show rows: 25</span> }
        >
          <div>rows</div>
        </ScanTableCard>
      </Provider>,
    );

    const headerPagination = container.querySelector('[data-actions] [data-header-pagination]');

    expect(headerPagination?.textContent).toBe('Page 1 of 9');
    expect(container.querySelector('[data-footer-pagination]')?.textContent).toBe('Page 1 of 9');
  });

  it('leaves the header pagination slot out when the list is not paginated', () => {
    const { container } = render(
      <Provider>
        <ScanTableCard title="A total of 4,321 blocks found" showRows={ <span>Show rows: 25</span> }>
          <div>rows</div>
        </ScanTableCard>
      </Provider>,
    );

    expect(container.querySelector('[data-header-pagination]')).toBeNull();
  });

  it('leaves the footer out when there is nothing to put in it', () => {
    const { container } = render(
      <Provider>
        <ScanTableCard title="A total of 4,321 blocks found">
          <div>rows</div>
        </ScanTableCard>
      </Provider>,
    );

    expect(container.querySelector('[data-footer]')).toBeNull();
  });
});
