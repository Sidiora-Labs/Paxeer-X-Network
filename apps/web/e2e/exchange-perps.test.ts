import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { createServer, type IncomingMessage } from "node:http";
import { registerHooks } from "node:module";
import test from "node:test";
import { fileURLToPath } from "node:url";
import ts from "typescript";
import { decodeFunctionData, encodeErrorResult, encodeFunctionData, encodeFunctionResult, type Abi, type Hex } from "viem";

registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier.startsWith(".") && context.parentURL !== undefined) {
      const candidate = new URL(specifier, context.parentURL);
      for (const suffix of [".ts", ".tsx", "/index.ts"]) {
        const source = candidate.href.endsWith(".js") ? new URL(candidate.href.slice(0, -3) + suffix) : new URL(candidate.href + suffix);
        if (existsSync(source)) return nextResolve(source.href, context);
      }
    }
    if (specifier === "server-only") return { url: "data:text/javascript,", shortCircuit: true };
    if (specifier === "next/navigation") return nextResolve("next/navigation.js", context);
    if (specifier === "next/link") return nextResolve("next/link.js", context);
    return nextResolve(specifier, context);
  },
  load(url, context, nextLoad) {
    if (/\.(ts|tsx)$/u.test(url) && !url.includes("/node_modules/")) {
      return {
        format: "module", shortCircuit: true,
        source: ts.transpileModule(readFileSync(new URL(url), "utf8"), {
          fileName: fileURLToPath(url),
          compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext, jsx: ts.JsxEmit.ReactJSX },
        }).outputText,
      };
    }
    const loaded = nextLoad(url, context);
    return loaded.format === "commonjs" ? { ...loaded, source: undefined } : loaded;
  },
});

const precompiles = await import("../src/api/precompiles.ts");
const sdk = await import("../src/api/sdk.ts");
const { preflightPerpsWrite } = await import("../src/app/exchange/actions.ts");
const client = await import("../src/app/exchange/exchange-client.tsx");
const wallet = await import("../src/api/wallet.ts");

// The ABI the chain publishes for 0x1015; every answer below is encoded from it.
const PERPS_ABI = JSON.parse(readFileSync(new URL("../../../precompiles/perps/abi/abi.json", import.meta.url), "utf8")) as Abi;
const PERPS = "0x0000000000000000000000000000000000001015";
const USDL = precompiles.PERPS_COLLATERAL_TOKEN;
const TRADER = `0x${"11".repeat(20)}`;
const ZERO_WORD = `0x${"0".repeat(64)}`;

const ANSWERS: Record<string, (args: readonly unknown[]) => unknown> = {
  totalMarkets: () => 2n,
  getMarket: ([id]) =>
    id === 0n
      ? ["Bitcoin", "BTC", 1000n, 5n, 10n ** 27n, true]
      : ["Ether", "ETH", 500n, 10n, 2n * 10n ** 26n, false],
  getAccountSummary: ([account, token]) => {
    assert.equal(String(account).toLowerCase(), TRADER);
    assert.equal(String(token).toLowerCase(), USDL);
    return { idle: 7_000_000n, totalLocked: 3_000_000n, positionCount: 1n };
  },
  getUserPositionIds: () => [4n, 5n],
  getPosition: ([id]) => [TRADER, 0n, true, 30n * 10n ** 18n, 3n * 10n ** 18n, USDL, 3_000_000n, 65_000n * 10n ** 18n, 1n, id === 4n],
  getUserOrderIds: () => [9n],
  getOrder: () => [TRADER, 1n, false, 0, 0n, 2_000n * 10n ** 18n, 10n ** 19n, 5n * 10n ** 18n, USDL, 2_000_000n, true],
};

type Reply = Readonly<{ result: string }> | Readonly<{ error: { code: number; message: string; data?: string } }>;

async function body(request: IncomingMessage): Promise<{ id: string; method: string; params: { from?: string; to: string; data: Hex }[] }> {
  let text = "";
  for await (const chunk of request) text += String(chunk);
  return JSON.parse(text) as { id: string; method: string; params: { from?: string; to: string; data: Hex }[] };
}

