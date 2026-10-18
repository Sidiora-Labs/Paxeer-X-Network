import test, { mock } from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { createServer, type Server } from "node:http";
import type { Socket } from "node:net";
import type { AddressInfo } from "node:net";
import type { SidPaxOrderbook } from "../packages/paxeer-backend/src/hooks/perps/useSidPaxMarketStream";

// Minimal RFC 6455 server: handshake plus unmasked server text frames.
function marketServer(onConnection: (socket: Socket, index: number) => void) {
  let connections = 0;
  const server: Server = createServer();
  server.on("upgrade", (req, socket: Socket) => {
    const accept = createHash("sha1")
      .update(
        `${req.headers["sec-websocket-key"]}258EAFA5-E914-47DA-95CA-C5AB0DC85B11`,
      )
      .digest("base64");
    socket.write(
      "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
        `Sec-WebSocket-Accept: ${accept}\r\n\r\n`,
    );
    socket.on("data", () => {});
    socket.on("error", () => {});
    onConnection(socket, connections++);
  });
  return server;
}

function sendText(socket: Socket, value: unknown) {
  const payload = Buffer.from(JSON.stringify(value));
  const header =
    payload.length < 126
      ? Buffer.from([0x81, payload.length])
      : Buffer.from([0x81, 126, payload.length >> 8, payload.length & 0xff]);
  socket.write(Buffer.concat([header, payload]));
}

const until = async (check: () => boolean, label: string) => {
  const deadline = Date.now() + 10_000;
  while (!check()) {
    if (Date.now() > deadline) assert.fail(`Timed out waiting for ${label}`);
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
};

test("a producer disconnect reaches native orderbook subscribers and the NativeWebSocket topic as a reset", async () => {
  const sockets: Socket[] = [];
  const server = marketServer((socket, index) => {
    sockets.push(socket);
    sendText(socket, {
      topic: "ETH@perp_orderbook",
      ts: Date.now(),
      data: {
        symbol: "ETH",
        asks: [[2401 + index, 3]],
        bids: [[2400 + index, 2]],
      },
    });
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const { port } = server.address() as AddressInfo;

  const browser = Object.assign(new EventTarget(), {
    location: { origin: "http://localhost:3000" },
  });
  Object.defineProperty(globalThis, "window", {
    configurable: true,
    value: browser,
  });
  Object.defineProperty(globalThis, "document", {
    configurable: true,
    value: Object.assign(new EventTarget(), { hidden: false }),
  });
  const beforeFetch = globalThis.fetch;
  globalThis.fetch = (async (input: string | URL | Request) => {
    assert.match(String(input), /^\/api\/price-feed\/ws-token\?symbol=eth$/);
    return new Response(
      JSON.stringify({
        token: "scoped",
        symbol: "eth",
        url: `ws://127.0.0.1:${port}/ws/eth`,
        expiresAt: new Date(Date.now() + 60_000).toISOString(),
      }),
    );
  }) as typeof fetch;

  let stopNative: (() => void) | undefined;
  let stopTopic: (() => void) | undefined;
  try {
    const { subscribeNativeOrderbook } =
      await import("../packages/paxeer-backend/src/hooks/perps/useSidPaxMarketStream");
    const { NativeWebSocket } =
      await import("../packages/hooks/src/native/NativeWebSocket");

    const books: (SidPaxOrderbook | null)[] = [];
    const messages: any[] = [];
    stopNative = subscribeNativeOrderbook("ETH", (book) => books.push(book));
    stopTopic = new NativeWebSocket().subscribe(
      "PERP_ETH_USDC@orderbookupdate",
      {
        onMessage: (data: unknown) => messages.push(data),
      },
    );
    await until(
      () => books.length === 1 && messages.length === 1,
      "first snapshot",
    );
    assert.deepEqual(books[0]?.asks, [{ price: 2401, size: 3 }]);
    assert.equal(messages[0].reset, undefined);
    assert.deepEqual(messages[0].asks, [[2401, 3]]);

    sockets[0].destroy();
    await until(
      () => books.length === 2 && messages.length === 2,
      "disconnect reset",
    );
    assert.equal(books[1], null);
    assert.equal(messages[1].symbol, "PERP_ETH_USDC");
    assert.equal(messages[1].snapshot, true);
    assert.equal(messages[1].reset, true);
    assert.deepEqual(messages[1].asks, []);
    assert.deepEqual(messages[1].bids, []);

    // The reconnect resubscribes and the producer's fresh snapshot repopulates.
    await until(
      () => books.length === 3 && messages.length === 3,
      "reconnect snapshot",
    );
    assert.deepEqual(books[2]?.bids, [{ price: 2401, size: 2 }]);
    assert.equal(messages[2].reset, undefined);
    assert.deepEqual(messages[2].asks, [[2402, 3]]);
  } finally {
    mock.timers.enable({ apis: ["setTimeout"] });
    stopTopic?.();
    stopNative?.();
    mock.timers.tick(15_000);
    mock.timers.reset();
    // The idle release has closed the client; end the server side and let
    // the client's close handlers run while window is still defined.
    sockets.forEach((socket) => socket.destroy());
    await new Promise((resolve) => setTimeout(resolve, 100));
    await new Promise((resolve) => server.close(resolve));
    globalThis.fetch = beforeFetch;
    Reflect.deleteProperty(globalThis, "window");
    Reflect.deleteProperty(globalThis, "document");
  }
});
