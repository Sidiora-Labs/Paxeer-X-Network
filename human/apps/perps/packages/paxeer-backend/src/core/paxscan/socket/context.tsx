// https://hexdocs.pm/phoenix/js/
import type { Channel, SocketConnectOption } from 'phoenix';
import { Socket } from 'phoenix';
import React, { useEffect, useState } from 'react';

type ChannelRegistry = Record<string, { channel: Channel; subscribers: number }>;

/**
 * Phoenix's Socket constructor auto-appends `/websocket` to reach the
 * actual WS transport endpoint. If the user's env var already contains
 * `/websocket` (easy mistake), Phoenix produces `…/websocket/websocket`
 * which 404s and retries forever. Normalize here so we never double.
 */
function normalizePhoenixUrl(raw: string): string {
  // Strip a trailing /websocket (with optional /) exactly once.
  return raw.replace(/\/websocket\/?$/, '');
}

/**
 * Backoff schedule for reconnect attempts. Phoenix's default is
 * [10, 50, 100, 150, 200, 250, 500, 1000, 2000] ms which floods the
 * network and the console when the endpoint is unreachable. This schedule
 * is much calmer and caps at 60s so a dead endpoint can't create a
 * sustained storm of failed requests.
 */
function reconnectBackoffMs(tries: number): number {
  const schedule = [2_000, 5_000, 10_000, 30_000, 60_000];
  return Math.floor(Math.random() * schedule[Math.min(Math.max(tries - 1, 0), schedule.length - 1)]!);
}

const socketContexts = new Map<string, React.Context<{
  socket: Socket | null;
  channelRegistry: React.MutableRefObject<ChannelRegistry>;
} | null>>();

function getSocketContext(name: string) {
  if (!socketContexts.has(name)) {
    socketContexts.set(name, React.createContext<{
      socket: Socket | null;
      channelRegistry: React.MutableRefObject<ChannelRegistry>;
    } | null>(null));
  }
  return socketContexts.get(name)!;
}

interface PaxscanSocketProviderProps {
  children: React.ReactNode;
  url?: string;
  options?: Partial<SocketConnectOption>;
  name?: string;
}

export function PaxscanSocketProvider({ children, options, url, name = 'default' }: PaxscanSocketProviderProps) {
  const [ socket, setSocket ] = useState<Socket | null>(null);
  const channelRegistry = React.useRef<ChannelRegistry>({});

  useEffect(() => {
    if (!url) {
      return;
    }

    const normalized = normalizePhoenixUrl(url);
    if (process.env.NODE_ENV !== 'production' && normalized !== url) {
      console.warn(
        `[paxscan] Trailing /websocket stripped from URL. ` +
        `Phoenix appends /websocket internally — ` +
        `set NEXT_PUBLIC_PAXSCAN_WS_URL to the socket endpoint without /websocket. ` +
        `(given: ${ url }, used: ${ normalized })`,
      );
    }

    const socketInstance = new Socket(normalized, {
      reconnectAfterMs: reconnectBackoffMs,
      ...options,
    });
    // The first channel join connects; an unused provider opens no socket.
    setSocket(socketInstance);

    return () => {
      socketInstance.disconnect();
      setSocket(null);
    };
  }, [ options, url ]);

  const value = React.useMemo(() => ({
    socket,
    channelRegistry,
  }), [ socket, channelRegistry ]);

  const SocketContext = getSocketContext(name);

  return (
    <SocketContext.Provider value={ value }>
      { children }
    </SocketContext.Provider>
  );
}

export function usePaxscanSocket(name: string = 'default') {
  const SocketContext = getSocketContext(name);
  const context = React.useContext(SocketContext);
  if (context === undefined) {
    throw new Error(`usePaxscanSocket must be used within a PaxscanSocketProvider with name "${ name }"`);
  }
  return context;
}
