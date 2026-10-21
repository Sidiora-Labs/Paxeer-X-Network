// @vitest-environment jsdom

import React from 'react';

import type { Block } from 'types/api/block';

import type getBlockTotalReward from 'lib/block/getBlockTotalReward';
import { SocketProvider } from 'lib/socket/context';
import { SOCKET_FLUSH_INTERVAL_MS } from 'lib/socket/useSocketBuffer';
import * as blockMock from 'mocks/blocks/block';
import * as statsMock from 'mocks/stats/index';
import { render, routerState } from 'ui/shared/layout/testWrapper';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { fireEvent, screen } from 'vitest/lib';
import { createTestSocket } from 'vitest/utils/socketServer';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_STATS_API_HOST: '',
    NEXT_PUBLIC_IS_ACCOUNT_SUPPORTED: 'false',
    NEXT_PUBLIC_MARKETPLACE_CONFIG_URL: '',
  };
});

vi.mock('next/router', async() => (await import('ui/shared/layout/testWrapper')).nextRouterModule());

// A profiler cannot tell a memoised row that bailed out from one that re-rendered, because the commit it
// reports covers the whole list. Every row calls this helper once per render of its own, so counting the
// heights it receives is the render count of each row.
const rowRenders = vi.hoisted(() => [] as Array<number>);

vi.mock('lib/block/getBlockTotalReward', async(importOriginal) => {
  const original = await importOriginal<{ 'default': typeof getBlockTotalReward }>();

  return {
    ...original,
    'default': (block: Block) => {
      rowRenders.push(block.height);

      return original.default(block);
    },
  };
});

import { HomeRpcDataContextProvider } from './fallbacks/rpcDataContext';
import LatestBlocks from './LatestBlocks';

const TOPIC = 'blocks:new_block';

const socketBlock = (offset: number): Block => ({
  ...blockMock.base,
  height: blockMock.base.height + offset,
  hash: `0x${ (1000 + offset).toString(16).padStart(64, '0') }`,
});

const renderCard = (socketUrl?: string, onRender?: React.ProfilerOnRenderCallback) => {
  const card = (
    <HomeRpcDataContextProvider>
      <LatestBlocks/>
    </HomeRpcDataContextProvider>
  );

  const tree = socketUrl ? <SocketProvider url={ socketUrl }>{ card }</SocketProvider> : card;

  return render(onRender ? <React.Profiler id="latest-blocks" onRender={ onRender }>{ tree }</React.Profiler> : tree);
};

const heights = (container: HTMLElement) => Array.from(container.querySelectorAll('[data-latest-block]'))
  .map((item) => item.getAttribute('data-latest-block'));

const rowsContainer = (container: HTMLElement) => container.querySelector('[data-label="latest-blocks-rows"]') as HTMLElement;

vi.setConfig({ testTimeout: 60_000 });

