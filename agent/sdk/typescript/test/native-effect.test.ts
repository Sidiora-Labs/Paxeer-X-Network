import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { AgentEnvelopeTransport, AgentSessionCredential, encodeAgentEnvelope } from "../src/agent-http.js";
import { encodeNativePrepareRequest } from "../src/generated/client.js";
import { encodeNativeEffectPrepareRequest, nativeEffectPrepareRequestDigest, type NativeEffectPrepareRequestV1 } from "../src/native-effect.js";
import { idempotencyKey, PlatformSdkError, SecretBytes } from "../src/production.js";

const payload = readFileSync(new URL("../../../../../agent/crates/layerx-crypto/tests/fixtures/payments/native-1-5.hex", import.meta.url), "utf8").trim();
assert.match(payload, /^(?:[0-9a-f]{2})+$/u);
assert.equal(payload.slice(0, 4), "5301");
const request: NativeEffectPrepareRequestV1 = {
  variant: "native_effect_v1", activity: { version: "1", module: "1", ordinal: "5" }, actor: "did:layerx:agent", authority: "owner",
  account_sequence: "7", not_before: "100", not_after: "200", idempotency_key: "11".repeat(32), fee_limit: "9", payload,
  payload_hash: createHash("sha256").update("LXP/v1/payload-hash\0").update(Buffer.from(payload, "hex")).digest("hex"), capability_id: "22".repeat(32),
  purpose: { purpose: { version: "1", tenant: "tenant-a", agent_did: "did:layerx:agent", session_id: "33".repeat(32), generation: "1",
    expires_at_ms: "200", capability_id: "22".repeat(32), preparation_id: "44".repeat(32), canonical_digest: "44".repeat(32), commitment: "55".repeat(32) },
    owner_public_key: "66".repeat(32), signature: "77".repeat(64) },
};
const canonical = encodeNativeEffectPrepareRequest(request);
assert.equal(canonical.payload, payload);
assert.deepEqual(canonical.activity, { version: "1", module: "1", ordinal: "5" });
assert.equal(Number(canonical.activity.module) * 65536 + Number(canonical.activity.ordinal), 0x10005);
assert.equal(canonical.local_grant, null);
assert.deepEqual(encodeNativeEffectPrepareRequest(canonical), canonical);
assert.equal(nativeEffectPrepareRequestDigest(request), nativeEffectPrepareRequestDigest(canonical));
const reordered = Object.fromEntries(Object.entries(request).reverse()) as unknown as NativeEffectPrepareRequestV1;
assert.equal(nativeEffectPrepareRequestDigest(reordered), nativeEffectPrepareRequestDigest(request));
assert.notEqual(nativeEffectPrepareRequestDigest({ ...request, account_sequence: "8" }), nativeEffectPrepareRequestDigest(request));
assert.notEqual(nativeEffectPrepareRequestDigest({ ...request, idempotency_key: "aa".repeat(32) }), nativeEffectPrepareRequestDigest(request));

function rejected(value: unknown): void {
  assert.throws(() => encodeNativeEffectPrepareRequest(value as NativeEffectPrepareRequestV1), TypeError);
}
for (const field of Object.keys(request)) {
  const missing = { ...request } as Record<string, unknown>; delete missing[field]; rejected(missing);
}
rejected(null); rejected([]); rejected({ ...request, extra: true }); rejected({ ...request, activity_type: "5" });
for (const variant of [undefined, "native_v1", "native_effect_v2", 1]) rejected({ ...request, variant });
for (const module of ["0", "9", "12", "65535", "01", 1, "-1"]) rejected({ ...request, activity: { ...request.activity, module } });
for (const ordinal of ["0", "65536", "05", 5, "-1"]) rejected({ ...request, activity: { ...request.activity, ordinal } });
rejected({ ...request, activity: { ...request.activity, version: "2" } });
rejected({ ...request, activity: { ...request.activity, extra: true } });
for (const module of ["1", "2", "3", "4", "5", "6", "7", "8", "10", "11"]) {
  const value = encodeNativeEffectPrepareRequest({ ...request, activity: { ...request.activity, module, ordinal: "65535" } });
  assert.equal(value.activity.ordinal, "65535"); assert.equal(value.activity.module, module);
}
for (const field of ["account_sequence", "not_before", "not_after"] as const) {
  for (const value of [1, "01", "-1", "18446744073709551616", "", "1.0"]) rejected({ ...request, [field]: value });
}
rejected({ ...request, not_before: "201" });
rejected({ ...request, fee_limit: "340282366920938463463374607431768211456" });
assert.equal(encodeNativeEffectPrepareRequest({ ...request, fee_limit: "340282366920938463463374607431768211455" }).fee_limit, "340282366920938463463374607431768211455");
for (const field of ["idempotency_key", "payload_hash", "capability_id"] as const) {
  for (const value of ["", "ab".repeat(31), "ab".repeat(33), "AB".repeat(32), "0x" + "ab".repeat(32)]) rejected({ ...request, [field]: value });
}
for (const value of ["", "a", "AB", "aa".repeat(524289)]) rejected({ ...request, payload: value });
assert.equal(encodeNativeEffectPrepareRequest({ ...request, payload: "aa".repeat(524288) }).payload.length, 1048576);
rejected({ ...request, actor: "different-agent" }); rejected({ ...request, actor: "é".repeat(128) });
rejected({ ...request, authority: "" }); rejected({ ...request, authority: "a".repeat(524289) });
rejected({ ...request, authority: "\ud800" }); rejected({ ...request, purpose: { ...request.purpose, extra: true } });
const purpose = request.purpose.purpose;
for (const field of Object.keys(purpose)) {
  const missing = { ...purpose } as Record<string, unknown>; delete missing[field];
  rejected({ ...request, purpose: { ...request.purpose, purpose: missing } });
}
for (const mutation of [{ generation: "0" }, { expires_at_ms: "0" }, { capability_id: "88".repeat(32) }, { agent_did: "different-agent" },
  { tenant: "\0" }, { tenant: "é".repeat(128) }, { version: "2" }, { session_id: "AB".repeat(32) }, { extra: true }]) {
  rejected({ ...request, purpose: { ...request.purpose, purpose: { ...purpose, ...mutation } } });
}
rejected({ ...request, purpose: { ...request.purpose, owner_public_key: "aa" } });
rejected({ ...request, purpose: { ...request.purpose, signature: "aa".repeat(63) } });
const grant = { version: "1" as const, capability: "010203", session_scope: "040506", expires_at_ms: "200", owner_public_key: "66".repeat(32), signature: "77".repeat(64) };
const withGrant = encodeNativeEffectPrepareRequest({ ...request, local_grant: grant });
assert.deepEqual(withGrant.local_grant, grant);
assert.notEqual(nativeEffectPrepareRequestDigest(withGrant), nativeEffectPrepareRequestDigest(request));
for (const mutation of [{ expires_at_ms: "0" }, { capability: "" }, { session_scope: "A0" }, { version: "2" },
  { capability: "aa".repeat(1048577) }, { session_scope: "aa".repeat(1048577) }, { signature: "aa" }, { extra: true }]) rejected({ ...request, local_grant: { ...grant, ...mutation } });

