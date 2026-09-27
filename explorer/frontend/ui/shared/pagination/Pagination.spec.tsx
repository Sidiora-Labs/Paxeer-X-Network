// @vitest-environment jsdom

import React from 'react';

import { Provider } from 'toolkit/chakra/provider';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render } from 'vitest/lib';

import Pagination from './Pagination';

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
  page: 3,
  onNextPageClick: vi.fn(),
  onPrevPageClick: vi.fn(),
  resetPage: vi.fn(),
  hasPages: true,
  hasNextPage: true,
  canGoBackwards: true,
  isLoading: false,
  isVisible: true,
});

describe('Pagination', () => {
  it('lays the controls out in reading order', () => {
    const { container } = render(
      <Provider>
        <Pagination { ...params() }/>
      </Provider>,
    );

    const controls = Array.from(container.querySelectorAll('[data-control]'));

    expect(container.querySelector('[data-pagination]')).not.toBeNull();
    expect(controls.map((control) => control.getAttribute('data-control'))).toEqual([ 'first', 'prev', 'page', 'next', 'last' ]);
  });

  it('counts the page against the total when the total is known', () => {
    const { container } = render(
      <Provider>
        <Pagination { ...params() } pageCount={ 12 }/>
      </Provider>,
    );

    expect(container.querySelector('[data-control="page"]')?.textContent).toBe('Page 3 of 12');
  });

  it('names the page alone when the total is unknown', () => {
    const { container } = render(
      <Provider>
        <Pagination { ...params() }/>
      </Provider>,
    );

    expect(container.querySelector('[data-control="page"]')?.textContent).toBe('Page 3');
  });

  it('carries the show-rows select beside the controls', () => {
    const { container } = render(
      <Provider>
        <Pagination { ...params() } showRows={ <span data-rows>Show rows: 25</span> }/>
      </Provider>,
    );

    expect(container.querySelector('[data-pagination] [data-rows]')?.textContent).toBe('Show rows: 25');
  });

  it('asks the hook to move when a direction is clicked', () => {
    const props = params();
    const { container } = render(
      <Provider>
        <Pagination { ...props }/>
      </Provider>,
    );

    fireEvent.click(container.querySelector('[data-control="next"]') as Element);
    fireEvent.click(container.querySelector('[data-control="prev"]') as Element);
    fireEvent.click(container.querySelector('[data-control="first"]') as Element);

    expect(props.onNextPageClick).toHaveBeenCalledTimes(1);
    expect(props.onPrevPageClick).toHaveBeenCalledTimes(1);
    expect(props.resetPage).toHaveBeenCalledTimes(1);
  });

  it('closes the way back on the first page', () => {
    const { container } = render(
      <Provider>
        <Pagination { ...params() } page={ 1 }/>
      </Provider>,
    );

    expect(container.querySelector('[data-control="first"]')?.hasAttribute('disabled')).toBe(true);
    expect(container.querySelector('[data-control="prev"]')?.hasAttribute('disabled')).toBe(true);
  });

  it('renders nothing while the list is not paginated', () => {
    const { container } = render(
      <Provider>
        <Pagination { ...params() } isVisible={ false }/>
      </Provider>,
    );

    expect(container.querySelector('[data-pagination]')).toBeNull();
  });
});
