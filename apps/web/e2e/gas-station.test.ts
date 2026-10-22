import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { registerHooks } from "node:module";
import test from "node:test";
import { fileURLToPath } from "node:url";
import ts from "typescript";

import { BANNED_VOCABULARY, copyEntries } from "../copy/catalog.ts";
import { copyEntry } from "../copy/runtime.ts";
import type { SidioraGasQuote } from "../src/api/gas-station.ts";

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

const { createElement } = await import("react");
const { renderToStaticMarkup } = await import("react-dom/server");

const sdk = await import("../src/api/sdk.ts");
const wallet = await import("../src/api/wallet.ts");
const { WalletFeeController } = await import("../src/settings/wallet/model.ts");
const { WalletFeeChoice } = await import("../src/settings/wallet/wallet-screen.tsx");

function quoteFixture(): SidioraGasQuote {
  return {
    batch: {
      chainId: 1325n,
      account: `0x${"11".repeat(20)}`,
      nonce: 0n,
      calls: [{ to: `0x${"33".repeat(20)}`, value: 0n, data: "0x1234" }],
      quote: {
        sponsor: `0x${"22".repeat(20)}`, token: sdk.SIDIORA_TOKEN,
        decimals: sdk.SIDIORA_DECIMALS,
        tokenAmount: 2_000_001n, maxTokenAmount: 2_100_000n,
        deadline: BigInt(Math.floor(Date.now() / 1000)) + 300n,
        quoteNonce: 7n, gasCost: 10n ** 18n,
      },
    },
    paymaster: `0x${"44".repeat(20)}`,
    relayerSignature: "0x",
  };
}

test("quote consent presents six decimals, the maximum and expiry before signing", () => {
  const quoted = quoteFixture();
  const review = wallet.sidioraQuotePresentation(quoted);
  assert.ok(review);
  assert.equal(review.amount, "2.000001");
  assert.equal(review.maximum, "2.100000");
  assert.equal(review.deadline, new Date(Number(quoted.batch.quote.deadline) * 1000).toUTCString());
  assert.equal(review.consent, copyEntry("gas.sidiora.consent").message
    .replace("{amount}", review.amount).replace("{maximum}", review.maximum).replace("{deadline}", review.deadline));
  assert.equal(wallet.sidioraAmount(1n), "0.000001");
  assert.equal(wallet.sidioraAmount(123456789012345678901234567890n), "123456789012345678901234.567890");
});

test("missing consent cancels before looking for a provider and changed consent is refused", async () => {
  const quoted = quoteFixture();
  const review = wallet.sidioraQuotePresentation(quoted);
  assert.ok(review);
  assert.deepEqual(await wallet.sendWalletSponsoredBatch(quoted, undefined), { outcome: "cancelled" });
  for (const changed of [
    { ...quoted, batch: { ...quoted.batch, nonce: 1n } },
    { ...quoted, batch: { ...quoted.batch, quote: { ...quoted.batch.quote, tokenAmount: 2_000_002n } } },
    { ...quoted, batch: { ...quoted.batch, calls: [] } },
    { ...quoted, paymaster: quoted.batch.account },
  ]) {
    assert.deepEqual(await wallet.sendWalletSponsoredBatch(changed, review.identity), { outcome: "rejected" });
  }
  assert.deepEqual(await wallet.sendWalletSponsoredBatch(quoted, review.identity), { outcome: "unavailable" });
});

test("expired, excessive and wrong-token quotes cannot reach signing", async () => {
  const quoted = quoteFixture();
  const review = wallet.sidioraQuotePresentation(quoted);
  assert.ok(review);
  for (const changes of [
    { deadline: 0n }, { maxTokenAmount: 1n }, { tokenAmount: 0n },
    { token: quoted.batch.account }, { decimals: 18 },
  ]) {
    const changed = { ...quoted, batch: { ...quoted.batch, quote: { ...quoted.batch.quote, ...changes } } };
    assert.equal(wallet.sidioraQuotePresentation(changed), undefined);
    assert.deepEqual(await wallet.sendWalletSponsoredBatch(changed, review.identity), { outcome: "rejected" });
  }
});

