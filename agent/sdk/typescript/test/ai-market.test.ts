import { once } from "node:events";
import { mkdtemp, readFile, writeFile } from "node:fs/promises";
import * as http from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { ed25519 } from "@noble/curves/ed25519.js";
import { sha256 } from "@noble/hashes/sha2.js";

import {
  AgentHttpTransport, LayerXKeyCredential, ProductionClient, ProgramOperations, ProgramTrustContext, SecretBytes,
  type NativePrepareResultV1,
} from "../src/index.js";
import {
  AiMarketError, AiMarketViews, CLAIM, FUND, OperationJournal, OperationRecord, PAXAI_LIMITS, REFUND_FREE,
  activityIdOf, decodeProgramCallActivity, decodeRequestEnvelope, encodeClaimRequest, encodeFundRequest,
  encodeProgramCallActivity, encodeRefundRequest, encodeRequestEnvelope, encodeSnapshotBinding, freshnessOf,
  parseDecimalU128, parseDecimalU64, prepareOperation, requestIntent, requireCurrent, requireRetainedEpoch,
  signingPreimage, snapshotIdOf,
  type AiMarketErrorDetail, type ApprovalTerms, type MarketView, type NativeTerms, type OperationRequest,
  type ParticipantPage, type SnapshotBinding,
} from "../src/ai_market.js";

function assert(condition: boolean, message: string): asserts condition {
  if (!condition) throw new Error(message);
}

let passed = 0;
let failed = 0;
async function check(name: string, body: () => Promise<void> | void): Promise<void> {
  try { await body(); passed += 1; console.log(`ok ${name}`); } catch (error) {
    failed += 1;
    console.log(`FAIL ${name}: ${error instanceof Error ? `${error.message}${error.cause === undefined ? "" : ` (${String(error.cause)})`}` : String(error)}`);
    if (error instanceof AiMarketError && error.detail.cause !== undefined) console.log(`  cause: ${String(error.detail.cause)}`);
  }
}

async function refuses(code: AiMarketError["code"], action: () => unknown, detail: Partial<AiMarketErrorDetail> = {}): Promise<AiMarketError> {
  let failure: unknown;
  try { await action(); } catch (error) { failure = error; }
  assert(failure instanceof AiMarketError, `expected ${code}, got ${failure instanceof Error ? failure.message : String(failure)}`);
  assert(failure.code === code, `expected ${code}, got ${failure.message}`);
  for (const [key, value] of Object.entries(detail)) {
    assert(failure.detail[key as keyof AiMarketErrorDetail] === value, `expected ${key}=${String(value)}, got ${String(failure.detail[key as keyof AiMarketErrorDetail])}`);
  }
  return failure;
}

const id = (byte: number): string => byte.toString(16).padStart(2, "0").repeat(32);
const hex = (bytes: Uint8Array): string => Buffer.from(bytes).toString("hex");
const raw = (value: string): Uint8Array => new Uint8Array(Buffer.from(value, "hex"));
const be = (value: bigint, length: number): Buffer => {
  const out = Buffer.alloc(length);
  let rest = value;
  for (let index = length - 1; index >= 0; index -= 1) { out[index] = Number(rest & 0xffn); rest >>= 8n; }
  return out;
};
const U128_MAX = (1n << 128n) - 1n;

// ----- Schema fixtures, encoded independently of the client -------------------------------------

function binding(changes: Partial<SnapshotBinding> = {}): SnapshotBinding {
  return {
    chain: id(0x01), program: id(0x02), market: id(0x03), observedSequence: 1_000n, executionHeight: 500n, batchId: id(0x04),
    nativeStateRoot: id(0x05), revision: 7n, stateDigest: id(0x06), epoch: 3n, config: 2n, policy: id(0x07), roster: id(0x08),
    checkpoint: id(0x09), settlement: null, rank: 4, publicationTimeMs: 1_700_000_000_000n, ...changes,
  };
}

function bindingPrefix(b: SnapshotBinding): Buffer {
  const presence = (value: Buffer | null): Buffer => value === null ? Buffer.from([0]) : Buffer.concat([Buffer.from([1]), value]);
  return Buffer.concat([be(1n, 2), raw(b.chain), raw(b.program), raw(b.market), be(b.observedSequence, 8), be(b.executionHeight, 8),
    raw(b.batchId), raw(b.nativeStateRoot), be(b.revision, 8), raw(b.stateDigest), presence(b.epoch === null ? null : be(b.epoch, 8)),
    be(b.config, 8), raw(b.policy), presence(b.roster === null ? null : Buffer.from(raw(b.roster)))]);
}

function snapshotId(b: SnapshotBinding): string {
  return hex(sha256(Buffer.concat([Buffer.from("PAXAI/view/v1", "ascii"), Buffer.from([0]), bindingPrefix(b)])));
}

const x = (value: string): string => `0x${value}`;
function bindingJson(b: SnapshotBinding): Record<string, unknown> {
  return {
    chain: x(b.chain), program: x(b.program), market: x(b.market), observed_sequence: b.observedSequence.toString(),
    execution_height: b.executionHeight.toString(), batch_id: x(b.batchId), native_state_root: x(b.nativeStateRoot),
    revision: b.revision.toString(), state_digest: x(b.stateDigest), epoch: b.epoch === null ? null : b.epoch.toString(),
    config: b.config.toString(), policy: x(b.policy), roster: b.roster === null ? null : x(b.roster), checkpoint: x(b.checkpoint),
    settlement: b.settlement === null ? null : x(b.settlement), rank: b.rank, publication_time_ms: b.publicationTimeMs.toString(),
  };
}

