import type { RenderOptions } from '@testing-library/react';
import { cleanup, render as baseRender } from '@testing-library/react';
import type { NextRouter } from 'next/router';
import React from 'react';

import { Provider as ChakraProvider } from 'toolkit/chakra/provider';
import { afterEach } from 'vitest';
import { wrapper as TestApp } from 'vitest/lib';

// The shell is a desktop layout, and jsdom answers no media query on its own, so the specs render
// against a viewport of this width and every width query is answered from it.
export const VIEWPORT_WIDTH = 1440;

const ROOT_FONT_SIZE = 16;

const matchesQuery = (query: string) => {
  const widthConditions = Array.from(query.matchAll(/\((min|max)-width:\s*([\d.]+)(px|r?em)\)/g));

  if (widthConditions.length === 0) {
    return false;
  }

  return widthConditions.every(([ , bound, rawValue, unit ]) => {
    const value = Number(rawValue) * (unit === 'px' ? 1 : ROOT_FONT_SIZE);

    return bound === 'min' ? VIEWPORT_WIDTH >= value : VIEWPORT_WIDTH <= value;
  });
};

const createMediaQueryList = (query: string): MediaQueryList => ({
  matches: matchesQuery(query),
  media: query,
  onchange: null,
  addListener: () => undefined,
  removeListener: () => undefined,
  addEventListener: () => undefined,
  removeEventListener: () => undefined,
  dispatchEvent: () => false,
});

// jsdom performs no layout and ships neither observer, while the shell measures itself with both:
// the search input group waits for its own visibility before it measures its paddings, and the
// overflowing menus watch their own size.
class TestIntersectionObserver implements IntersectionObserver {
  readonly root = null;
  readonly rootMargin = '';
  readonly thresholds: ReadonlyArray<number> = [];
  observe = () => undefined;
  unobserve = () => undefined;
  disconnect = () => undefined;
  takeRecords = (): Array<IntersectionObserverEntry> => [];
}

class TestResizeObserver implements ResizeObserver {
  observe = () => undefined;
  unobserve = () => undefined;
  disconnect = () => undefined;
}

if (typeof window !== 'undefined') {
  Object.defineProperty(window, 'matchMedia', { writable: true, value: createMediaQueryList });
  Object.defineProperty(window, 'innerWidth', { writable: true, value: VIEWPORT_WIDTH });
  window.scrollTo = () => undefined;
  window.IntersectionObserver = TestIntersectionObserver;
  window.ResizeObserver = TestResizeObserver;
}

// jsdom carries no page router, so the specs answer the router the shell reads from this state and
// set the route they are rendering before they render it.
export const routerState = {
  pathname: '/',
  query: {} as NextRouter['query'],
};

export const nextRouterModule = () => ({
  useRouter: () => ({
    pathname: routerState.pathname,
    route: routerState.pathname,
    asPath: routerState.pathname,
    query: routerState.query,
    basePath: '',
    isReady: true,
    isFallback: false,
    isPreview: false,
    isLocaleDomain: false,
    push: () => Promise.resolve(true),
    replace: () => Promise.resolve(true),
    reload: () => undefined,
    back: () => undefined,
    forward: () => undefined,
    prefetch: () => Promise.resolve(),
    beforePopState: () => undefined,
    events: {
      on: () => undefined,
      off: () => undefined,
      emit: () => undefined,
    },
  }),
});

// The shared vitest wrapper carries the app contexts but no Chakra provider, and the shell is built
// out of Chakra components, so the specs render inside the real provider from the toolkit.
export const Wrapper = ({ children }: { children: React.ReactNode }) => (
  <ChakraProvider>
    <TestApp>{ children }</TestApp>
  </ChakraProvider>
);

// The suite runs without vitest globals, so the testing library cannot register its own teardown
// and every rendered tree would otherwise pile up in the document for the next test to find.
afterEach(cleanup);

export const render = (ui: React.ReactElement, options?: Omit<RenderOptions, 'wrapper'>) =>
  baseRender(ui, { wrapper: Wrapper, ...options });
