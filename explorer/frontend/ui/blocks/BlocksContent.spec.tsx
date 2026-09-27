// @vitest-environment jsdom

import React from 'react';

import * as blockMock from 'mocks/blocks/block';
import * as statsMock from 'mocks/stats/index';
import { render } from 'ui/shared/layout/testWrapper';
import useQueryWithPages from 'ui/shared/pagination/useQueryWithPages';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { waitFor } from 'vitest/lib';

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

import BlocksContent from './BlocksContent';

const Harness = ({ type }: { type: 'block' | 'reorg' }) => {
  const query = useQueryWithPages({
    resourceName: 'general:blocks',
    filters: { type },
  });

  return <BlocksContent type={ type } query={ query } top={ 0 }/>;
};

describe('BlocksContent', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse((request) => {
      if (request.url.includes('/api/v2/stats')) {
        return { body: JSON.stringify(statsMock.base), headers: { 'Content-Type': 'application/json' } };
      }

      if (request.url.includes('/api/v2/blocks')) {
        return { body: JSON.stringify(blockMock.baseListResponse), headers: { 'Content-Type': 'application/json' } };
      }

      return { body: JSON.stringify({}), headers: { 'Content-Type': 'application/json' } };
    });
  });

  it('heads the card with the count line the chain statistics carry', async() => {
    const { container } = render(<Harness type="block"/>);

    await waitFor(() => {
      expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
        .toBe('Latest 2 from a total of 30,215,608 blocks');
    });
  });

  it('notes which records the page is showing', async() => {
    const { container } = render(<Harness type="block"/>);

    await waitFor(() => {
      expect(container.querySelector('[data-scan-table-card] [data-note]')?.textContent)
        .toBe('Showing page 1 of the records the node returns, newest first');
    });
  });

  it('counts a forked list on its own and names what it holds', async() => {
    const { container } = render(<Harness type="reorg"/>);

    await waitFor(() => {
      expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent)
        .toBe('A total of 2 blocks found');
    });

    expect(container.querySelector('[data-scan-table-card] [data-note]')?.textContent)
      .toBe('Blocks replaced by a competing block at the same height');
  });

  it('puts the block table inside the card body', async() => {
    const { container } = render(<Harness type="block"/>);

    await waitFor(() => {
      const heights = Array.from(container.querySelectorAll('[data-scan-table-card] [data-body] table tbody a[href^="/block/"]'))
        .map((link) => link.getAttribute('href'))
        .filter((href) => !href?.includes('?'));

      expect(heights).toEqual([ `/block/${ blockMock.base.height }`, `/block/${ blockMock.base2.height }` ]);
    });
  });
});