function components(f06 = "available"): Record<string, string> {
  return { F01: "available", F02: "available", F03: "available", F04: "available", F05: "available", F06: f06,
    F07: "not-enabled", F08: "available", F09: "not-yet-produced", F10: "available" };
}

type Row = Record<string, unknown>;
function workerRow(byte: number, changes: Row = {}): Row {
  return {
    kind: "worker", id: x(id(byte)), owner: x(id(0xa0)), generation: "1", identity_state: 1, frozen_member: true,
    frozen_generation: "1", eligibility: 1, metadata: null, metadata_revision: "0",
    score: { status: "present", epoch: "3", ppm: 250_000 },
    reward: { status: "available", asset: x(id(0xa1)), earned: "1000", claimed: "0" },
    history: { status: "not-enabled", digest: null }, ...changes,
  };
}

function viewJson(b: SnapshotBinding, served = snapshotId(b)): Record<string, unknown> {
  return { snapshot_id: x(served), projection: "finalized-publishable", binding: bindingJson(b), components: components(),
    source_activity: x(id(0x0a)), freshness: { label: "current" } };
}

function pageJson(b: SnapshotBinding, rows: readonly Row[], cursor: string | null, f06 = "available"): Record<string, unknown> {
  return { snapshot_id: x(snapshotId(b)), binding: bindingJson(b), components: components(f06), rows, cursor };
}

// ----- Local gateway ----------------------------------------------------------------------------

interface Served { readonly status?: number; readonly body: unknown; readonly contentType?: string }
let route: (url: URL) => Served = () => ({ status: 500, body: { ok: false, error: { code: "projection-store" } } });
const seen: { url: URL; authorization: string | undefined }[] = [];
const gateway = http.createServer((request, response) => {
  const url = new URL(request.url ?? "/", "http://gateway");
  seen.push({ url, authorization: request.headers.authorization });
  const served = route(url);
  response.writeHead(served.status ?? 200, { "Content-Type": served.contentType ?? "application/json" });
  response.end(JSON.stringify(served.body));
});
gateway.listen(0, "127.0.0.1");
await once(gateway, "listening");
const gatewayAddress = gateway.address();
assert(gatewayAddress !== null && typeof gatewayAddress === "object", "gateway listener missing");
const endpoint = `http://127.0.0.1:${gatewayAddress.port}`;
const ok = (result: unknown): Served => ({ body: { ok: true, result } });
const fail = (status: number, error: Record<string, unknown>): Served => ({ status, body: { ok: false, error } });
const credential = (secret: number): LayerXKeyCredential => new LayerXKeyCredential("key_1",
  new SecretBytes(new TextEncoder().encode(`lxp_live_${secret.toString(16).padStart(2, "0").repeat(32)}`)));
const views = new AiMarketViews({ endpoint, credential: credential(0x22) });
const market = id(0x03);

// ----- Owner keys and the conformance fixture ---------------------------------------------------

const ownerSecret = Uint8Array.from({ length: 32 }, (_value, index) => index + 1);
const ownerKey = ed25519.getPublicKey(ownerSecret);
const workerSecret = Uint8Array.from({ length: 32 }, (_value, index) => 0x80 + index);
const workerKey = ed25519.getPublicKey(workerSecret);
const owner = { publicKey: ownerKey, sign: (preimage: Uint8Array) => ed25519.sign(preimage, ownerSecret) };
const fixture = JSON.parse(await readFile(new URL("../../../../../platform/sdk/conformance/fixtures/native-program-call-v3.json", import.meta.url), "utf8")) as
  { payload_hex: string; signed_activity_hex: string; activity_id_hex: string; public_key_hex: string; idempotency_key_hex: string; fee_limit: string };

const terms = (changes: Partial<NativeTerms> = {}): NativeTerms => ({
  networkId: 7, actorDid: "did:lxp:ai-market-owner", ownerPublicKey: ownerKey, accountSequence: 4n, idempotencyKey: id(0x44),
  notBefore: 1n, notAfter: 100n, feeLimit: 1_000n, capabilities: new Uint8Array(), accessDeclaration: new Uint8Array(),
  responseCapacity: 4_096, resources: [1_000_000n, 16_777_216n, 1_048_576n, 1_048_576n, 64n, 1_048_576n, 4_096n], ...changes,
});

const finalized: MarketView = { snapshotId: snapshotId(binding()), binding: binding(), projection: "finalized-publishable",
  components: { F01: "available", F02: "available", F03: "available", F04: "available", F05: "available", F06: "available",
    F07: "not-enabled", F08: "available", F09: "not-yet-produced", F10: "available" },
  sourceActivity: id(0x0a), freshness: { label: "current" } };

const claimPayload = (amount: bigint, worker = id(0x11)): Uint8Array => encodeClaimRequest({ worker, recipient: id(0xb0), amount });
const claimRequest = (changes: Partial<OperationRequest> = {}): OperationRequest => ({
  selector: CLAIM, payload: claimPayload(U128_MAX), actor: id(0xc0), roster: "bound", sequence: 0n, expiry: 900n,
  request: id(0xd0), requiredRank: 4, ...changes,
});

async function entitlements(): Promise<ParticipantPage> {
  route = () => ok(pageJson(binding(), [workerRow(0x11, { reward: { status: "available", asset: x(id(0xa1)), earned: U128_MAX.toString(), claimed: "0" } })], null));
  return await views.participants({ market, kind: "worker" });
}