test("absent station configuration returns the wallet unavailable shape without a request", async () => {
  const previous = process.env.PAXEER_GAS_STATION_QUOTE_URL;
  delete process.env.PAXEER_GAS_STATION_QUOTE_URL;
  try {
    const quoted = quoteFixture();
    const outcome = await sdk.requestSidioraGasQuote({
      account: quoted.batch.account, did: `did:layerx:${"55".repeat(32)}`, nonce: quoted.batch.nonce, calls: quoted.batch.calls,
      maxTokenAmount: quoted.batch.quote.maxTokenAmount, gasCost: quoted.batch.quote.gasCost,
    }, quoted.batch.chainId);
    assert.deepEqual(outcome, { ok: false, failure: { outcome: "unavailable" } });
    assert.deepEqual(await sdk.sendSponsoredBatch(quoted, "0x", 0n, "0x"), { outcome: "unavailable" });
  } finally {
    if (previous === undefined) delete process.env.PAXEER_GAS_STATION_QUOTE_URL;
    else process.env.PAXEER_GAS_STATION_QUOTE_URL = previous;
  }
});

test("provider refusals retain the existing wallet outcome vocabulary", () => {
  for (const [code, outcome] of [[4001, "cancelled"], [4100, "rejected"], [4900, "unavailable"], [4901, "unavailable"]] as const) {
    assert.deepEqual(wallet.walletSendFailure({ code }), { outcome });
  }
  assert.deepEqual(wallet.walletSendFailure(null), { outcome: "failed" });
});

test("the settings controller drives the real fee selection and renders catalogue copy", () => {
  const controller = new WalletFeeController();
  const initial = controller.choose("paxeer", "");
  const onChange = controller.choose.bind(controller);
  const native = renderToStaticMarkup(createElement(WalletFeeChoice, { snapshot: initial, onChange }));
  assert.ok(native.includes(copyEntry("gas.sidiora.paxeer").message));
  assert.ok(native.includes(copyEntry("gas.sidiora.choose").message));
  assert.ok(native.includes('role="radiogroup"'));
  assert.equal(native.split('role="radio"').length - 1, 2);
  assert.ok(native.includes('aria-checked="true"'));
  for (const rendered of native.split("<input").slice(1)) assert.match(rendered, /aria-hidden="true"/u);
  assert.doesNotMatch(native, /<select/u);
  try {
    const selected = controller.choose("sidiora", "2.100001");
    assert.equal(selected.valid, true);
    assert.deepEqual(wallet.walletFeePreference(), { currency: "sidiora", maximum: 2_100_001n });
    const html = renderToStaticMarkup(createElement(WalletFeeChoice, { snapshot: selected, onChange }));
    assert.ok(html.includes(copyEntry("gas.sidiora.maximum").message));
    assert.ok(html.includes(copyEntry("gas.sidiora.review").message));
    assert.ok(html.includes('value="2.100001"'));
    assert.match(html, /<label for="[^"]+"/u);
    assert.doesNotMatch(html, /aria-invalid="true"/u);
    for (const maximum of ["", "0", "-1", "1e3", "0.0000001"]) {
      const invalid = controller.choose("sidiora", maximum);
      assert.equal(invalid.valid, false);
      assert.deepEqual(wallet.walletFeePreference(), { currency: "sidiora" });
      const error = renderToStaticMarkup(createElement(WalletFeeChoice, { snapshot: invalid, onChange }));
      assert.ok(error.includes('role="alert"'));
      assert.ok(error.includes(copyEntry("gas.sidiora.invalid").message));
      assert.ok(error.includes('aria-invalid="true"'));
      assert.match(error, /aria-describedby="[^"]+"/u);
    }
  } finally {
    controller.choose("paxeer", "");
  }
});

test("Sidiora copy is generated from the catalogue and remains within vocabulary rules", () => {
  const entries = copyEntries.filter((entry) => entry.key.startsWith("gas.sidiora."));
  assert.ok(entries.length > 0);
  for (const entry of entries) {
    assert.equal(copyEntry(entry.key).message, entry.message);
    assert.equal(entry.surface, "default");
    assert.equal(entry.moneyAdjacent, true);
    for (const banned of BANNED_VOCABULARY) assert.doesNotMatch(entry.message, new RegExp(`\\b${banned}\\b`, "iu"));
  }
  const scripts = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8")) as { scripts: Record<string, string> };
  for (const name of ["test", "test:component"]) assert.ok(scripts.scripts[name]?.includes("e2e/gas-station.test.ts"));
});