assert.throws(() => encodeNativePrepareRequest(request as unknown as Parameters<typeof encodeNativePrepareRequest>[0]), TypeError);
assert.throws(() => encodeNativePrepareRequest({ ...request, variant: "native_v1" }), TypeError);
const programs = encodeNativePrepareRequest({ ...request, variant: "native_v1", activity: { version: "1", module: "9", ordinal: "3" } });
assert.equal(programs.activity.module, "9");
rejected({ ...programs, variant: "native_effect_v1" });

const session = new AgentSessionCredential(purpose.tenant, purpose.session_id, new SecretBytes(Buffer.alloc(32, 1)), purpose.generation);
const key = idempotencyKey(request.idempotency_key);
const envelope = JSON.parse(encodeAgentEnvelope("prepare", "1", canonical, session, key).toString("utf8")) as Record<string, unknown>;
assert.equal(envelope.operation, "prepare"); assert.equal(envelope.idempotency_key, key);
assert.deepEqual(envelope.request, canonical);
assert.deepEqual(envelope.credential, session.encode());
const transport = new AgentEnvelopeTransport({ endpoint: "http://127.0.0.1:1", session });
const invalid = (error: unknown): boolean => error instanceof PlatformSdkError && error.code === "invalid-argument";
await assert.rejects(transport.prepareNativeEffect({ ...request, purpose: { ...request.purpose, purpose: { ...purpose, tenant: "other-tenant" } } }, key), invalid);
await assert.rejects(transport.prepareNativeEffect({ ...request, purpose: { ...request.purpose, purpose: { ...purpose, session_id: "88".repeat(32) } } }, key), invalid);
await assert.rejects(transport.prepareNativeEffect({ ...request, purpose: { ...request.purpose, purpose: { ...purpose, generation: "2" } } }, key), invalid);
await assert.rejects(transport.prepareNativeEffect(request, idempotencyKey("88".repeat(32))), invalid);
await assert.rejects(new AgentEnvelopeTransport({ endpoint: "http://127.0.0.1:1" }).prepareNativeEffect(request, key), invalid);

const crossPath = process.env.NATIVE_EFFECT_CROSS_LANGUAGE_REQUEST;
if (crossPath !== undefined) {
  const actual = JSON.parse(readFileSync(crossPath, "utf8")) as NativeEffectPrepareRequestV1;
  const encoded = encodeNativeEffectPrepareRequest(actual);
  assert.deepEqual(encoded, actual);
  assert.equal(encoded.payload, payload);
  const sorted = (value: unknown): unknown => value !== null && typeof value === "object" && !Array.isArray(value)
    ? Object.fromEntries(Object.entries(value as Record<string, unknown>).sort(([left], [right]) => left < right ? -1 : left > right ? 1 : 0).map(([key, item]) => [key, sorted(item)])) : value;
  const canonicalRequest = JSON.stringify(sorted(encoded));
  const expected = createHash("sha256").update("LXP/agent/native-effect-prepare/v1\0").update(canonicalRequest).digest("hex");
  assert.equal(nativeEffectPrepareRequestDigest(encoded), expected);
  const output = process.env.NATIVE_EFFECT_TYPESCRIPT_CANONICAL_OUTPUT;
  if (output !== undefined) writeFileSync(output, canonicalRequest, "utf8");
}
console.log("native effect strict codec, genuine Asset1/5 bytes, authenticated envelope and pre-dispatch refusals passed");
