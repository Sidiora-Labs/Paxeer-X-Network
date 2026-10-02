import { createHash, createPublicKey, verify } from "node:crypto";
import {
  encodeNativePrepareRequest, decodeNativePrepareResult, decodeNativeApprovalListResult,
  PlatformSdkError, type NativePrepareRequestV1, type ProductionClient,
} from "@sidiora/layerx-sdk";
import type { AgentSpendRequest, PreparedActivity, Submission, ApprovalHold, AgentReceiptEvidence } from "./index.js";

export interface DaemonPrepareRequest {
  readonly activity_type: string;
  readonly actor: string;
  readonly authority: string;
  readonly account_sequence: string;
  readonly not_before: string;
  readonly not_after: string;
  readonly idempotency_key: string;
  readonly fee_limit: string;
  readonly payload: string;
  readonly payload_hash: string;
  readonly capability_id?: string;
}
export type DaemonPreparation = DaemonPrepareRequest | NativePrepareRequestV1;
const U64 = 18446744073709551615n;
const U128 = 340282366920938463463374607431768211455n;
export function native(request: DaemonPreparation): request is NativePrepareRequestV1 {
  return "variant" in request;
}
function fail(): never { throw new PlatformSdkError({ code: "decode-failure", retry: "unknown-outcome" }); }
function object(value: unknown): Readonly<Record<string, unknown>> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) fail();
  return value as Readonly<Record<string, unknown>>;
}
function exact(value: Readonly<Record<string, unknown>>, fields: readonly string[], optional: readonly string[] = []): void {
  if (fields.some((key) => !(key in value)) || Object.keys(value).some((key) => !fields.includes(key) && !optional.includes(key))) fail();
}
function text(value: unknown, max: number): string {
  if (typeof value !== "string" || value.length === 0 || Buffer.byteLength(value) > max || value.includes("\0") || Buffer.from(value).toString("utf8") !== value) fail();
  return value;
}
export function hex(value: unknown, max: number, size?: number): string {
  if (typeof value !== "string" || !/^(?:[0-9a-f]{2})+$/u.test(value) || value.length > max * 2 || size !== undefined && value.length !== size * 2) fail();
  return value;
}
function decimal(value: unknown, max = U64): string {
  if (typeof value !== "string" || !/^(0|[1-9][0-9]*)$/u.test(value) || value.length > 39 || BigInt(value) > max) fail();
  return value;
}
export function digest(bytes: Uint8Array, domain = ""): string {
  return createHash("sha256").update(domain).update(bytes).digest("hex");
}
export function validateDaemonSpend(request: AgentSpendRequest): void {
  const p = request.preparation;
  if (native(p)) {
    encodeNativePrepareRequest(p);
    if (p.purpose.purpose.tenant !== request.tenant) fail();
  } else {
    exact(object(p), ["activity_type", "actor", "authority", "account_sequence", "not_before", "not_after", "idempotency_key", "fee_limit", "payload", "payload_hash"], ["capability_id"]);
    const activity = BigInt(decimal(p.activity_type, 4294967295n));
    if (activity === 0n || (activity & 65535n) === 0n) fail();
    if (p.capability_id !== undefined) hex(p.capability_id, 32, 32);
  }
  text(p.actor, 255); text(p.authority, 524288);
  decimal(p.account_sequence); decimal(p.not_before); decimal(p.not_after); decimal(p.fee_limit, U128);
  if (BigInt(p.not_before) > BigInt(p.not_after)) fail();
  hex(p.idempotency_key, 32, 32); hex(p.payload, 524288); hex(p.payload_hash, 32, 32);
  hex(request.submitIdempotencyKey, 32, 32); hex(request.signerPublicKey, 32, 32);
  if (request.submitIdempotencyKey === p.idempotency_key) fail();
  hex(request.authorityHex, 524288); decimal(request.networkId, 4294967295n);
  decimal(request.approvalCurrentSequence);
  if (request.approvalReleaseRef !== undefined) hex(request.approvalReleaseRef, 32, 32);
  if (digest(Buffer.from(p.payload, "hex"), "LXP/v1/payload-hash\0") !== p.payload_hash) fail();
}

