import { test } from "node:test";
import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";
import { decodeFunctionData, encodeFunctionData, type Hex } from "viem";

import { paxeerChain } from "../src/core/chain";
import { getPaxeerRpcUrls } from "../src/core/rpc";
import { PRECOMPILES } from "../src/core/network/precompiles";
import { PerpsPrecompileABI } from "../src/core/network/abi/PerpsPrecompile";
import { buildOpenPosition } from "../src/core/perps/calls";
import { resolvePaxeerConfig } from "../src/core/wallet/embedded/client";
import { createPaxeerEIP1193Provider } from "../src/core/wallet/embedded/eip1193";
import { PaxeerWallet } from "../src/core/wallet/embedded/sdk";

const CLIENT_SOURCE = readFileSync(new URL("../src/core/wallet/embedded/client.ts", import.meta.url), "utf8");
const ENV_KEYS = [
  "NEXT_PUBLIC_PAXEER_WALLET_API",
  "NEXT_PUBLIC_SUPABASE_URL",
  "NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY",
  "NEXT_PUBLIC_PAXEER_RPC_URLS",
  "NEXT_PUBLIC_PAXEER_RPC_URL",
] as const;
const WALLET_ADDRESS = "0x00000000000000000000000000000000000000A1";
const TX_HASH = `0x${"ab".repeat(32)}` as Hex;
const E18 = 10n ** 18n;

/** Run `fn` with a browser `window` present and exactly the given NEXT_PUBLIC_* values set. */
async function withBrowserEnv<T>(env: Partial<Record<(typeof ENV_KEYS)[number], string>>, fn: () => Promise<T> | T) {
  const saved = new Map(ENV_KEYS.map((k) => [k, process.env[k]]));
  const g = globalThis as { window?: unknown };
  const hadWindow = "window" in g;
  const savedWindow = g.window;
  for (const k of ENV_KEYS) {
    const v = env[k];
    if (v === undefined) delete process.env[k];
    else process.env[k] = v;
  }
  g.window = {};
  try {
    return await fn();
  } finally {
    for (const [k, v] of saved) {
      if (v === undefined) delete process.env[k];
      else process.env[k] = v;
    }
    if (hadWindow) g.window = savedWindow;
    else delete g.window;
  }
}

function base64url(value: unknown): string {
  return Buffer.from(JSON.stringify(value)).toString("base64url");
}

function readBody(req: IncomingMessage): Promise<string> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    req.on("data", (c: Buffer) => chunks.push(c));
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    req.on("error", reject);
  });
}

function sendJson(res: ServerResponse, status: number, body: unknown): void {
  res.writeHead(status, { "Content-Type": "application/json" });
  res.end(JSON.stringify(body));
}

test("chainIdFromChainObject", async () => {
  const config = await withBrowserEnv({ NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY: "test-anon-key" }, () =>
    resolvePaxeerConfig(),
  );
  assert.ok(config);
  assert.equal(config.chainId, paxeerChain.id);
  assert.equal(config.chainId, 125);
  assert.equal(config.rpcUrl, getPaxeerRpcUrls()[0]);
  assert.equal(config.rpcUrl, "https://api1.mainnet-beta.paxeer.network");
  assert.deepEqual(config, {
    apiUrl: "https://connect.paxportwallet.com",
    supabaseUrl: "https://zezsqawedbikldiedlse.supabase.co",
    supabaseAnonKey: "test-anon-key",
    chainId: 125,
    rpcUrl: "https://api1.mainnet-beta.paxeer.network",
  });

  const pinned = await withBrowserEnv(
    {
      NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY: "test-anon-key",
      NEXT_PUBLIC_PAXEER_RPC_URLS: "https://rpc-a.example.test, https://rpc-b.example.test",
    },
    () => resolvePaxeerConfig(),
  );
  assert.ok(pinned);
  assert.equal(pinned.rpcUrl, "https://rpc-a.example.test");
  assert.equal(pinned.chainId, 125);

  assert.equal(
    await withBrowserEnv({}, () => resolvePaxeerConfig()),
    null,
    "a missing publishable key disables the embedded wallet",
  );
  assert.equal(resolvePaxeerConfig(), null, "no configuration on the server");

  assert.ok(!/chainId:\s*\d/.test(CLIENT_SOURCE), "client.ts carries no chain id literal");
  assert.ok(!CLIENT_SOURCE.includes("mainnet-beta"), "client.ts carries no RPC host literal");
  assert.ok(CLIENT_SOURCE.includes("https://connect.paxportwallet.com"), "wallet API origin stays");
  assert.ok(CLIENT_SOURCE.includes("https://zezsqawedbikldiedlse.supabase.co"), "auth origin stays");
});

