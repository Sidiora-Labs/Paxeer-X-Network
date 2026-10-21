import type { AddressInfo } from 'node:net';
import type { WebSocket } from 'ws';
import { WebSocketServer } from 'ws';

// The live lists on the home page read their updates through the real socket hooks, so the specs run a
// real socket endpoint on a free port, answer the channel join the client sends and push real channel
// messages over it instead of standing a double in front of the hooks.
type ChannelKey = [ string, string, string ];

type Waiter = () => void;

interface JoinedChannel {
  socket: WebSocket;
  key: ChannelKey;
}

export interface TestSocket {
  url: string;
  join: (topic: string) => Promise<void>;
  send: (topic: string, event: string, payload: unknown) => void;
  close: () => Promise<void>;
}

export async function createTestSocket(): Promise<TestSocket> {
  const server = new WebSocketServer({ host: '127.0.0.1', port: 0 });
  const channels = new Map<string, JoinedChannel>();
  const waiting = new Map<string, Waiter>();

  await new Promise<void>((resolve) => {
    server.once('listening', resolve);
  });

  server.on('connection', (socket: WebSocket) => {
    socket.on('message', (raw) => {
      const [ joinRef, ref, topic, event ] = JSON.parse(raw.toString()) as [ string, string, string, string, unknown ];

      if (event === 'heartbeat') {
        socket.send(JSON.stringify([ joinRef, ref, topic, 'phx_reply', { response: {}, status: 'ok' } ]));
        return;
      }

      if (event !== 'phx_join') {
        return;
      }

      socket.send(JSON.stringify([ joinRef, ref, topic, 'phx_reply', { response: {}, status: 'ok' } ]));
      channels.set(topic, { socket, key: [ joinRef, ref, topic ] });
      waiting.get(topic)?.();
      waiting.delete(topic);
    });
  });

  const { port } = server.address() as AddressInfo;

  return {
    url: `ws://127.0.0.1:${ port }/socket`,

    join: (topic: string) => new Promise<void>((resolve) => {
      if (channels.has(topic)) {
        resolve();
        return;
      }

      waiting.set(topic, resolve);
    }),

    send: (topic: string, event: string, payload: unknown) => {
      const channel = channels.get(topic);

      if (!channel) {
        throw new Error(`no client joined ${ topic }`);
      }

      channel.socket.send(JSON.stringify([ ...channel.key, event, payload ]));
    },

    close: () => new Promise<void>((resolve, reject) => {
      server.clients.forEach((client) => client.terminate());
      server.close((error) => error ? reject(error) : resolve());
    }),
  };
}
