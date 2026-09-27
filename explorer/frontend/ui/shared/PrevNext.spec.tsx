// @vitest-environment jsdom

import React from 'react';

import { Provider } from 'toolkit/chakra/provider';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render } from 'vitest/lib';

import PrevNext from './PrevNext';

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

describe('PrevNext', () => {
  it('offers the two steps in reading order', () => {
    const onClick = vi.fn();
    const { container } = render(
      <Provider>
        <PrevNext onClick={ onClick }/>
      </Provider>,
    );

    const controls = Array.from(container.querySelectorAll('[data-control]'));

    expect(container.querySelector('[data-prev-next]')).not.toBeNull();
    expect(controls.map((control) => control.getAttribute('data-control'))).toEqual([ 'prev', 'next' ]);
  });

  it('reports the direction a reader picks', () => {
    const onClick = vi.fn();
    const { container } = render(
      <Provider>
        <PrevNext onClick={ onClick }/>
      </Provider>,
    );

    fireEvent.click(container.querySelector('[data-control="prev"]') as Element);
    fireEvent.click(container.querySelector('[data-control="next"]') as Element);

    expect(onClick).toHaveBeenNthCalledWith(1, 'prev');
    expect(onClick).toHaveBeenNthCalledWith(2, 'next');
  });

  it('closes a step the caller says is not there', () => {
    const onClick = vi.fn();
    const { container } = render(
      <Provider>
        <PrevNext onClick={ onClick } isNextDisabled/>
      </Provider>,
    );

    expect(container.querySelector('[data-control="next"]')?.hasAttribute('disabled')).toBe(true);
    expect(container.querySelector('[data-control="prev"]')?.hasAttribute('disabled')).toBe(false);
  });

  it('stands in with two placeholders while the neighbours load', () => {
    const onClick = vi.fn();
    const { container } = render(
      <Provider>
        <PrevNext onClick={ onClick } isLoading/>
      </Provider>,
    );

    expect(container.querySelectorAll('[data-control]')).toHaveLength(0);
    expect(container.querySelectorAll('[data-loading]')).toHaveLength(2);
  });
});