/** A gateway answering eth_call at 0x1015 only for functions the published perps ABI declares. */
async function gateway(answer: (call: { from?: string; to: string; data: Hex }, name: string, args: readonly unknown[]) => Reply, run: (called: string[]) => Promise<void>) {
  const called: string[] = [];
  const server = createServer((request, response) => {
    void body(request).then((envelope) => {
      response.setHeader("content-type", "application/json");
      const [call] = envelope.params;
      let reply: Reply;
      try {
        assert.equal(envelope.method, "eth_call");
        assert.ok(call !== undefined && call.to.toLowerCase() === PERPS, `eth_call to ${String(call?.to)}`);
        const { functionName, args } = decodeFunctionData({ abi: PERPS_ABI, data: call.data });
        called.push(functionName);
        reply = answer(call, functionName, args ?? []);
      } catch (error) {
        called.push(`rejected: ${String(error)}`);
        reply = { error: { code: -32602, message: "unexpected call" } };
      }
      response.end(JSON.stringify({ jsonrpc: "2.0", id: envelope.id, ...reply }));
    });
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  assert.ok(address !== null && typeof address === "object");
  const previous = process.env.LAYERX_GATEWAY_URL;
  process.env.LAYERX_GATEWAY_URL = `http://127.0.0.1:${address.port}`;
  try {
    await run(called);
  } finally {
    if (previous === undefined) delete process.env.LAYERX_GATEWAY_URL;
    else process.env.LAYERX_GATEWAY_URL = previous;
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
}

function served(_call: unknown, name: string, args: readonly unknown[]): Reply {
  const answer = ANSWERS[name];
  assert.ok(answer !== undefined, `unexpected read ${name}`);
  return { result: encodeFunctionResult({ abi: PERPS_ABI, functionName: name, result: answer(args) } as never) };
}

test("the web perps constants match the module default collateral and the precompile's custom errors", () => {
  const params = readFileSync(new URL("../../../modules/perps/types/params.go", import.meta.url), "utf8");
  const declared = /DefaultCollateralToken = "(0x[0-9a-fA-F]{40})"/u.exec(params)?.[1];
  assert.equal(declared?.toLowerCase(), USDL);
  const errors = PERPS_ABI.filter((entry) => entry.type === "error").map((entry) => (entry as { name: string }).name);
  assert.deepEqual([...precompiles.PERPS_ERRORS].sort(), errors.sort());
  assert.equal(sdk.PERPS_PRECOMPILE, PERPS);
});

test("markets come from totalMarkets and getMarket, and the account from its position and order ids", async () => {
  await gateway(served, async (called) => {
    const markets = await precompiles.perpsMarkets();
    assert.deepEqual(markets, [
      { marketId: 0n, name: "Bitcoin", symbol: "BTC", maxLeverage: 1000n, maintenanceMarginBps: 5n, maxOpenInterest: 10n ** 27n, enabled: true },
      { marketId: 1n, name: "Ether", symbol: "ETH", maxLeverage: 500n, maintenanceMarginBps: 10n, maxOpenInterest: 2n * 10n ** 26n, enabled: false },
    ]);
    const account = await precompiles.perpsAccount(TRADER);
    assert.equal(account.idle, 7_000_000n);
    assert.equal(account.locked, 3_000_000n);
    assert.deepEqual(account.positions, [
      { positionId: 4n, marketId: 0n, isLong: true, sizeUsd: 30n * 10n ** 18n, collateralUsd: 3n * 10n ** 18n, collateralAmount: 3_000_000n, entryPrice: 65_000n * 10n ** 18n },
    ]);
    assert.deepEqual(account.orders, [
      { orderId: 9n, marketId: 1n, isLong: false, orderType: 0n, triggerPrice: 0n, limitPrice: 2_000n * 10n ** 18n, sizeUsd: 10n ** 19n, collateralAmount: 2_000_000n },
    ]);
    assert.ok(called.every((name) => !name.startsWith("rejected")), called.join("\n"));
  });
});

test("a perps refusal surfaces the precompile's custom error or reason string", async () => {
  const close = encodeFunctionData({ abi: PERPS_ABI, functionName: "closePosition", args: [4n] });
  assert.equal(close, sdk.perpsClosePositionCall(4n).data);
  const replies: Reply[] = [
    { error: { code: 3, message: "execution reverted", data: encodeErrorResult({ abi: PERPS_ABI, errorName: "PerpsNotPositionHolder" }) } },
    { error: { code: 3, message: "execution reverted", data: encodeErrorResult({ abi: [{ type: "error", name: "Error", inputs: [{ type: "string", name: "reason" }] }], errorName: "Error", args: ["perps: the perps keeper is not wired"] }) } },
    { result: "0x" },
  ];
  const senders: (string | undefined)[] = [];
  await gateway((call, name) => {
    assert.equal(name, "closePosition");
    senders.push(call.from);
    return replies.shift()!;
  }, async () => {
    assert.deepEqual(await preflightPerpsWrite(TRADER, close), { ok: false, detail: "The perps precompile refused: PerpsNotPositionHolder" });
    assert.deepEqual(await preflightPerpsWrite(TRADER, close), { ok: false, detail: "The perps precompile refused: perps: the perps keeper is not wired" });
    assert.deepEqual(await preflightPerpsWrite(TRADER, close), { ok: true });
    assert.deepEqual(await preflightPerpsWrite("0x12", close), { ok: false, detail: "The perps write is malformed." });
  });
  assert.deepEqual(senders, [TRADER, TRADER, TRADER]);
});

test("a market read refusal names the custom error", async () => {
  await gateway(() => ({ error: { code: 3, message: "execution reverted", data: encodeErrorResult({ abi: PERPS_ABI, errorName: "PerpsPaused" }) } }), async () => {
    await assert.rejects(precompiles.perpsMarkets(), /The perps precompile refused: PerpsPaused/u);
  });
});

class MemoryStorage {
  readonly #items = new Map<string, string>();
  getItem(key: string) { return this.#items.get(key) ?? null; }
  setItem(key: string, value: string) { this.#items.set(key, value); }
  removeItem(key: string) { this.#items.delete(key); }
}

function walletWindow(answer: (method: string, params: readonly unknown[]) => unknown) {
  const requests: { method: string; params: readonly unknown[] }[] = [];
  const paxeer = {
    isPaxeer: true,
    request: async ({ method, params = [] }: { method: string; params?: readonly unknown[] }) => {
      requests.push({ method, params });
      return answer(method, params);
    },
  };
  Object.assign(globalThis, { window: { paxeer, localStorage: new MemoryStorage(), sessionStorage: new MemoryStorage(), confirm: () => true } });
  return requests;
}

test("opening a position approves the exact USDL, preflights at 0x1015 and then sends the perps write", async () => {
  const collateral = { token: USDL, decimals: precompiles.PERPS_COLLATERAL_DECIMALS, symbol: "USDL" };
  const order = { marketId: 0n, collateralToken: USDL, collateralAmount: 5_000_000n, leverage: 10n * 10n ** 18n, isLong: true };
  const call = sdk.perpsOpenPositionCall(order);
  assert.equal(call.to, PERPS);
  assert.equal(call.data, encodeFunctionData({ abi: PERPS_ABI, functionName: "openPosition", args: [0n, USDL, 5_000_000n, 10n * 10n ** 18n, true] }));
  const approveHash = `0x${"aa".repeat(32)}`;
  const writeHash = `0x${"bb".repeat(32)}`;
  const requests = walletWindow((method, params) => {
    switch (method) {
      case "eth_call": return ZERO_WORD;
      case "eth_sendTransaction": return (params[0] as { to: string }).to.toLowerCase() === USDL ? approveHash : writeHash;
      case "eth_getTransactionReceipt": return { status: "0x1" };
      default: throw new Error(`unexpected ${method}`);
    }
  });
  const approving: boolean[] = [];
  try {
    await gateway((preflight, name) => {
      assert.equal(name, "openPosition");
      assert.equal(preflight.from, TRADER);
      return { result: encodeFunctionResult({ abi: PERPS_ABI, functionName: "openPosition", result: 4n }) };
    }, async (called) => {
      const outcome = await client.sendPerpsWrite(TRADER, collateral, order.collateralAmount, call, (active) => approving.push(active));
      assert.deepEqual(outcome, { outcome: "sent", transactionHash: writeHash });
      assert.deepEqual(called, ["openPosition"]);
    });
    const sends = requests.filter((request) => request.method === "eth_sendTransaction").map((request) => request.params[0] as { to: string; data: string });
    assert.equal(sends.length, 2);
    assert.equal(sends[0]!.to.toLowerCase(), USDL);
    assert.equal(sends[0]!.data, sdk.encodeAbiCall("approve", ["address", "uint256"], [PERPS, order.collateralAmount]));
    assert.equal(sends[1]!.to.toLowerCase(), PERPS);
    assert.equal(sends[1]!.data, call.data);
    assert.deepEqual(approving, [true, false]);
  } finally {
    delete (globalThis as { window?: unknown }).window;
  }
});

test("a refused perps write is reported with its reason and never reaches the wallet", async () => {
  const collateral = { token: USDL, decimals: precompiles.PERPS_COLLATERAL_DECIMALS, symbol: "USDL" };
  const requests = walletWindow((method) => {
    throw new Error(`unexpected ${method}`);
  });
  try {
    await gateway(() => ({ error: { code: 3, message: "execution reverted", data: encodeErrorResult({ abi: PERPS_ABI, errorName: "PerpsUnknownOrder" }) } }), async () => {
      const outcome = await client.sendPerpsWrite(TRADER, collateral, 0n, sdk.perpsCancelOrderCall(9n), () => undefined);
      assert.deepEqual(outcome, { outcome: "failed", detail: "The perps precompile refused: PerpsUnknownOrder" });
    });
    assert.deepEqual(requests, []);
  } finally {
    delete (globalThis as { window?: unknown }).window;
  }
});

test("a short allowance under the sponsored SID fee is refused before any send", async () => {
  const collateral = { token: USDL, decimals: precompiles.PERPS_COLLATERAL_DECIMALS, symbol: "USDL" };
  const requests = walletWindow((method) => {
    if (method !== "eth_call") throw new Error(`unexpected ${method}`);
    return ZERO_WORD;
  });
  try {
    assert.equal(wallet.chooseWalletFee("sidiora", "2.1"), true);
    const outcome = await client.sendPerpsWrite(TRADER, collateral, 1_000_000n, sdk.perpsDepositCall(USDL, 1_000_000n), () => undefined);
    assert.equal(outcome.outcome, "failed");
    assert.ok(requests.every((request) => request.method === "eth_call"));
  } finally {
    wallet.chooseWalletFee("paxeer", "");
    delete (globalThis as { window?: unknown }).window;
  }
});
