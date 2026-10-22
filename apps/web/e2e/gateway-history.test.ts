import assert from "node:assert/strict";
import { createServer, type IncomingMessage } from "node:http";
import { registerHooks } from "node:module";
import { readFileSync } from "node:fs";
import test from "node:test";
import { fileURLToPath } from "node:url";
import ts from "typescript";

registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier === "server-only") return { url: "data:text/javascript,", shortCircuit: true };
    return nextResolve(specifier, context);
  },
  load(url, context, nextLoad) {
    if (/\.ts$/u.test(url) && !url.includes("/node_modules/")) {
      return {
        format: "module",
        shortCircuit: true,
        source: ts.transpileModule(readFileSync(new URL(url), "utf8"), {
          fileName: fileURLToPath(url),
          compilerOptions: {
            target: ts.ScriptTarget.ES2022,
            module: ts.ModuleKind.ESNext,
          },
        }).outputText,
      };
    }
    return nextLoad(url, context);
  },
});

const gateway = await import("../src/api/gateway.ts");

const POSITION = "00000000000000070000000100000002";

// The gateway's whole px_getUnifiedHistory answer, recorded over the loopback
// indexer store with the account resolved on Paxeer (history::tests in the
// gateway); the gateway test fails when this file stops matching what the
// gateway answers.
const recorded = JSON.parse(
  readFileSync(new URL("./fixtures/px-get-unified-history.json", import.meta.url), "utf8"),
) as {
  result: {
    account: { evm_address: string };
    items: Record<string, unknown>[];
    next_cursor: string | null;
    indexedHeight: number | null;
    chainHeight: number;
  };
};
const ADDRESS = recorded.result.account.evm_address;

/** The recorded rows as px_getHistory serves them: without the unified side. */
function paxeerRows(): Record<string, unknown>[] {
  return recorded.result.items.map(({ side: _side, ...item }) => item);
}

async function body(request: IncomingMessage): Promise<{ id: string; method: string; params: unknown[] }> {
  let text = "";
  for await (const chunk of request) text += String(chunk);
  return JSON.parse(text) as { id: string; method: string; params: unknown[] };
}