test("private sponsorship carries exact SDK constructions and binds every approved quote field", async () => {
  const { PaxeerProvider, constructionDigest, toWireConstruction } = await import("@paxeer/wallet/provider");
  const quoted = quoteFixture();
  const provider = new PaxeerProvider({ gatewayUrl: "http://127.0.0.1:1", rpcUrl: "http://127.0.0.1:1", token: () => null, chainId: Number(quoted.batch.chainId) });
  const module = wallet.sponsoredWalletModule(quoted, provider);
  const construction = module.construction(quoted.batch);
  assert.deepEqual(construction, {
    kind: "sponsored_batch", chainId: "1325", account: quoted.batch.account, nonce: "0",
    calls: [{ to: quoted.batch.calls[0]!.to, value: "0", data: "0x1234" }],
    quote: { sponsor: quoted.batch.quote.sponsor, token: sdk.SIDIORA_TOKEN.toLowerCase(),
      maxTokenAmount: "2100000", tokenAmount: "2000001", deadline: quoted.batch.quote.deadline.toString(), quoteNonce: "7", gasCost: (10n ** 18n).toString() },
  });
  const digest = sdk.sponsoredBatchDigest(quoted.batch);
  assert(digest.ok);
  assert.equal(constructionDigest(construction), digest.value);
  const authorization = toWireConstruction({ kind: "eip7702_authorization", chainId: 1325n, address: quoted.paymaster as `0x${string}`, nonce: 8n });
  assert.deepEqual(authorization, { kind: "eip7702_authorization", chainId: "1325", address: quoted.paymaster, nonce: "8" });
  const authDigest = sdk.eip7702AuthorizationDigest({ chainId: 1325n, address: quoted.paymaster, nonce: 8n });
  assert(authDigest.ok);
  assert.equal(constructionDigest(authorization), authDigest.value);
  assert.throws(() => toWireConstruction({ ...construction, kind: "bare_digest" } as never));
  assert.throws(() => toWireConstruction({ ...construction, domain: "other" } as never));
  const review = wallet.sidioraQuotePresentation(quoted)!;
  for (const changed of [
    { ...quoted, batch: { ...quoted.batch, chainId: 125n } },
    { ...quoted, batch: { ...quoted.batch, account: quoted.paymaster } },
    { ...quoted, batch: { ...quoted.batch, calls: [{ ...quoted.batch.calls[0]!, to: quoted.paymaster }] } },
    { ...quoted, batch: { ...quoted.batch, calls: [{ ...quoted.batch.calls[0]!, data: "0x1235" }] } },
    { ...quoted, batch: { ...quoted.batch, calls: [{ ...quoted.batch.calls[0]!, value: 1n }] } },
    { ...quoted, batch: { ...quoted.batch, quote: { ...quoted.batch.quote, sponsor: quoted.paymaster } } },
    { ...quoted, batch: { ...quoted.batch, quote: { ...quoted.batch.quote, quoteNonce: 8n } } },
    { ...quoted, batch: { ...quoted.batch, quote: { ...quoted.batch.quote, deadline: quoted.batch.quote.deadline + 1n } } },
    { ...quoted, relayerSignature: "0x01" },
  ]) assert.deepEqual(await wallet.sendWalletSponsoredBatch(changed, review.identity), { outcome: "rejected" });
  assert.equal(wallet.sidioraQuotePresentation(quoted, quoted.batch.quote.deadline), undefined);
  await assert.rejects(provider.request({ method: "eth_sign", params: [quoted.batch.account, digest.value] }));
  assert.deepEqual(await wallet.resumeWalletSponsoredSubmission(quoted.batch.account), { outcome: "unavailable" });
});

