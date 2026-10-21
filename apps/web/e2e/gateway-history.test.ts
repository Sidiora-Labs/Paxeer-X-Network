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

const ADDRESS = `0x${"ab".repeat(20)}`;
const POSITION = "00000000000000070000000100000002";

function row(height: number) {
  return {
    id: "41",
    height_or_seq: String(height),
    chain: "paxeer",
    kind: "erc20_transfer",
    direction: "in",
    account: ADDRESS,
    counterparty: null,
    asset: "0x00000000000000000000000000000000000000cd",
    asset_metadata: { symbol: "SID", decimals: 18 },
    amount: "1000",
    tx_id: `0x${"11".repeat(32)}`,
    final: true,
    side: "paxeer",
    blockHeight: height,
    txIndex: 1,
    logIndex: 2,
  };
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
      items: [row(7)],
      next_cursor: POSITION,
      indexedHeight: 9,
      chainHeight: 12,
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
      assert.equal(page.indexedHeight, 9);
      assert.equal(page.chainHeight, 12);
      assert.deepEqual(
        page.items.map((item) => [item.blockHeight, item.txIndex, item.logIndex]),
        [[7, 1, 2]],
      );
      await gateway.paxeerHistory(ADDRESS, { cursor: "cursor_next_page" });
      assert.equal(calls[1]?.params[1], null);
    },
  );
});

test("history refuses a non-position cursor, a missing position and a bare native key", async () => {
  await serving(
    (method) =>
      method === "px_getUnifiedHistory"
        ? {
            items: [{ ...row(3), blockHeight: undefined }],
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