function disclosure(canonical: Uint8Array): NativePrepareResultV1 {
  return { version: "1", preparation_id: hex(sha256(canonical)), canonical_bytes: hex(canonical), signing_preimage: hex(signingPreimage(canonical)),
    activity: { version: "1", module: "9", ordinal: "3" }, approval_required: false, approval_id: null };
}

try {
  // ----- AI.F10-A01: snapshot pinning across a newer snapshot ---------------------------------

  await check("A01 binding bytes and snapshot id match the schema layout", () => {
    const b = binding();
    const expected = Buffer.concat([bindingPrefix(b), raw(b.checkpoint), Buffer.from([0]), Buffer.from([4]), be(b.publicationTimeMs, 8), Buffer.alloc(8)]);
    assert(hex(encodeSnapshotBinding(b)) === expected.toString("hex"), "binding bytes differ from the schema layout");
    assert(snapshotIdOf(b) === snapshotId(b), "snapshot id differs from H(PAXAI/view/v1, prefix)");
    assert(snapshotIdOf({ ...b, rank: 3, checkpoint: id(0x19), publicationTimeMs: 1n }) === snapshotId(b), "finality evidence changed the content id");
    assert(snapshotIdOf({ ...b, revision: 8n }) !== snapshotId(b), "revision did not change the content id");
  });

  await check("A01 snapshot view binds identity, market and finality", async () => {
    route = () => ok(viewJson(binding()));
    const view = await views.snapshot(market);
    assert(view.snapshotId === snapshotId(binding()) && view.binding.revision === 7n && view.binding.epoch === 3n, "view binding not decoded exactly");
    assert(seen.at(-1)?.authorization === `LayerX-Key key_1:lxp_live_${"22".repeat(32)}`, "credential not presented");
    assert(seen.at(-1)?.url.pathname === `/v1/ai/markets/${market}/snapshot`, "wrong snapshot route");
    route = () => ok(viewJson(binding(), id(0x77)));
    await refuses("IntegrityFailure", () => views.snapshot(market));
    route = () => ok(viewJson(binding({ market: id(0x33) })));
    await refuses("BindingMismatch", () => views.snapshot(market));
    route = () => ok(viewJson(binding({ rank: 3 })));
    await refuses("FinalityUnavailable", () => views.snapshot(market));
  });

  await check("A01 pages stay on the first snapshot after a newer one publishes", async () => {
    const first = binding();
    const newer = binding({ revision: 8n, stateDigest: id(0x16), observedSequence: 1_010n, executionHeight: 505n });
    const token = "ab".repeat(176);
    route = (url) => url.searchParams.has("cursor")
      ? (url.searchParams.get("snapshot") === snapshotId(first) && url.searchParams.get("cursor") === token
        ? ok(pageJson(first, [workerRow(0x12)], null)) : ok(pageJson(newer, [workerRow(0x13)], null)))
      : ok(pageJson(first, [workerRow(0x11)], token));
    const page1 = await views.participants({ market, kind: "worker", limit: 1 });
    assert(page1.next !== null && page1.next.snapshotId === snapshotId(first), "cursor not pinned to the first snapshot");
    const page2 = await views.participants({ market, kind: "worker", limit: 1 }, page1.next);
    const ids = [...page1.rows, ...page2.rows].map((row) => row.id);
    assert(ids.join() === [id(0x11), id(0x12)].join() && page2.next === null, `rows not returned once each: ${ids.join()}`);
    const request = seen.at(-1)?.url;
    assert(request?.searchParams.get("snapshot") === snapshotId(first) && request.searchParams.get("cursor") === token, "follow-up not pinned");
    route = () => ok(pageJson(newer, [workerRow(0x12)], null));
    await refuses("SnapshotConflict", () => views.participants({ market, kind: "worker", limit: 1 }, page1.next ?? undefined));
    await refuses("SnapshotConflict", () => views.participants({ market, snapshot: snapshotId(first) }));
  });

  await check("A01 cursors are bound to filter and reader before any request", async () => {
    route = () => ok(pageJson(binding(), [workerRow(0x11)], "cd".repeat(176)));
    const page = await views.participants({ market, kind: "worker", limit: 1 });
    assert(page.next !== null, "cursor missing");
    const before = seen.length;
    await refuses("CursorMismatch", () => views.participants({ market, kind: "evaluator", limit: 1 }, page.next ?? undefined));
    await refuses("CursorMismatch", () => views.participants({ market, kind: "worker", activeOnly: true, limit: 1 }, page.next ?? undefined));
    await refuses("CursorMismatch", () => views.participants({ market: id(0x33), kind: "worker", limit: 1 }, page.next ?? undefined));
    const otherReader = new AiMarketViews({ endpoint, credential: credential(0x23) });
    await refuses("CursorMismatch", () => otherReader.participants({ market, kind: "worker", limit: 1 }, page.next ?? undefined));
    assert(seen.length === before, "a mismatched cursor reached the gateway");
    route = () => fail(409, { code: "cursor-mismatch" });
    await refuses("CursorMismatch", () => views.participants({ market, kind: "worker", limit: 1 }, page.next ?? undefined));
    route = () => fail(410, { code: "cursor-expired" });
    await refuses("CursorExpired", () => views.participants({ market, kind: "worker", limit: 1 }, page.next ?? undefined));
  });

  await check("A01 freshness labels and the stale-authority bound", async () => {
    const b = binding();
    assert(freshnessOf(b, 508n).label === "current", "lag 8 is current");
    const stale = freshnessOf(b, 509n);
    assert(stale.label === "stale" && stale.lag === 9n, "lag 9 is stale");
    assert(freshnessOf(b, null).label === "unknown", "no authority is unknown");
    requireCurrent(b, 508n);
    await refuses("StaleAuthority", () => requireCurrent(b, 509n), { lag: 9n });
  });

  // ----- AI.F10-A03: score and reward absence --------------------------------------------------

  await check("A03 a zero score is distinct from an absent score", async () => {
    route = () => ok(pageJson(binding(), [
      workerRow(0x11, { score: { status: "present", epoch: "3", ppm: 0 } }),
      workerRow(0x12, { score: { status: "insufficient-coverage", epoch: null, ppm: null } }),
      workerRow(0x13, { score: { status: "no-admissible-score", epoch: "3", ppm: null } }),
    ], null));
    const rows = (await views.participants({ market })).rows;
    assert(rows[0]?.score.status === "present" && rows[0].score.ppm === 0 && rows[0].score.epoch === 3n, "zero score lost");
    assert(rows[1]?.score.status === "insufficient-coverage" && rows[1].score.ppm === null, "absent score became a value");
    assert(rows[2]?.score.status === "no-admissible-score" && rows[2].score.ppm === null, "no-admissible score became a value");
  });

  await check("A03 rewards keep exact u128 values and explicit absence", async () => {
    route = () => ok(pageJson(binding(), [
      workerRow(0x11, { reward: { status: "available", asset: x(id(0xa1)), earned: U128_MAX.toString(), claimed: (U128_MAX - 1n).toString() } }),
    ], null));
    const reward = (await views.participants({ market })).rows[0]?.reward;
    assert(reward?.earned === U128_MAX && reward.claimed === U128_MAX - 1n && reward.asset === id(0xa1), "u128 reward not exact");
    route = () => ok(pageJson(binding(), [workerRow(0x11, { reward: { status: "not-enabled", asset: null, earned: null, claimed: null } })], null, "not-enabled"));
    const absent = (await views.participants({ market })).rows[0]?.reward;
    assert(absent?.status === "not-enabled" && absent.earned === null && absent.claimed === null, "absent reward became zero");
  });

  await check("A03 row invariants refuse inconsistent projections", async () => {
    const cases: readonly [AiMarketError["code"], Row, string?][] = [
      ["IntegrityFailure", { score: { status: "present", epoch: "3", ppm: null } }],
      ["IntegrityFailure", { score: { status: "unavailable", epoch: null, ppm: 0 } }],
      ["InvalidEncoding", { score: { status: "present", epoch: "3", ppm: 1_000_001 } }],
      ["IntegrityFailure", { reward: { status: "available", asset: x(id(0xa1)), earned: "1", claimed: "2" } }],
      ["IntegrityFailure", { reward: { status: "not-enabled", asset: null, earned: "0", claimed: null } }],
      ["IntegrityFailure", {}, "not-enabled"],
      ["InvalidEncoding", { reward: { status: "available", asset: x(id(0xa1)), earned: (U128_MAX + 1n).toString(), claimed: "0" } }],
      ["InvalidEncoding", { reward: { status: "available", asset: x(id(0xa1)), earned: "01", claimed: "0" } }],
      ["InvalidEncoding", { reward: { status: "available", asset: x(id(0xa1)), earned: 1000, claimed: "0" } }],
      ["InvalidEncoding", { id: id(0x11) }],
      ["InvalidEncoding", { extra: true }],
    ];
    for (const [code, changes, f06] of cases) {
      route = () => ok(pageJson(binding(), [workerRow(0x11, changes)], null, f06));
      await refuses(code, () => views.participants({ market }));
    }
  });

  await check("A03 epoch history separates retained, never-opened and archived epochs", async () => {
    route = (url) => url.searchParams.get("from") === "2" && url.searchParams.get("limit") === "4" ? ok({ component: "available", entries: [
      { epoch: "2", status: "never-opened", snapshot_id: null },
      { epoch: "3", status: "retained", snapshot_id: x(id(0x31)) },
      { epoch: "4", status: "archive-required", snapshot_id: x(id(0x32)) },
    ] }) : fail(400, { code: "invalid-encoding" });
    const page = await views.epochs(market, 2n, 4);
    const [never, retained, archived] = page.entries;
    assert(never !== undefined && retained !== undefined && archived !== undefined && page.entries.length === 3, "epochs missing");
    assert(requireRetainedEpoch(retained) === id(0x31), "retained epoch source lost");
    await refuses("HistoryUnavailable", () => requireRetainedEpoch(never), { epochStatus: "never-opened" });
    await refuses("HistoryUnavailable", () => requireRetainedEpoch(archived), { epochStatus: "archive-required" });
    route = () => ok({ component: "available", entries: [{ epoch: "3", status: "retained", snapshot_id: x(id(0x31)) }, { epoch: "3", status: "retained", snapshot_id: x(id(0x31)) }] });
    await refuses("IntegrityFailure", () => views.epochs(market, 2n, 4));
    route = () => ok({ component: "available", entries: [{ epoch: "3", status: "never-opened", snapshot_id: x(id(0x31)) }] });
    await refuses("IntegrityFailure", () => views.epochs(market, 2n, 4));
  });

  // ----- AI.F10-A04: bounded pagination ------------------------------------------------------

  await check("A04 forty rows arrive in two bounded pages", async () => {
    const all = Array.from({ length: 40 }, (_value, index) => workerRow(0x40 + index));
    const tokenAt = (offset: number): string => offset.toString(16).padStart(352, "0");
    route = (url) => {
      const cursor = url.searchParams.get("cursor");
      const limit = Number(url.searchParams.get("limit"));
      const start = cursor === null ? 0 : Number.parseInt(cursor, 16);
      const end = Math.min(start + limit, all.length);
      return ok(pageJson(binding(), all.slice(start, end), end < all.length ? tokenAt(end) : null));
    };
    const first = await views.participants({ market, limit: PAXAI_LIMITS.maxPageRows });
    assert(first.rows.length === 32 && first.next !== null, "first page not bounded at 32");
    const second = await views.participants({ market, limit: PAXAI_LIMITS.maxPageRows }, first.next);
    assert(second.rows.length === 8 && second.next === null, "second page not the remaining 8");
    const ids = [...first.rows, ...second.rows].map((row) => row.id);
    assert(new Set(ids).size === 40 && ids.every((value, index) => value === id(0x40 + index)), "rows not unique and ordered");
  });

  await check("A04 invalid limits, markets and decimals refuse before any request", async () => {
    const before = seen.length;
    for (const limit of [0, 33, 1.5]) await refuses("InvalidEncoding", () => views.participants({ market, limit }));
    await refuses("InvalidEncoding", () => views.participants({ market: `${market}0` }));
    await refuses("InvalidEncoding", () => views.participants({ market: market.toUpperCase().replace(/^0/u, "A") }));
    await refuses("InvalidEncoding", () => views.participants({ market: id(0x00) }));
    await refuses("InvalidEncoding", () => views.epochs(market, 0n, 33));
    await refuses("InvalidEncoding", () => views.snapshot(`0x${market}`));
    assert(seen.length === before, "an invalid query reached the gateway");
    await refuses("InvalidEncoding", () => parseDecimalU64("01"));
    await refuses("InvalidEncoding", () => parseDecimalU64("18446744073709551616"));
    await refuses("InvalidEncoding", () => parseDecimalU64("-1"));
    assert(parseDecimalU64("18446744073709551615") === 0xffff_ffff_ffff_ffffn && parseDecimalU128(U128_MAX.toString()) === U128_MAX, "max decimals lost");
    await refuses("InvalidEncoding", () => parseDecimalU128((U128_MAX + 1n).toString()));
  });

  await check("A04 oversized, unordered or filtered-out pages refuse", async () => {
    route = () => ok(pageJson(binding(), Array.from({ length: 33 }, (_value, index) => workerRow(0x40 + index)), null));
    await refuses("InvalidEncoding", () => views.participants({ market, limit: 32 }));
    route = () => ok(pageJson(binding(), [workerRow(0x11)], "ab".repeat(513)));
    await refuses("ResponseTooLarge", () => views.participants({ market }));
    route = () => ok(pageJson(binding(), [workerRow(0x12), workerRow(0x11)], null));
    await refuses("IntegrityFailure", () => views.participants({ market }));
    route = () => ok(pageJson(binding(), [{ ...workerRow(0x11), kind: "evaluator" }], null));
    await refuses("IntegrityFailure", () => views.participants({ market, kind: "worker" }));
    route = () => ok(pageJson(binding(), [workerRow(0x11, { eligibility: 0 })], null));
    await refuses("IntegrityFailure", () => views.participants({ market, activeOnly: true }));
    route = () => ok(pageJson(binding(), [], "ab".repeat(176)));
    await refuses("IntegrityFailure", () => views.participants({ market }));
    route = () => ({ body: { ok: true, result: pageJson(binding(), [], null) }, contentType: "text/plain" });
    await refuses("InvalidEncoding", () => views.participants({ market }));
  });

  await check("A04 an empty result is an empty page, and gateway refusals keep their category", async () => {
    route = (url) => url.searchParams.get("kind") === "evaluator" && url.searchParams.get("active_only") === "true"
      && url.searchParams.get("limit") === "16" ? ok(pageJson(binding(), [], null)) : fail(400, { code: "invalid-encoding" });
    const empty = await views.participants({ market, kind: "evaluator", activeOnly: true });
    assert(empty.rows.length === 0 && empty.next === null, "empty result not an empty page");
    route = () => fail(429, { code: "rate-limited" });
    await refuses("Service", () => views.participants({ market }), { category: "rate-limited", status: 429 });
    route = () => fail(503, { code: "authority-stale", lag: "12" });
    await refuses("StaleAuthority", () => views.participants({ market }), { lag: 12n });
    route = () => fail(413, { code: "response-too-large" });
    await refuses("ResponseTooLarge", () => views.participants({ market }));
    route = () => fail(409, { code: "snapshot-conflict" });
    await refuses("SnapshotConflict", () => views.participants({ market }));
  });

  // ----- AI.F10-A07: native operation approval -----------------------------------------------

  await check("A07 activity codec reproduces the native program call conformance fixture", () => {
    const signed = raw(fixture.signed_activity_hex);
    const activity = decodeProgramCallActivity(signed);
    assert(hex(encodeProgramCallActivity(activity)) === fixture.signed_activity_hex, "signed activity bytes not reproduced");
    assert(activity.networkId === 7 && activity.feeLimit === BigInt(fixture.fee_limit) && hex(activity.payload) === fixture.payload_hex
      && hex(activity.idempotencyKey) === fixture.idempotency_key_hex && hex(activity.authority) === fixture.public_key_hex, "activity fields lost");
    assert(activityIdOf(signed) === fixture.activity_id_hex, "activity id differs");
    const unsigned = encodeProgramCallActivity({ ...activity, signature: null });
    assert(activity.signature !== null && ed25519.verify(activity.signature, signingPreimage(unsigned), raw(fixture.public_key_hex)), "fixture signature does not verify over the preimage");
  });

  await check("A07 envelopes enforce sequence, roster and authentication rules", async () => {
    const base = { chain: id(0x01), program: id(0x02), market, actor: id(0xc0), epoch: 3n, config: 2n, roster: id(0x08),
      expiry: 900n, request: id(0xd0), delegated: false };
    const claim = encodeRequestEnvelope({ ...base, selector: CLAIM, sequence: 0n, payload: claimPayload(5n) });
    const decoded = decodeRequestEnvelope(claim);
    assert(decoded.unsigned.length === claim.length - 1 && decoded.envelope.sequence === 0n && decoded.envelope.roster === id(0x08), "claim envelope not canonical");
    assert(requestIntent(decoded.unsigned) === hex(sha256(Buffer.concat([Buffer.from("PAXAI/request/v1\0", "ascii"), decoded.unsigned]))), "intent differs");
    await refuses("Application", () => encodeRequestEnvelope({ ...base, selector: CLAIM, sequence: 1n, payload: claimPayload(5n) }), { applicationCode: 0x0002 });
    const fund = encodeFundRequest({ amount: 10n, refundRecipient: id(0xb1), policyVersion: 1n, consent: true });
    await refuses("Application", () => encodeRequestEnvelope({ ...base, selector: FUND, sequence: 0n, payload: fund }), { applicationCode: 0x0002 });
    encodeRequestEnvelope({ ...base, selector: FUND, sequence: 5n, payload: fund, epoch: 0n, roster: null });
    await refuses("Application", () => encodeRequestEnvelope({ ...base, selector: CLAIM, sequence: 0n, payload: claimPayload(5n), epoch: 0n, roster: null }), { applicationCode: 0x000e });
    const fundBytes = encodeRequestEnvelope({ ...base, selector: FUND, sequence: 5n, payload: fund });
    const delegated = new Uint8Array([...fundBytes.slice(0, -1), 1, ...new Uint8Array(96).fill(7)]);
    await refuses("Application", () => decodeRequestEnvelope(delegated), { applicationCode: 0x0006 });
    const badVersion = claim.slice(); badVersion[7] = 2;
    await refuses("Application", () => decodeRequestEnvelope(badVersion), { applicationCode: 0x0001 });
    const unknown = claim.slice(); unknown[8] = 0x0f;
    await refuses("Application", () => decodeRequestEnvelope(unknown), { applicationCode: 0x001f });
    await refuses("Application", () => decodeRequestEnvelope(claim.slice(0, -2)), { applicationCode: 0x0002 });
  });

  await check("A07 prepare refuses stale, unfinalized, read-only and F06-invalid operations", async () => {
    const page = await entitlements();
    await refuses("StaleAuthority", () => prepareOperation(finalized, 509n, claimRequest({ entitlements: page }), terms()), { lag: 9n });
    await refuses("FinalityUnavailable", () => prepareOperation(finalized, 500n, claimRequest({ entitlements: page, requiredRank: 5 }), terms()));
    await refuses("Application", () => prepareOperation(finalized, 500n, claimRequest({ entitlements: page, requiredRank: 3 }), terms()), { applicationCode: 0x0002 });
    await refuses("NotMutation", () => prepareOperation(finalized, 500n, claimRequest({ selector: 0x0a01, payload: new Uint8Array() }), terms()));
    await refuses("IntegrityFailure", () => prepareOperation({ ...finalized, snapshotId: id(0x77) }, 500n, claimRequest({ entitlements: page }), terms()));
    const fund = (policyVersion: bigint, consent: boolean, amount: bigint): OperationRequest => ({ selector: FUND, actor: id(0xc0),
      payload: encodeFundRequest({ amount, refundRecipient: id(0xb1), policyVersion, consent }), roster: "absent", sequence: 5n,
      expiry: 900n, request: id(0xd1), requiredRank: 4 });
    await refuses("Application", () => prepareOperation(finalized, 500n, fund(2n, true, 10n), terms()), { applicationCode: 0x0601 });
    await refuses("Application", () => prepareOperation(finalized, 500n, fund(1n, false, 10n), terms()), { applicationCode: 0x0610 });
    await refuses("Application", () => prepareOperation(finalized, 500n, fund(1n, true, 0n), terms()), { applicationCode: 0x0604 });
    await refuses("Application", () => prepareOperation(finalized, 500n, claimRequest({ entitlements: page, payload: claimPayload(5n, id(0x12)) }), terms()), { applicationCode: 0x060b });
    await refuses("Application", () => prepareOperation(finalized, 500n, claimRequest({ payload: claimPayload(5n) }), terms()), { applicationCode: 0x060b });
    await refuses("Application", () => prepareOperation(finalized, 500n, claimRequest({ entitlements: page, payload: claimPayload(0n) }), terms()), { applicationCode: 0x060e });
    await refuses("Application", () => prepareOperation(finalized, 500n, { selector: REFUND_FREE, actor: id(0xc0), roster: "absent", sequence: 0n,
      payload: encodeRefundRequest({ expectedRefunded: 0n, amount: 0n, recipient: id(0xb1) }), expiry: 900n, request: id(0xd2), requiredRank: 4 }, terms()), { applicationCode: 0x0604 });
    route = () => ok(pageJson(binding({ revision: 8n }), [workerRow(0x11)], null));
    const otherSnapshot = await views.participants({ market });
    await refuses("SnapshotConflict", () => prepareOperation(finalized, 500n, claimRequest({ entitlements: otherSnapshot }), terms()));
  });

  await check("A07 a claim is prepared, reviewed from its disclosure, approved and signed", async () => {
    const page = await entitlements();
    const prepared = prepareOperation(finalized, 500n, claimRequest({ entitlements: page }), terms());
    const review = prepared.review(disclosure(prepared.canonicalBytes()));
    const t = review.terms;
    assert(t.action === CLAIM && t.chain === id(0x01) && t.program === id(0x02) && t.market === market && t.actor === id(0xc0)
      && t.epoch === 3n && t.config === 2n && t.roster === id(0x08) && t.policy === id(0x07) && t.snapshot === snapshotId(binding()), "binding terms wrong");
    assert(t.effect.kind === "claim" && t.effect.amount === U128_MAX && t.effect.asset === id(0xa1) && t.effect.recipient === id(0xb0)
      && t.effect.worker === id(0x11), "claim effect not exact");
    assert(t.feeLimit === 1_000n && t.notBefore === 1n && t.notAfter === 100n && t.expiry === 900n && t.idempotencyKey === id(0x44)
      && t.authority === hex(ownerKey) && t.commitment === prepared.preparationId, "native terms wrong");
    const record = await review.approve({ ...t }, ownerKey).sign(owner, 505n);
    assert(record.state === "signed" && record.attempt === 0 && record.intent === prepared.intent && record.networkId === 7, "signed record wrong");
    const signed = decodeProgramCallActivity(record.signedBytes);
    assert(signed.signature !== null && ed25519.verify(signed.signature, signingPreimage(prepared.canonicalBytes()), ownerKey), "signature not over the approved bytes");
    assert(record.activityId === activityIdOf(record.signedBytes), "activity id not derived from signed bytes");
  });

  await check("A07 any changed approval term or key is refused", async () => {
    const page = await entitlements();
    const prepared = prepareOperation(finalized, 500n, claimRequest({ entitlements: page }), terms());
    const review = prepared.review(disclosure(prepared.canonicalBytes()));
    const t = review.terms;
    const effect = t.effect.kind === "claim" ? t.effect : null;
    assert(effect !== null, "claim effect missing");
    const changes: readonly [string, ApprovalTerms][] = [
      ["action", { ...t, action: FUND }], ["chain", { ...t, chain: id(0x21) }], ["market", { ...t, market: id(0x23) }],
      ["epoch", { ...t, epoch: 4n }], ["roster", { ...t, roster: null }], ["snapshot", { ...t, snapshot: id(0x24) }],
      ["amount", { ...t, effect: { ...effect, amount: U128_MAX - 1n } }], ["asset", { ...t, effect: { ...effect, asset: id(0xa2) } }],
      ["payee", { ...t, effect: { ...effect, recipient: id(0xb2) } }], ["worker", { ...t, effect: { ...effect, worker: id(0x12) } }],
      ["resources", { ...t, resources: [2n, 16_777_216n, 1_048_576n, 1_048_576n, 64n, 1_048_576n, 4_096n] }],
      ["fee_limit", { ...t, feeLimit: 1_001n }], ["validity", { ...t, notAfter: 101n }], ["expiry", { ...t, expiry: 901n }],
      ["idempotency_key", { ...t, idempotencyKey: id(0x45) }], ["authority", { ...t, authority: hex(workerKey) }],
      ["commitment", { ...t, commitment: id(0x46) }],
    ];
    for (const [field, approved] of changes) {
      const failure = await refuses("ReviewMismatch", () => review.approve(approved, ownerKey));
      assert(failure.detail.field === field, `expected ${field}, got ${String(failure.detail.field)}`);
    }
    await refuses("UnauthorizedKey", () => review.approve(t, workerKey));
    await refuses("UnauthorizedKey", () => review.approve(t, ownerKey).sign({ publicKey: workerKey, sign: (p) => ed25519.sign(p, workerSecret) }, 500n));
    await refuses("Signature", () => review.approve(t, ownerKey).sign({ publicKey: ownerKey, sign: (p) => ed25519.sign(p, workerSecret) }, 500n));
    await refuses("StaleAuthority", () => review.approve(t, ownerKey).sign(owner, 509n));
    const altered = prepared.canonicalBytes(); altered[altered.length - 1] = (altered.at(-1) ?? 0) ^ 1;
    await refuses("IntegrityFailure", () => prepared.review(disclosure(altered)));
    await refuses("NotNativeProgramCall", () => prepared.review({ ...disclosure(prepared.canonicalBytes()), activity: { version: "1", module: "9", ordinal: "4" } }));
  });

  // ----- AI.F10-A08: journaled recovery ------------------------------------------------------

  const recordActivity = encodeProgramCallActivity({ networkId: 7, actorDid: new TextEncoder().encode("did:lxp:ai-market-owner"),
    authority: ownerKey, accountSequence: 4n, notBefore: 1n, notAfter: 100n, idempotencyKey: raw(id(0x44)), feeLimit: BigInt(fixture.fee_limit),
    payload: raw(fixture.payload_hex), signature: null });
  const recordSigned = encodeProgramCallActivity({ ...decodeProgramCallActivity(recordActivity), signature: ed25519.sign(signingPreimage(recordActivity), ownerSecret) });

  await check("A08 records round-trip and refuse corruption", async () => {
    const record = OperationRecord.signed(recordSigned, ownerKey, id(0x55));
    const bytes = record.encode();
    assert(Buffer.from(bytes.slice(0, 8)).toString("ascii") === "PAXAIOP1", "record magic missing");
    const decoded = OperationRecord.decode(bytes);
    assert(decoded.state === "signed" && decoded.activityId === activityIdOf(recordSigned) && decoded.idempotencyKey === id(0x44)
      && decoded.notAfter === 100n && decoded.intent === id(0x55) && hex(decoded.signedBytes) === hex(recordSigned), "record fields lost");
    const flipped = bytes.slice(); flipped[flipped.length - 1] = (flipped.at(-1) ?? 0) ^ 1;
    await refuses("CorruptRecord", () => OperationRecord.decode(flipped));
    const prepared = bytes.slice(); prepared[8] = 1;
    await refuses("CorruptRecord", () => OperationRecord.decode(prepared));
    await refuses("CorruptRecord", () => OperationRecord.decode(new Uint8Array([...bytes, 0])));
    const pending = bytes.slice(); pending[8] = 5;
    await refuses("CorruptRecord", () => OperationRecord.decode(pending));
    await refuses("CorruptRecord", () => OperationRecord.signed(recordSigned, workerKey, id(0x55)));
  });

  const lost: { route: string; body: Buffer }[] = [];
  const agent = http.createServer((request, response) => {
    const chunks: Buffer[] = [];
    request.on("data", (chunk: Buffer) => chunks.push(chunk));
    request.on("end", () => { lost.push({ route: `${request.method} ${request.url}`, body: Buffer.concat(chunks) }); response.socket?.destroy(); });
  });
  agent.listen(0, "127.0.0.1");
  await once(agent, "listening");
  const agentAddress = agent.address();
  assert(agentAddress !== null && typeof agentAddress === "object", "agent listener missing");
  const operations = new ProgramOperations(new ProductionClient(new AgentHttpTransport({ endpoint: `http://127.0.0.1:${agentAddress.port}` })),
    new ProgramTrustContext(Uint8Array.from({ length: 32 }, () => 0x44), () => 50n, 5n, 3));

  try {
    await check("A08 a lost acknowledgement leaves the operation unknown and resends the exact bytes", async () => {
      const journal = await OperationJournal.open(await mkdtemp(join(tmpdir(), "paxai-journal-")));
      const record = OperationRecord.signed(recordSigned, ownerKey, id(0x55));
      await journal.recordSigned(record);
      const unknown = await journal.submit(record, operations);
      assert(unknown.state === "unknown" && unknown.attempt === 1, `lost acknowledgement became ${unknown.state}`);
      assert((await journal.load(record.activityId)).state === "unknown", "unknown state not durable");
      const resent = await journal.resendExact(unknown, operations);
      assert(resent.state === "unknown" && resent.attempt === 2, "resend did not stay unknown");
      const calls = lost.filter((sent) => sent.route === "POST /v1/programs/call");
      assert(calls.length === 2 && calls.every((sent) => hex(sent.body) === hex(recordSigned)), "resend did not carry the exact signed bytes");
      assert((await journal.resolveThrough(resent, operations)).state === "unknown", "a lost lookup changed the state");
      assert((await journal.resolve(resent, null)).state === "unknown", "an absent receipt changed the state");
      assert((await journal.resolve(resent, { state: "unknown", activity_id: resent.activityId, idempotency_key: resent.idempotencyKey })).state === "unknown", "an unknown lookup changed the state");
      await refuses("InvalidTransition", () => journal.expire(resent, 1_000n), { from: "unknown" });
      await refuses("InvalidTransition", () => journal.submit(resent, operations), { from: "unknown" });
      await refuses("InvalidTransition", () => journal.recordSigned(record), { from: "unknown" });
    });

    await check("A08 an interrupted submission recovers as unknown and an unsent signature expires", async () => {
      const journal = await OperationJournal.open(await mkdtemp(join(tmpdir(), "paxai-journal-")));
      const record = OperationRecord.signed(recordSigned, ownerKey, id(0x55));
      await journal.recordSigned(record);
      assert((await journal.expire(record, 100n)).state === "signed", "expired inside the validity window");
      const expired = await journal.expire(record, 101n);
      assert(expired.state === "failed" && (await journal.load(record.activityId)).state === "failed", "unsent signature did not expire durably");
      const second = await OperationJournal.open(await mkdtemp(join(tmpdir(), "paxai-journal-")));
      const unknown = await second.submit(OperationRecord.signed(recordSigned, ownerKey, id(0x55)), operations);
      const interrupted = unknown.encode(); interrupted[8] = 4;
      await writeFile(second.recordPath(unknown.activityId), interrupted);
      const recovered = await second.load(unknown.activityId);
      assert(recovered.state === "unknown" && recovered.attempt === 1, "interrupted submission not recovered as unknown");
      assert((await readFile(second.recordPath(unknown.activityId)))[8] === 9, "recovery not persisted");
      await writeFile(second.recordPath(unknown.activityId), interrupted.slice(0, -1));
      await refuses("CorruptRecord", () => second.load(unknown.activityId));
    });
  } finally {
    agent.close();
    await once(agent, "close");
  }
} finally {
  gateway.close();
  await once(gateway, "close");
}

console.log(`${passed} passed, ${failed} failed`);
if (failed !== 0) process.exitCode = 1;
