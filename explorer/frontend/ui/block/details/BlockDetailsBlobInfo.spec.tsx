// @vitest-environment jsdom

import React from 'react';

import * as blockMock from 'mocks/blocks/block';
import * as DetailedInfo from 'ui/shared/DetailedInfo/DetailedInfo';
import { render } from 'ui/shared/layout/testWrapper';
import { describe, expect, it, vi } from 'vitest';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import BlockDetailsBlobInfo from './BlockDetailsBlobInfo';

const renderBlobInfo = (data: typeof blockMock.base) => render(
  <DetailedInfo.Container>
    <BlockDetailsBlobInfo data={ data }/>
  </DetailedInfo.Container>,
);

describe('BlockDetailsBlobInfo', () => {
  it('lists the four blob rows in order', () => {
    const { container } = renderBlobInfo(blockMock.withBlobTxs);

    const labels = Array.from(container.querySelectorAll('[data-scan-key]')).map((node) => node.textContent);

    expect(labels).toEqual([ 'Blob gas price', 'Blob gas used', 'Blob burnt fees', 'Excess blob gas' ]);
  });

  it('states the blob gas price in gwei, the gas used in full and the burnt share', () => {
    const { container } = renderBlobInfo(blockMock.withBlobTxs);

    const values = Array.from(container.querySelectorAll('[data-scan-value]')).map((node) => node.textContent);

    expect(values[0]).toContain('21.518435987');
    expect(values[1]).toBe('393,216');
    expect(values[2]).toContain('0.008461393325064192');
    expect(values[2]).toContain('100%');
    expect(values[3]).toContain('0.079429632');
  });

  it('closes the group with a divider', () => {
    const { container } = renderBlobInfo(blockMock.withBlobTxs);

    const cells = Array.from(container.querySelectorAll('[data-scan-key], [data-scan-value], [data-scan-divider]'));

    expect(cells.at(-1)?.hasAttribute('data-scan-divider')).toBe(true);
  });

  it('renders nothing for a block without blob fields', () => {
    const { container } = renderBlobInfo(blockMock.base);

    expect(container.querySelectorAll('[data-scan-key]')).toHaveLength(0);
  });
});
