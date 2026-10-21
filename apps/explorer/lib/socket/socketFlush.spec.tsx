// @vitest-environment jsdom

import React from 'react';

import type { SocketMessage } from 'lib/socket/types';

import { SocketProvider } from 'lib/socket/context';
import useSocketBuffer from 'lib/socket/useSocketBuffer';
import useSocketChannel from 'lib/socket/useSocketChannel';
import useSocketMessage from 'lib/socket/useSocketMessage';
import { render } from 'ui/shared/layout/testWrapper';
import { describe, it, expect, vi } from 'vitest';
import { createTestSocket } from 'vitest/utils/socketServer';

const TOPIC = 'transactions:new_transaction';

interface HarnessProps {
  flushes: Array<Array<number>>;
  isPaused: boolean;
}

// The messages travel the way they travel on the home page: a real socket endpoint, the channel the socket
// context joins, the message hook and the buffer the live lists flush from. Nothing here stands in for the
// socket, so what the cadence does to twenty messages is what it does to twenty blocks.
const Harness = ({ flushes, isPaused }: HarnessProps) => {
  const onFlush = React.useCallback((items: Array<number>) => {
    flushes.push(items);
  }, [ flushes ]);

  const { push, isHeld } = useSocketBuffer<number>({ onFlush, isPaused });

  const handler: SocketMessage.NewTx['handler'] = React.useCallback((payload) => {
    push(payload.transaction);
  }, [ push ]);

  const channel = useSocketChannel({ topic: TOPIC, isDisabled: false });
  useSocketMessage({ channel, event: 'transaction', handler });

  return <div data-held={ isHeld }/>;
};

vi.setConfig({ testTimeout: 60_000 });

describe('the socket flush cadence', () => {
  it('turns twenty messages that arrive inside half a second into one flush', async() => {
    const socket = await createTestSocket();

    try {
      const flushes: Array<Array<number>> = [];

      const { container, rerender } = render(
        <SocketProvider url={ socket.url }>
          <Harness flushes={ flushes } isPaused={ true }/>
        </SocketProvider>,
      );

      await socket.join(TOPIC);

      expect(container.querySelector('[data-held="true"]')).not.toBeNull();

      const start = Date.now();

      for (let count = 1; count <= 20; count++) {
        socket.send(TOPIC, 'transaction', { transaction: count });
        await new Promise((resolve) => {
          setTimeout(resolve, 5);
        });
      }

      await new Promise((resolve) => {
        setTimeout(resolve, 200);
      });

      expect(Date.now() - start).toBeLessThan(500);
      expect(flushes).toHaveLength(0);

      rerender(
        <SocketProvider url={ socket.url }>
          <Harness flushes={ flushes } isPaused={ false }/>
        </SocketProvider>,
      );

      await vi.waitFor(() => {
        expect(flushes).toHaveLength(1);
      }, { timeout: 30_000, interval: 50 });

      expect(flushes[0]).toHaveLength(20);
      expect(flushes[0][19]).toBe(20);

      await new Promise((resolve) => {
        setTimeout(resolve, 2500);
      });

      expect(flushes).toHaveLength(1);
    } finally {
      await socket.close();
    }
  });
});
