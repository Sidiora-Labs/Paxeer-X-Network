import { createHash } from "node:crypto";
import type { NativeActivityV1, NativePrepareRequestV1, NativePurposeV1, NativeLocalGrantV1 } from "./generated/client.js";

export interface NativeEffectPrepareRequestV1 extends Omit<NativePrepareRequestV1, "variant"> {
  variant: "native_effect_v1";
}

function fields(value: unknown, required: readonly string[], optional: readonly string[] = []): Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) throw new TypeError("native_effect_v1.object");
  const item = value as Record<string, unknown>;
  if (required.some((name) => !Object.prototype.hasOwnProperty.call(item, name))
    || Object.keys(item).some((name) => !required.includes(name) && !optional.includes(name))) throw new TypeError("native_effect_v1.fields");
  return item;
}

function text(value: unknown, maximum: number, forbidNul = false): string {
  if (typeof value !== "string" || value.length === 0 || !wellFormedUtf16(value)
    || Buffer.byteLength(value, "utf8") > maximum || forbidNul && value.includes("\0")) throw new TypeError("native_effect_v1.text");
  return value;
}

function wellFormedUtf16(value: string): boolean {
  for (let index = 0; index < value.length; index++) {
    const code = value.charCodeAt(index);
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = value.charCodeAt(++index);
      if (!(next >= 0xdc00 && next <= 0xdfff)) return false;
    } else if (code >= 0xdc00 && code <= 0xdfff) return false;
  }
  return true;
}

function decimal(value: unknown, maximum = 18446744073709551615n): string {
  if (typeof value !== "string" || value.length > 39 || !/^(0|[1-9][0-9]*)$/u.test(value)
    || BigInt(value) > maximum) throw new TypeError("native_effect_v1.integer");
  return value;
}

function hex(value: unknown, bytes: number, exact = true): string {
  if (typeof value !== "string" || value.length === 0 || (exact ? value.length !== bytes * 2 : value.length > bytes * 2)
    || !/^(?:[0-9a-f]{2})+$/u.test(value)) throw new TypeError("native_effect_v1.hex");
  return value;
}

function version(value: unknown): "1" {
  if (value !== "1") throw new TypeError("native_effect_v1.version");
  return value;
}

export function encodeNativeEffectPrepareRequest(value: NativeEffectPrepareRequestV1): NativeEffectPrepareRequestV1 {
  const item = fields(value, ["variant", "activity", "actor", "authority", "account_sequence", "not_before", "not_after", "idempotency_key", "fee_limit", "payload", "payload_hash", "capability_id", "purpose"], ["local_grant"]);
  if (item.variant !== "native_effect_v1") throw new TypeError("native_effect_v1.variant");
  const activityInput = fields(item.activity, ["version", "module", "ordinal"]);
  const activity: NativeActivityV1 = { version: version(activityInput.version), module: decimal(activityInput.module, 11n), ordinal: decimal(activityInput.ordinal, 65535n) };
  if (activity.module === "0" || activity.module === "9" || activity.ordinal === "0") throw new TypeError("native_effect_v1.activity");
  const signed = fields(item.purpose, ["purpose", "owner_public_key", "signature"]);
  const input = fields(signed.purpose, ["version", "tenant", "agent_did", "session_id", "generation", "expires_at_ms", "capability_id", "preparation_id", "canonical_digest", "commitment"]);
  const purpose: NativePurposeV1 = { version: version(input.version), tenant: text(input.tenant, 255, true),
    agent_did: text(input.agent_did, 255), session_id: hex(input.session_id, 32), generation: decimal(input.generation),
    expires_at_ms: decimal(input.expires_at_ms), capability_id: hex(input.capability_id, 32), preparation_id: hex(input.preparation_id, 32),
    canonical_digest: hex(input.canonical_digest, 32), commitment: hex(input.commitment, 32) };
  const actor = text(item.actor, 255), capability_id = hex(item.capability_id, 32);
  const not_before = decimal(item.not_before), not_after = decimal(item.not_after);
  if (actor !== purpose.agent_did || capability_id !== purpose.capability_id || purpose.generation === "0"
    || purpose.expires_at_ms === "0" || BigInt(not_before) > BigInt(not_after)) throw new TypeError("native_effect_v1.binding");
  let local_grant: NativeLocalGrantV1 | null = null;
  if (item.local_grant !== undefined && item.local_grant !== null) {
    const grant = fields(item.local_grant, ["version", "capability", "session_scope", "expires_at_ms", "owner_public_key", "signature"]);
    local_grant = { version: version(grant.version), capability: hex(grant.capability, 1048576, false), session_scope: hex(grant.session_scope, 1048576, false),
      expires_at_ms: decimal(grant.expires_at_ms), owner_public_key: hex(grant.owner_public_key, 32), signature: hex(grant.signature, 64) };
    if (local_grant.expires_at_ms === "0") throw new TypeError("native_effect_v1.expiry");
  }
  return { variant: "native_effect_v1", activity, actor, authority: text(item.authority, 524288), account_sequence: decimal(item.account_sequence),
    not_before, not_after, idempotency_key: hex(item.idempotency_key, 32), fee_limit: decimal(item.fee_limit, 340282366920938463463374607431768211455n),
    payload: hex(item.payload, 524288, false), payload_hash: hex(item.payload_hash, 32), capability_id,
    purpose: { purpose, owner_public_key: hex(signed.owner_public_key, 32), signature: hex(signed.signature, 64) }, local_grant };
}

export function nativeEffectPrepareRequestDigest(value: NativeEffectPrepareRequestV1): string {
  const sorted = (value: unknown): unknown => {
    if (value !== null && typeof value === "object" && !Array.isArray(value)) {
      const item = value as Record<string, unknown>;
      return Object.fromEntries(Object.keys(item).sort().map((key) => [key, sorted(item[key])]));
    }
    return value;
  };
  return createHash("sha256").update("LXP/agent/native-effect-prepare/v1\0")
    .update(JSON.stringify(sorted(encodeNativeEffectPrepareRequest(value)))).digest("hex");
}
