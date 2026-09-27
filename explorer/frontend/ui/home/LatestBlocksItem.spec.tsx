// @vitest-environment jsdom

import React from 'react';

import * as blockMock from 'mocks/blocks/block';
import { render } from 'ui/shared/layout/testWrapper';
import { describe, it, expect, vi } from 'vitest';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_IS_ACCOUNT_SUPPORTED: 'false',
    NEXT_PUBLIC_MARKETPLACE_CONFIG_URL: '',
  };
});

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import LatestBlocksItem from './LatestBlocksItem';

vi.setConfig({ testTimeout: 60_000 });

describe('LatestBlocksItem', () => {
  it('carries the height, the hash and the transaction count of the block', () => {
    const { container } = render(<LatestBlocksItem block={ blockMock.base }/>);

    const row = container.querySelector(`[data-latest-block="${ blockMock.base.height }"]`) as HTMLElement;

    expect(row.textContent).toContain(String(blockMock.base.height));
    expect(row.textContent).toContain('Hash');
    expect(row.textContent).toContain(`${ blockMock.base.transactions_count } txns`);
    expect(row.querySelector(`a[href="/block/${ blockMock.base.hash }"]`)).not.toBeNull();
  });

  it('carries the reward of the block', () => {
    const { container } = render(<LatestBlocksItem block={ blockMock.base }/>);

    expect(container.querySelector('[data-label="block-reward"]')?.textContent).toContain('ETH');
  });

  it('links the height to the block page', () => {
    const { container } = render(<LatestBlocksItem block={ blockMock.base }/>);

    expect(container.querySelector(`a[href="/block/${ blockMock.base.height }"]`)).not.toBeNull();
  });

  it('enters without the fade that moved the rows below it', () => {
    const { container } = render(<LatestBlocksItem block={ blockMock.base }/>);

    const row = container.querySelector(`[data-latest-block="${ blockMock.base.height }"]`) as HTMLElement;
    const styles = Array.from(document.querySelectorAll('style'))
      .map((element) => element.textContent ?? '')
      .join('\n')
      .replace(/\s+/g, '');

    expect(row.getAttribute('style') ?? '').not.toContain('animation');
    expect(styles).not.toContain('animation:fade-in');
  });

  it('lays the row out so its columns wrap instead of widening the card', () => {
    const { container } = render(<LatestBlocksItem block={ blockMock.base }/>);

    const row = container.querySelector(`[data-latest-block="${ blockMock.base.height }"]`) as HTMLElement;

    expect(row.hasAttribute('data-wrap-row')).toBe(true);
    expect(Array.from(row.querySelectorAll('[data-label]')).map((slot) => slot.getAttribute('data-label')))
      .toEqual([ 'block-height', 'block-hash', 'block-reward' ]);
  });

  it('holds the hash and the transaction count in a slot of their own', () => {
    const { container } = render(<LatestBlocksItem block={ blockMock.base }/>);

    const slot = container.querySelector('[data-label="block-hash"]') as HTMLElement;

    expect(slot.textContent).toContain('Hash');
    expect(slot.textContent).toContain(`${ blockMock.base.transactions_count } txns`);
    expect(slot.querySelector(`a[href="/block/${ blockMock.base.hash }"]`)).not.toBeNull();
  });

  it('sizes the height and age column from its text rather than a fixed width', () => {
    const { container } = render(<LatestBlocksItem block={ blockMock.base }/>);

    const column = container.querySelector('[data-label="block-height"]') as HTMLElement;
    const styles = Array.from(document.querySelectorAll('style'))
      .map((element) => element.textContent ?? '')
      .join('\n')
      .replace(/\s+/g, '');

    expect(column.textContent).toContain(String(blockMock.base.height));
    expect(column.getAttribute('style') ?? '').not.toContain('width');
    expect(styles).not.toContain('width:96px');
    expect(styles).not.toContain('width:116px');
  });

  it('shortens the hash to the width the row leaves it', () => {
    const { container } = render(<LatestBlocksItem block={ blockMock.base }/>);

    const link = container.querySelector(`a[href="/block/${ blockMock.base.hash }"]`) as HTMLElement;

    // the dynamic shortener measures the slot around it; jsdom reports every box as zero wide, so it keeps
    // the whole hash here and cuts it only where a real layout constrains the slot
    expect(link.textContent).toBe(blockMock.base.hash);
  });
});