describe('LatestBlocks', () => {
  beforeEach(() => {
    routerState.pathname = '/';
    routerState.query = {};
    rowRenders.length = 0;
    fetchMock.resetMocks();
    fetchMock.mockResponse((request) => {
      const body = request.url.includes('/api/v2/main-page/blocks') ?
        [ blockMock.base, blockMock.base2 ] :
        statsMock.base;

      return { body: JSON.stringify(body), headers: { 'Content-Type': 'application/json' } };
    });
  });

  it('heads the card with the latest blocks and the network utilization', async() => {
    const { container } = renderCard();

    await screen.findByText('Latest blocks');

    await vi.waitFor(() => {
      expect(container.querySelector('[data-scan-table-card] [data-note]')?.textContent).toContain('1.55%');
    }, { timeout: 30_000, interval: 100 });

    expect(container.querySelector('[data-scan-table-card] [data-title]')?.textContent).toBe('Latest blocks');
    expect(container.querySelector('[data-scan-table-card] [data-note]')?.textContent).toContain('Network utilization:');
  });

  it('lists the blocks newest first', async() => {
    const { container } = renderCard();

    await screen.findByText('Latest blocks');

    await vi.waitFor(() => {
      expect(heights(container)).toEqual([ String(blockMock.base.height), String(blockMock.base2.height) ]);
    }, { timeout: 30_000, interval: 100 });
  });

  it('tells how long each block took after its direct parent', async() => {
    const { container } = renderCard();

    await screen.findByText('Latest blocks');

    await vi.waitFor(() => {
      expect(heights(container)).toEqual([ String(blockMock.base.height), String(blockMock.base2.height) ]);
    }, { timeout: 30_000, interval: 100 });

    const rows = Array.from(container.querySelectorAll('[data-latest-block] [data-label="block-hash"]'));

    expect(rows[0].textContent).toContain(`${ blockMock.base.transactions_count } txns in 810s`);
    expect(rows[1].textContent).not.toContain(' in ');
  });

  it('closes the card with the link to every block', async() => {
    const { container } = renderCard();

    await screen.findByText('Latest blocks');

    const footer = container.querySelector('[data-label="view-all-blocks"] a') as HTMLAnchorElement;

    expect(footer.textContent).toBe('View all blocks');
    expect(footer.getAttribute('href')).toBe('/blocks');
  });

  it('turns twenty blocks that arrive inside half a second into one flush and one row set update', async() => {
    const socket = await createTestSocket();

    try {
      const commits: Array<string> = [];
      const { container } = renderCard(socket.url, (id, phase) => {
        commits.push(phase);
      });

      await vi.waitFor(() => {
        expect(heights(container)).toEqual([ String(blockMock.base.height), String(blockMock.base2.height) ]);
      }, { timeout: 30_000, interval: 100 });

      await socket.join(TOPIC);

      // One message the list merges below the two it already holds. Its flush lands on a cadence tick, so
      // the burst that follows sits inside one cadence instead of straddling two of them.
      socket.send(TOPIC, 'new_block', { average_block_time: '1000', block: socketBlock(-100) });

      await vi.waitFor(() => {
        expect(heights(container)).toHaveLength(3);
      }, { timeout: 30_000, interval: 50 });

      // Let the mount work of the row that flush added settle before the burst window opens.
      await new Promise((resolve) => {
        setTimeout(resolve, 300);
      });

      const batches: Array<number> = [];
      const observer = new MutationObserver((records) => {
        batches.push(records.length);
      });
      observer.observe(rowsContainer(container), { childList: true });

      const commitsBeforeBurst = commits.length;

      for (let offset = 1; offset <= 20; offset++) {
        socket.send(TOPIC, 'new_block', { average_block_time: '1000', block: socketBlock(offset) });
        await new Promise((resolve) => {
          setTimeout(resolve, 20);
        });
      }

      expect(commits.length - commitsBeforeBurst).toBe(0);
      expect(batches).toHaveLength(0);

      await vi.waitFor(() => {
        expect(heights(container)).toEqual([
          String(blockMock.base.height + 20),
          String(blockMock.base.height + 19),
          String(blockMock.base.height + 18),
          String(blockMock.base.height + 17),
          String(blockMock.base.height + 16),
          String(blockMock.base.height + 15),
        ]);
      }, { timeout: 30_000, interval: 50 });

      // The flush writes the list once; the rows it mounts may attach their own tooltip state in a second
      // commit, which is what the upper bound leaves room for.
      expect(commits.length - commitsBeforeBurst).toBeLessThanOrEqual(2);
      expect(batches).toHaveLength(1);

      observer.disconnect();
    } finally {
      await socket.close();
    }
  });

  it('holds the flush while the pointer rests on the list and releases it when the pointer leaves', async() => {
    const socket = await createTestSocket();

    try {
      const { container } = renderCard(socket.url);

      await vi.waitFor(() => {
        expect(heights(container)).toEqual([ String(blockMock.base.height), String(blockMock.base2.height) ]);
      }, { timeout: 30_000, interval: 100 });

      await socket.join(TOPIC);

      fireEvent.mouseOver(rowsContainer(container));

      socket.send(TOPIC, 'new_block', { average_block_time: '1000', block: socketBlock(1) });

      await new Promise((resolve) => {
        setTimeout(resolve, SOCKET_FLUSH_INTERVAL_MS * 2);
      });

      expect(heights(container)).toEqual([ String(blockMock.base.height), String(blockMock.base2.height) ]);

      fireEvent.mouseOut(rowsContainer(container), { relatedTarget: document.body });

      await vi.waitFor(() => {
        expect(heights(container)[0]).toBe(String(blockMock.base.height + 1));
      }, { timeout: 30_000, interval: 50 });
    } finally {
      await socket.close();
    }
  });

  it('leaves the rows a flush does not change alone', async() => {
    const socket = await createTestSocket();

    try {
      const { container } = renderCard(socket.url);

      await vi.waitFor(() => {
        expect(heights(container)).toEqual([ String(blockMock.base.height), String(blockMock.base2.height) ]);
      }, { timeout: 30_000, interval: 100 });

      await socket.join(TOPIC);

      await new Promise((resolve) => {
        setTimeout(resolve, 300);
      });

      rowRenders.length = 0;

      socket.send(TOPIC, 'new_block', { average_block_time: '1000', block: socketBlock(1) });

      await vi.waitFor(() => {
        expect(heights(container)[0]).toBe(String(blockMock.base.height + 1));
      }, { timeout: 30_000, interval: 50 });

      expect(rowRenders.filter((height) => height === blockMock.base.height)).toHaveLength(0);
      expect(rowRenders.filter((height) => height === blockMock.base2.height)).toHaveLength(0);
      expect(rowRenders.filter((height) => height === blockMock.base.height + 1)).toHaveLength(1);
    } finally {
      await socket.close();
    }
  });
});