class MemoryStorage {
  readonly #items = new Map<string, string>();
  getItem(key: string): string | null { return this.#items.get(key) ?? null; }
  setItem(key: string, value: string): void { this.#items.set(key, value); }
  removeItem(key: string): void { this.#items.delete(key); }
}

const ZERO_WORD = `0x${"0".repeat(64)}`;

function walletWindow(answer: (method: string, params: readonly unknown[]) => unknown) {
  const methods: string[] = [];
  const paxeer = {
    isPaxeer: true,
    request: async ({ method, params = [] }: { method: string; params?: readonly unknown[] }) => {
      methods.push(method);
      return answer(method, params);
    },
  };
  Object.assign(globalThis, { window: { paxeer, localStorage: new MemoryStorage(), sessionStorage: new MemoryStorage(), confirm: () => true } });
  return methods;
}

test("a write never resumes another call's retained sponsored submission as its own", async () => {
  const { privateKeyToAccount } = await import("viem/accounts");
  const signer = privateKeyToAccount(`0x${"0b".repeat(32)}`);
  const account = signer.address.toLowerCase();
  const fixture = quoteFixture();
  const approveCall = { to: `0x${"66".repeat(20)}`, value: 0n, data: "0x095ea7b3" };
  const writeCall = { to: `0x${"77".repeat(20)}`, value: 0n, data: "0x12345678" };
  const approveHash = `0x${"aa".repeat(32)}`;
  const relayer = privateKeyToAccount(`0x${"0c".repeat(32)}`);
  const quote = { ...fixture.batch.quote, sponsor: relayer.address.toLowerCase() };
  const quoteDigest = sdk.gasQuoteDigest(fixture.batch.chainId, account, quote);
  assert.ok(quoteDigest.ok);
  const quoted: SidioraGasQuote = {
    ...fixture, batch: { ...fixture.batch, account, calls: [approveCall], quote },
    relayerSignature: await relayer.sign({ hash: quoteDigest.value as `0x${string}` }),
  };
  const pendingReport = { status: "pending", tx_hash: approveHash, station: null };
  const methods = walletWindow(async (method, params) => {
    switch (method) {
      case "eth_accounts": return [account];
      case "eth_chainId": return `0x${quoted.batch.chainId.toString(16)}`;
      case "eth_getCode": return `0xef0100${quoted.paymaster.slice(2)}`;
      case "eth_call": return ZERO_WORD;
      case "eth_getTransactionCount": return "0x0";
      case "eth_sign": return signer.sign({ hash: params[1] as `0x${string}` });
      case "paxeer_submitSponsored": case "paxeer_sponsoredStatus": return pendingReport;
      default: throw new Error(`unexpected ${method}`);
    }
  });
  try {
    const review = wallet.sidioraQuotePresentation(quoted);
    assert.ok(review);
    assert.deepEqual(await wallet.sendWalletSponsoredBatch(quoted, review.identity), { outcome: "sent", transactionHash: approveHash });
    assert.deepEqual(await wallet.resumeWalletSponsoredSubmission(account, writeCall), { outcome: "unavailable" });
    assert.deepEqual(await wallet.resumeWalletSponsoredSubmission(account, approveCall), { outcome: "sent", transactionHash: approveHash });
    assert.equal(wallet.chooseWalletFee("sidiora", "2.1"), true);
    methods.length = 0;
    assert.deepEqual(await wallet.sendWalletPrecompileCall(account, () => writeCall), { outcome: "unavailable" });
    assert.ok(!methods.includes("eth_estimateGas"));
    assert.ok(!methods.includes("paxeer_submitSponsored"));
  } finally {
    wallet.chooseWalletFee("paxeer", "");
    delete (globalThis as { window?: unknown }).window;
  }
});

test("the launchpad refuses an approve under the sponsored SID fee before sending anything", async () => {
  const launchpad = await import("../src/app/launchpad/launchpad-client.tsx");
  const { SendOutcome } = await import("../src/app/_markets/send-outcome.tsx");
  const account = `0x${"11".repeat(20)}`;
  const usdl = `0x${"88".repeat(20)}`;
  const methods = walletWindow(async (method, params) => {
    if (method !== "eth_call") throw new Error(`unexpected ${method}`);
    const call = params[0] as { to: string };
    return call.to.toLowerCase() === usdl ? ZERO_WORD : `0x${"0".repeat(24)}${usdl.slice(2)}`;
  });
  const writes: unknown[] = [];
  const approving: boolean[] = [];
  try {
    assert.equal(wallet.chooseWalletFee("sidiora", "2.1"), true);
    const order = { pool: `0x${"99".repeat(20)}`, amountIn: 5_000_000n, minOut: 1n, deadline: 2_000_000_000n };
    const outcome = await launchpad.sendWithApproval(account, { kind: "buy", order }, () => {
      writes.push(order);
      return sdk.launchpadBuyCall(order);
    }, (active) => approving.push(active));
    assert.deepEqual(outcome, { outcome: "failed", detail: launchpad.SPONSORED_APPROVE_UNSUPPORTED });
    assert.deepEqual(writes, []);
    assert.ok(methods.every((method) => method === "eth_call"));
    assert.ok(!approving.includes(true));
    const html = renderToStaticMarkup(createElement(SendOutcome, { outcome }));
    assert.doesNotMatch(html, /Sent\./u);
    assert.ok(html.includes(launchpad.SPONSORED_APPROVE_UNSUPPORTED));
  } finally {
    wallet.chooseWalletFee("paxeer", "");
    delete (globalThis as { window?: unknown }).window;
  }
});