class Reader {
  offset = 0;
  constructor(readonly bytes: Buffer) {}
  take(n: number): Buffer { if (n < 0 || this.offset + n > this.bytes.length) fail(); const b = this.bytes.subarray(this.offset, this.offset + n); this.offset += n; return b; }
  integer(n: number): bigint { const value = this.take(n); let out = 0n; for (const b of value) out = out * 256n + BigInt(b); return out; }
  field(n: number): void { if (this.integer(1) !== BigInt(n)) fail(); }
  sized(max: number): Buffer { const size = Number(this.integer(4)); if (size > max) fail(); return this.take(size); }
}
function bindCanonical(encoded: string, request: AgentSpendRequest, protocolVersion: number): void {
  const p = request.preparation, r = new Reader(Buffer.from(hex(encoded, 1048576), "hex"));
  if (r.integer(2) !== BigInt(protocolVersion) || r.integer(2) !== 0x1001n || r.integer(1) !== 11n) fail();
  r.field(1); if (r.integer(2) !== BigInt(protocolVersion)) fail();
  r.field(2); if (r.integer(4) !== BigInt(request.networkId)) fail();
  const activity = native(p) ? BigInt(p.activity.module) * 65536n + BigInt(p.activity.ordinal) : BigInt(p.activity_type);
  r.field(3); if (r.integer(4) !== activity) fail();
  r.field(4); if (!r.sized(255).equals(Buffer.from(p.actor))) fail();
  r.field(5); if (r.sized(524288).toString("hex") !== request.authorityHex) fail();
  r.field(6); if (r.integer(8) !== BigInt(p.account_sequence)) fail();
  r.field(7); if (r.integer(8) !== BigInt(p.not_before) || r.integer(8) !== BigInt(p.not_after)) fail();
  r.field(8); if (r.sized(32).toString("hex") !== p.idempotency_key) fail();
  r.field(9); if (r.integer(16) !== BigInt(p.fee_limit)) fail();
  r.field(10); if (r.sized(32).toString("hex") !== p.payload_hash) fail();
  r.field(11); if (r.sized(524288).toString("hex") !== p.payload || r.offset !== r.bytes.length) fail();
}
export function decodeDaemonPrepared(value: unknown, request: AgentSpendRequest, protocolVersion: number): PreparedActivity {
  const p = request.preparation;
  let reference: string, canonical: string, preimage: string, approval: ApprovalHold | undefined;
  if (native(p)) {
    const result = decodeNativePrepareResult(value);
    reference = result.preparation_id; canonical = result.canonical_bytes; preimage = result.signing_preimage;
    if (result.activity.module !== p.activity.module || result.activity.ordinal !== p.activity.ordinal
      || reference !== p.purpose.purpose.preparation_id || reference !== p.purpose.purpose.canonical_digest
      || result.approval_id !== null && result.approval_id !== reference) fail();
    if (result.approval_required) approval = { approvalId: reference, state: "Held", canonicalBytesDigest: reference, enforcement: "daemon_enforced" };
  } else {
    const result = object(value);
    exact(result, ["preparation_ref", "unsigned_canonical_bytes", "signing_preimage", "activity_type", "actor", "authority", "account_sequence", "not_before", "not_after", "fee_limit", "payload", "payload_hash", "idempotency_key"]);
    for (const field of ["activity_type", "actor", "authority", "account_sequence", "not_before", "not_after", "fee_limit", "payload", "payload_hash", "idempotency_key"] as const) if (result[field] !== p[field]) fail();
    reference = hex(result.preparation_ref, 32, 32); canonical = hex(result.unsigned_canonical_bytes, 1048576); preimage = hex(result.signing_preimage, 32, 32);
  }
  bindCanonical(canonical, request, protocolVersion);
  if (digest(Buffer.from(canonical, "hex")) !== reference || digest(Buffer.from(canonical, "hex"), "LXP/v1/signature-preimage\0") !== preimage) fail();
  return Object.freeze({ preparation_ref: reference, unsigned_canonical_bytes: canonical, signing_preimage: preimage,
    disclosure: Object.freeze({ ...p, canonical_digest: reference, network_id: request.networkId, authority_bytes: request.authorityHex }),
    expiry: p.not_after, ...(approval === undefined ? {} : { approval: Object.freeze(approval) }) });
}
export function signedActivityId(prepared: PreparedActivity, signature: string, publicKey: string): string {
  hex(signature, 64, 64); hex(publicKey, 32, 32);
  const key = createPublicKey({ key: Buffer.concat([Buffer.from("302a300506032b6570032100", "hex"), Buffer.from(publicKey, "hex")]), format: "der", type: "spki" });
  if (!verify(null, Buffer.from(prepared.signing_preimage, "hex"), key, Buffer.from(signature, "hex"))) throw new PlatformSdkError({ code: "verification-failure", retry: "never" });
  const canonical = Buffer.from(prepared.unsigned_canonical_bytes, "hex");
  if (canonical[4] !== 11) fail(); canonical[4] = 12;
  return digest(Buffer.concat([canonical, Buffer.from([12, 0, 0, 0, 64]), Buffer.from(signature, "hex")]), "LXP/v1/activity-id\0");
}
export function decodeDaemonSubmission(value: unknown, expectedActivityId?: string, expectedReference?: string): Submission {
  const observation = object(value); exact(observation, ["activity_id", "submission", "receipt"]);
  const activityId = hex(observation.activity_id, 32, 32);
  if (activityId === "00".repeat(32) || expectedActivityId !== undefined && activityId !== expectedActivityId) fail();
  const s = object(observation.submission), state = text(s.state, 32);
  const terminal = state === "Executed" ? ["receipt_ref"] : state === "Failed" ? ["result_code"] : [];
  exact(s, ["submission_ref", "state", "verification_level", "evidence", "transitions", ...terminal]);
  if (!["Prepared", "Signed", "Queued", "Submitted", "Acknowledged", "Unknown", "Executed", "Failed", "Expired"].includes(state)) fail();
  const reference = text(s.submission_ref, 255);
  if (expectedReference !== undefined && reference !== expectedReference) fail();
  if (!Array.isArray(s.evidence) || s.evidence.length > 64 || !Array.isArray(s.transitions) || s.transitions.length > 64) fail();
  const level = text(s.verification_level, 64);
  const levels = ["Unverified", "SequencerSigned", "BatchIncluded", "StateProven", "CheckpointFinalised", "SettlementAnchored"];
  if (!levels.includes(level)) fail();
  for (const value of s.evidence) { const item = object(value); exact(item, ["kind", "digest"]); text(item.kind, 255); hex(item.digest, 32, 32); }
  for (const value of s.transitions) {
    const item = object(value); exact(item, ["from", "to", "cause", "at"]);
    const states = ["Prepared", "Signed", "Queued", "Submitted", "Acknowledged", "Unknown", "Expired"];
    if (!states.includes(text(item.from, 32)) || !states.includes(text(item.to, 32))) fail();
    text(item.cause, 255); decimal(item.at);
  }
  let receiptEvidence: AgentReceiptEvidence | undefined;
  if (state === "Executed") {
    text(s.receipt_ref, 255);
    const receipt = object(observation.receipt); exact(receipt, ["canonical_bytes", "authorised_batch", "verification_level"]);
    if (!levels.slice(1).includes(text(receipt.verification_level, 64))) fail();
    const batch = object(receipt.authorised_batch);
    exact(batch, ["batch_id", "asset", "previous_state_root", "resulting_state_root", "sequencer_public_key"]);
    const field = (name: string): Uint8Array => Buffer.from(hex(batch[name], 32, 32), "hex");
    receiptEvidence = { canonicalReceipt: Buffer.from(hex(receipt.canonical_bytes, 1048576), "hex"), authorizedBatch: {
      batchId: field("batch_id"), asset: field("asset"), previousStateRoot: field("previous_state_root"), resultingStateRoot: field("resulting_state_root"), sequencerPublicKey: field("sequencer_public_key"),
    } };
  } else if (observation.receipt !== null) fail();
  if (state === "Failed" && (typeof s.result_code !== "string" || s.result_code.length > 11 || !/^(0|-?[1-9][0-9]*)$/u.test(s.result_code) || BigInt(s.result_code) < -2147483648n || BigInt(s.result_code) > 2147483647n)) fail();
  return { submission_ref: reference, state, activity_id: activityId, verification_level: level,
    evidence: s.evidence, transitions: s.transitions,
    ...(state === "Executed" ? { receipt_ref: s.receipt_ref as string } : {}),
    ...(receiptEvidence === undefined ? {} : { receiptEvidence }) };
}
export async function daemonApprovalHold(client: ProductionClient, request: AgentSpendRequest, prepared: PreparedActivity): Promise<ApprovalHold | undefined> {
  const digest = prepared.preparation_ref;
  if (native(request.preparation)) {
    const result = decodeNativeApprovalListResult(await client.agent("approval.list", { variant: "native_v1" }));
    if (new Set(result.approvals.map((entry) => entry.approval_id)).size !== result.approvals.length) fail();
    const approval = result.approvals.find((entry) => entry.approval_id === digest);
    if (approval === undefined || approval.state !== "Awaiting") return undefined;
    if (approval.activity.module !== request.preparation.activity.module || approval.activity.ordinal !== request.preparation.activity.ordinal) fail();
    return { approvalId: approval.approval_id, state: "Held", canonicalBytesDigest: digest, enforcement: "daemon_enforced" };
  }
  let cursor: string | null = null;
  const seen = new Set<string>();
  const approvalsSeen = new Set<string>();
  for (let page = 0; page < 100; page += 1) {
    const result = object(await client.agent("approval.list", { current_sequence: request.approvalCurrentSequence, cursor, limit: "100" }));
    exact(result, ["approvals", "next_cursor"]);
    if (!Array.isArray(result.approvals) || result.approvals.length > 100) fail();
    for (const value of result.approvals) {
      const a = object(value);
      exact(a, ["approval_id", "canonical_digest", "activity_type", "actor", "authority", "counterparties", "amounts", "asset", "fee_limit", "expiry", "idempotency_key", "canonical_bytes_digest", "hold_reason_code", "hold_reason", "created_at_sequence", "expires_at_sequence", "state"], ["submission_ref"]);
      const approvalId = hex(a.approval_id, 32, 32);
      if (approvalsSeen.has(approvalId)) fail(); approvalsSeen.add(approvalId);
      hex(a.canonical_digest, 32, 32); hex(a.canonical_bytes_digest, 32, 32); decimal(a.activity_type, 65535n);
      text(a.actor, 255); text(a.authority, 255); text(a.asset, 255); text(a.idempotency_key, 255);
      decimal(a.fee_limit, U128); decimal(a.expiry); decimal(a.created_at_sequence); decimal(a.expires_at_sequence);
      text(a.hold_reason_code, 255); text(a.hold_reason, 255);
      if (!Array.isArray(a.counterparties) || a.counterparties.length > 64 || !Array.isArray(a.amounts) || a.amounts.length > 64) fail();
      for (const party of a.counterparties) text(party, 255);
      for (const amount of a.amounts) { const row = object(amount); exact(row, ["counterparty", "amount"]); text(row.counterparty, 255); decimal(row.amount, U128); }
      if (!["AwaitingApproval", "Approved", "Rejected", "Expired", "Defective"].includes(text(a.state, 32))) fail();
      if (a.state === "Approved") hex(a.submission_ref, 32, 32); else if (a.submission_ref !== undefined) fail();
      if (a.canonical_bytes_digest === digest && a.state === "AwaitingApproval") {
        if (a.canonical_digest !== digest || a.actor !== request.preparation.actor || a.authority !== request.preparation.authority
          || a.idempotency_key !== request.preparation.idempotency_key) fail();
        return { approvalId: hex(a.approval_id, 32, 32), state: "Held", canonicalBytesDigest: digest, enforcement: "daemon_enforced" };
      }
    }
    if (result.next_cursor === null) return undefined;
    cursor = hex(result.next_cursor, 32, 32); if (seen.has(cursor)) fail(); seen.add(cursor);
  }
  fail();
}
