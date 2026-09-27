// @vitest-environment jsdom

import React from 'react';

import * as addressMock from 'mocks/address/address';
import * as txMock from 'mocks/txs/tx';
import { render } from 'ui/shared/layout/testWrapper';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import AddressFromTo from './AddressFromTo';

const from = txMock.base.from;
const to = txMock.base.to as NonNullable<typeof txMock.base.to>;

const COURSE_ARROW = 'use[href$="#arrows/east"]';

describe('AddressFromTo', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({}), { headers: { 'Content-Type': 'application/json' } });
  });

  it('marks the row outgoing when the current address sent it', () => {
    const { container } = render(<AddressFromTo from={ from } to={ to } current={ from.hash }/>);

    const badge = container.querySelector('[data-direction]');

    expect(badge?.getAttribute('data-direction')).toBe('out');
    expect(badge?.textContent).toBe('OUT');
    expect(container.querySelector(COURSE_ARROW)).toBeNull();
  });

  it('marks the row incoming when the current address received it', () => {
    const { container } = render(<AddressFromTo from={ from } to={ to } current={ addressMock.hash }/>);

    const badge = container.querySelector('[data-direction]');

    expect(badge?.getAttribute('data-direction')).toBe('in');
    expect(badge?.textContent).toBe('IN');
  });

  it('puts the badge between the two addresses', () => {
    const { container } = render(<AddressFromTo from={ from } to={ to } current={ from.hash }/>);

    const badge = container.querySelector('[data-direction]') as HTMLElement;
    const columns = Array.from(badge.parentElement?.children ?? []);

    expect(columns).toHaveLength(3);
    expect(columns.indexOf(badge)).toBe(1);
    expect(columns[0].textContent).not.toBe('');
    expect(columns[2].querySelector('a[href^="/address/"]')?.getAttribute('href')).toBe(`/address/${ to.hash }`);
  });

  it('keeps the course arrow when no address is in scope', () => {
    const { container } = render(<AddressFromTo from={ from } to={ to }/>);

    expect(container.querySelector('[data-direction]')).toBeNull();
    expect(container.querySelector(COURSE_ARROW)).not.toBeNull();
  });

  it('keeps the course arrow when the address sent the transaction to itself', () => {
    const { container } = render(<AddressFromTo from={ from } to={ from } current={ from.hash }/>);

    expect(container.querySelector('[data-direction]')).toBeNull();
    expect(container.querySelector(COURSE_ARROW)).not.toBeNull();
  });

  it('carries the badge into the stacked layout as well', () => {
    const { container } = render(<AddressFromTo from={ from } to={ to } current={ from.hash } mode="compact"/>);

    expect(container.querySelector('[data-direction]')?.getAttribute('data-direction')).toBe('out');
    expect(container.querySelector(COURSE_ARROW)).toBeNull();
  });
});