async function serving(
  answer: (method: string) => unknown,
  run: (calls: { method: string; params: unknown[] }[]) => Promise<void>,
) {
  const calls: { method: string; params: unknown[] }[] = [];
  const server = createServer((request, response) => {
    void body(request).then((call) => {
      calls.push({ method: call.method, params: call.params });
      response.setHeader("content-type", "application/json");
      response.end(
        JSON.stringify({
          jsonrpc: "2.0",
          id: call.id,
          result: answer(call.method),
        }),
      );
    });
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  assert.ok(address !== null && typeof address === "object");
  process.env.LAYERX_GATEWAY_URL = `http://127.0.0.1:${String(address.port)}/rpc`;
  try {
    await run(calls);
  } finally {
    server.close();
  }
}

test("px_getHistory pages by the position cursor and keeps positions and heights", async () => {
  await serving(
    () => ({
      items: paxeerRows(),
      next_cursor: POSITION,
      indexedHeight: recorded.result.indexedHeight,
      chainHeight: recorded.result.chainHeight,
    }),
    async (calls) => {
      const page = await gateway.paxeerHistory(ADDRESS.toUpperCase().replace("0X", "0x"), { cursor: POSITION });
      assert.deepEqual(calls, [
        {
          method: "px_getHistory",
          params: [ADDRESS, POSITION, gateway.HISTORY_PAGE_LIMIT, null],
        },
      ]);
      assert.equal(page.nextCursor, POSITION);
      assert.equal(page.indexedHeight, 12);
      assert.equal(page.chainHeight, 12);
      assert.deepEqual(
        page.items.map((item) => [item.blockHeight, item.txIndex, item.logIndex]),
        [
          [10, 0, 0],
          [11, 0, 0],
          [12, 0, 0],
          [12, 1, 0],
        ],
      );
      await gateway.paxeerHistory(ADDRESS, { cursor: "cursor_next_page" });
      assert.equal(calls[1]?.params[1], null);
    },
  );
});

test("px_getUnifiedHistory reads the recorded gateway answer with side on every row", async () => {
  await serving(
    () => recorded.result,
    async (calls) => {
      const page = await gateway.unifiedHistory(ADDRESS);
      assert.deepEqual(calls, [
        {
          method: "px_getUnifiedHistory",
          params: [ADDRESS, null, gateway.HISTORY_PAGE_LIMIT, null],
        },
      ]);
      assert.equal(page.nextCursor, null);
      assert.equal(page.indexedHeight, 12);
      assert.equal(page.chainHeight, 12);
      assert.deepEqual(
        page.items.map((item) => [item.id, item.side, item.chain, item.kind, item.direction]),
        [
          ["3", "paxeer", "paxeer", "native_transfer", "out"],
          ["6", "paxeer", "paxeer", "pointer_transfer", "in"],
          ["9", "paxeer", "paxeer", "bank_transfer", "out"],
          ["11", "paxeer", "paxeer", "erc20_transfer", "in"],
        ],
      );
      assert.deepEqual(
        page.items.map((item) => [item.assetSymbol, item.assetDecimals, item.amount]),
        [
          [null, 18, 1000000000000000000n],
          ["LXP", 6, 3n],
          [null, null, 2n],
          [null, null, 4n],
        ],
      );
    },
  );
});

test("px_getHistory reports no side even for rows that carry one", async () => {
  assert.ok(recorded.result.items.every((item) => item.side === "paxeer"));
  await serving(
    () => recorded.result,
    async () => {
      const page = await gateway.paxeerHistory(ADDRESS);
      assert.deepEqual(
        page.items.map((item) => item.side),
        [null, null, null, null],
      );
    },
  );
});

test("history takes every row id the gateway passes through", async () => {
  const ids = ["0", "007", "explorer:0", "explorer:18446744073709551615"];
  await serving(
    () => ({
      ...recorded.result,
      items: recorded.result.items.map((item, index) => ({ ...item, id: ids[index] })),
    }),
    async () => {
      const page = await gateway.unifiedHistory(ADDRESS);
      assert.deepEqual(
        page.items.map((item) => item.id),
        ids,
      );
    },
  );
  for (const id of ["18446744073709551616", "explorer:", "", "1a", "-1", "explorer:explorer:1"]) {
    await serving(
      () => ({ ...recorded.result, items: [{ ...recorded.result.items[0], id }] }),
      async () => {
        await assert.rejects(() => gateway.unifiedHistory(ADDRESS), gateway.GatewayUnavailableError, id);
      },
    );
  }
});

test("history refuses a non-position cursor, a missing position and a bare native key", async () => {
  await serving(
    (method) =>
      method === "px_getUnifiedHistory"
        ? {
            items: [{ ...recorded.result.items[0], blockHeight: undefined }],
            next_cursor: null,
            indexedHeight: null,
            chainHeight: 4,
          }
        : {
            items: [],
            next_cursor: "f".repeat(112),
            indexedHeight: null,
            chainHeight: 4,
          },
    async (calls) => {
      await assert.rejects(() => gateway.paxeerHistory(ADDRESS), gateway.GatewayUnavailableError);
      await assert.rejects(
        () => gateway.unifiedHistory(`did:layerx:${"ab".repeat(32)}`),
        gateway.GatewayUnavailableError,
      );
      await assert.rejects(() => gateway.unifiedHistory("ab".repeat(32)), gateway.GatewayUnavailableError);
      assert.deepEqual(
        calls.map((call) => call.method),
        ["px_getHistory", "px_getUnifiedHistory"],
      );
    },
  );
});

test("the web app names no switched-off history or registration method", () => {
  for (const path of ["../src/api/gateway.ts", "../src/app/_markets/activity-feed.tsx"]) {
    const source = readFileSync(new URL(path, import.meta.url), "utf8");
    assert.doesNotMatch(source, /lx_getHistory|lx_register/u, path);
  }
});
