// @vitest-environment jsdom

import React from 'react';

import { Provider } from 'toolkit/chakra/provider';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render } from 'vitest/lib';

import { Container, Content, Copy, Link } from './components';

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

const HASH = '0x0000000000000000000000000000000000000001';

describe('entity container', () => {
  it('marks the row so a page can style every entity the same way', () => {
    const { container } = render(
      <Provider>
        <Container>
          <span>content</span>
        </Container>
      </Provider>,
    );

    expect(container.querySelector('[data-entity]')?.textContent).toBe('content');
  });
});

describe('entity link', () => {
  it('leads to the entity through an anchor of its own', () => {
    const { container } = render(
      <Provider>
        <Link href={ `/address/${ HASH }` }>
          <span>{ HASH }</span>
        </Link>
      </Provider>,
    );

    const link = container.querySelector('[data-entity-link]');

    expect(link).not.toBeNull();
    expect(link?.getAttribute('href')).toBe(`/address/${ HASH }`);
    expect(link?.textContent).toBe(HASH);
  });

  it('renders the text without a link when the caller asks for none', () => {
    const { container } = render(
      <Provider>
        <Link href={ `/address/${ HASH }` } noLink>
          <span>{ HASH }</span>
        </Link>
      </Provider>,
    );

    expect(container.querySelector('[data-entity-link]')).toBeNull();
  });
});

describe('entity content', () => {
  it('keeps the whole text when nothing is truncated', () => {
    const { container } = render(
      <Provider>
        <Content text="Paxeer" truncation="none"/>
      </Provider>,
    );

    expect(container.querySelector('[data-entity-content]')?.textContent).toBe('Paxeer');
  });

  it('shortens a hash from the middle by default', () => {
    const { container } = render(
      <Provider>
        <Content text={ HASH } truncation="constant"/>
      </Provider>,
    );

    const text = container.querySelector('[data-entity-content]')?.textContent ?? '';

    expect(text.length).toBeLessThan(HASH.length);
    expect(text.startsWith('0x')).toBe(true);
    expect(text.endsWith(HASH.slice(-4))).toBe(true);
  });
});

describe('entity copy', () => {
  it('sits after the value as one labelled control', () => {
    const { container } = render(
      <Provider>
        <Container>
          <Content text="Paxeer" truncation="none"/>
          <Copy text={ HASH }/>
        </Container>
      </Provider>,
    );

    const children = Array.from(container.querySelector('[data-entity]')?.children ?? []);

    expect(children).toHaveLength(2);
    expect(children[1].getAttribute('aria-label')).toBe('copy');
  });

  it('renders nothing when the entity carries no copy control', () => {
    const { container } = render(
      <Provider>
        <Copy text={ HASH } noCopy/>
      </Provider>,
    );

    expect(container.querySelector('[aria-label="copy"]')).toBeNull();
  });
});