test("embeddedTxTargetsPrecompile", async () => {
  const accessToken = [
    base64url({ alg: "HS256", typ: "JWT" }),
    base64url({
      sub: "00000000-0000-4000-8000-000000000001",
      aud: "authenticated",
      role: "authenticated",
      exp: Math.floor(Date.now() / 1000) + 3600,
    }),
    base64url("signature"),
  ].join(".");
  const sent: Array<{ authorization: string | undefined; body: { tx: Record<string, unknown> } }> = [];

  const server = createServer((req, res) => {
    void (async () => {
      const body = await readBody(req);
      if (req.method === "GET" && req.url === "/auth/v1/user") {
        sendJson(res, 200, {
          id: "00000000-0000-4000-8000-000000000001",
          aud: "authenticated",
          role: "authenticated",
          app_metadata: {},
          user_metadata: {},
          created_at: "1970-01-01T00:00:00Z",
        });
        return;
      }
      if (req.method === "GET" && req.url === "/v1/wallet/me") {
        sendJson(res, 200, {
          wallet: { address: WALLET_ADDRESS.toLowerCase() },
          chain: { chain_id: 125 },
        });
        return;
      }
      if (req.method === "POST" && req.url === "/v1/wallet/send") {
        sent.push({ authorization: req.headers.authorization, body: JSON.parse(body) });
        sendJson(res, 200, { tx_hash: TX_HASH, address: WALLET_ADDRESS.toLowerCase(), chain_id: 125 });
        return;
      }
      sendJson(res, 404, { error: "not_found", message: `${req.method} ${req.url}` });
    })();
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;

  const config = await withBrowserEnv(
    {
      NEXT_PUBLIC_PAXEER_WALLET_API: origin,
      NEXT_PUBLIC_SUPABASE_URL: origin,
      NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY: "test-anon-key",
    },
    () => resolvePaxeerConfig(),
  );
  assert.ok(config);
  const wallet = new PaxeerWallet({
    apiUrl: config.apiUrl,
    supabaseUrl: config.supabaseUrl,
    supabaseAnonKey: config.supabaseAnonKey,
  });
  const provider = createPaxeerEIP1193Provider({ wallet, chainId: config.chainId, rpcUrl: config.rpcUrl });

  try {
    const { error } = await wallet.supabase.auth.setSession({ access_token: accessToken, refresh_token: "refresh" });
    assert.equal(error, null);
    assert.deepEqual(await provider.request({ method: "eth_requestAccounts" }), [WALLET_ADDRESS]);

    const open = buildOpenPosition({
      marketId: 0n,
      isLong: true,
      collateralAmount: 100_000_000n,
      leverage: 5n * E18,
      quote: { entryPrice: 2_000n * E18, sizeUsd: 500n * E18 },
    });
    const data = encodeFunctionData(open);
    const request = {
      method: "eth_sendTransaction",
      params: [{ from: WALLET_ADDRESS, to: open.address, data, value: "0x0" }],
    };

    assert.equal(await provider.request(request), TX_HASH);
    assert.equal(sent.length, 1);
    const [{ authorization, body }] = sent;
    assert.equal(authorization, `Bearer ${accessToken}`);
    assert.deepEqual(body.tx, {
      to: "0x0000000000000000000000000000000000001015",
      value: "0",
      data,
      chainId: 125,
    });
    assert.equal(body.tx.to, PRECOMPILES.perps);
    assert.equal(body.tx.chainId, paxeerChain.id);

    const decoded = decodeFunctionData({ abi: PerpsPrecompileABI, data: body.tx.data as Hex });
    assert.equal(decoded.functionName, "placeOrder");
    assert.deepEqual(decoded.args, open.args);
  } finally {
    provider.destroy();
    await wallet.supabase.auth.stopAutoRefresh();
    await new Promise<void>((resolve, reject) => server.close((err) => (err ? reject(err) : resolve())));
  }
});
