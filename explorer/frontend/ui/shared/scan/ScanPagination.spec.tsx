// @vitest-environment jsdom

import React from 'react';

import { Provider } from 'toolkit/chakra/provider';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render } from 'vitest/lib';

import ScanPagination from './ScanPagination';

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

const params = () => ({
  page: 2,
  onNextPageClick: vi.fn(),
  onPrevPageClick: vi.fn(),
  resetPage: vi.fn(),
  hasPages: true,
  hasNextPage: true,
  canGoBackwards: true,
  isLoading: false,
  isVisible: true,
});

describe('ScanPagination', () => {
  it('lays the five controls out in reading order', () => {
    const { container } = render(
      <Provider>
        <ScanPagination { ...params() } pageCount={ 10 }/>
      </Provider>,
    );

    const controls = Array.from(container.querySelectorAll('[data-control]'));

    expect(controls.map((control) => control.getAttribute('data-control'))).toEqual([ 'first', 'prev', 'page', 'next', 'last' ]);
    expect(controls[0].textContent).toBe('First');
    expect(controls[4].textContent).toBe('Last');
  });

  it('counts the page against the total when the total is known', () => {
    const { container } = render(
      <Provider>
        <ScanPagination { ...params() } pageCount={ 10 }/>
      </Provider>,
    );

    expect(container.querySelector('[data-control="page"]')?.textContent).toBe('Page 2 of 10');
  });

  it('names the page alone when the total is unknown', () => {
    const { container } = render(
      <Provider>
        <ScanPagination { ...params() }/>
      </Provider>,
    );

    expect(container.querySelector('[data-control="page"]')?.textContent).toBe('Page 2');
  });

  it('asks the hook to move when a direction is clicked', () => {
    const props = params();
    const { container } = render(
      <Provider>
        <ScanPagination { ...props } pageCount={ 10 }/>
      </Provider>,
    );

    fireEvent.click(container.querySelector('[data-control="next"]') as Element);
    fireEvent.click(container.querySelector('[data-control="prev"]') as Element);
    fireEvent.click(container.querySelector('[data-control="first"]') as Element);

    expect(props.onNextPageClick).toHaveBeenCalledTimes(1);
    expect(props.onPrevPageClick).toHaveBeenCalledTimes(1);
    expect(props.resetPage).toHaveBeenCalledTimes(1);
  });

  it('disables the last page control until a jump is wired up', () => {
    const props = params();
    const onLastPageClick = vi.fn();
    const { container, rerender } = render(
      <Provider>
        <ScanPagination { ...props } pageCount={ 10 }/>
      </Provider>,
    );

    expect(container.querySelector('[data-control="last"]')?.hasAttribute('disabled')).toBe(true);

    rerender(
      <Provider>
        <ScanPagination { ...props } pageCount={ 10 } onLastPageClick={ onLastPageClick }/>
      </Provider>,
    );

    fireEvent.click(container.querySelector('[data-control="last"]') as Element);

    expect(onLastPageClick).toHaveBeenCalledTimes(1);
  });

  it('renders nothing while the list is not paginated', () => {
    const { container } = render(
      <Provider>
        <ScanPagination { ...params() } isVisible={ false }/>
      </Provider>,
    );

    expect(container.querySelector('[data-scan-pagination]')).toBeNull();
  });
});
