// Generated from the LayerX Agent API schema. Do not hand-edit.

const PACKAGE_METADATA = Object.freeze({
  name: "@sidiora/layerx-sdk",
  version: "0.1.0",
  contractMajor: 1,
});

export function layerx_sdk_ts_package(): typeof PACKAGE_METADATA {
  return PACKAGE_METADATA;
}

export type Amount = bigint;
export function parseAmount(value: string): Amount {
  if (!/^(0|[1-9][0-9]*)$/.test(value)) throw new RangeError("invalid Amount");
  const parsed = BigInt(value);
  if (parsed > 340282366920938463463374607431768211455n) throw new RangeError("Amount out of range");
  return parsed;
}
export type BudgetLimit = bigint;
export function parseBudgetLimit(value: string): BudgetLimit {
  if (!/^(0|[1-9][0-9]*)$/.test(value)) throw new RangeError("invalid BudgetLimit");
  const parsed = BigInt(value);
  if (parsed > 340282366920938463463374607431768211455n) throw new RangeError("BudgetLimit out of range");
  return parsed;
}
export type Sequence = bigint;
export function parseSequence(value: string): Sequence {
  if (!/^(0|[1-9][0-9]*)$/.test(value)) throw new RangeError("invalid Sequence");
  const parsed = BigInt(value);
  if (parsed > 18446744073709551615n) throw new RangeError("Sequence out of range");
  return parsed;
}
export type TimestampSeconds = bigint;
export function parseTimestampSeconds(value: string): TimestampSeconds {
  if (!/^(0|[1-9][0-9]*)$/.test(value)) throw new RangeError("invalid TimestampSeconds");
  const parsed = BigInt(value);
  if (parsed > 18446744073709551615n) throw new RangeError("TimestampSeconds out of range");
  return parsed;
}

export enum VerificationLevel {
  Unverified = 0,
  SequencerSigned = 1,
  BatchIncluded = 2,
  StateProven = 3,
  CheckpointFinalised = 4,
  SettlementAnchored = 5,
}

export type SubmissionState =
  | { kind: "Unknown" }
  | { kind: "Executed"; receiptRef: string }
  | { kind: "Failed"; protocolResultCode: number }
  | { kind: "Pending"; stage: string };

export interface VerifiedRead<T> {
  value: T;
  achievedVerificationLevel: VerificationLevel;
  freshness: { chainHead: bigint; latestBatch: string; latestCheckpoint: string; valueSequence: bigint };
}

export function requireVerified<T>(requested: VerificationLevel, read: VerifiedRead<T>): VerifiedRead<T> {
  if (read.achievedVerificationLevel === VerificationLevel.Unverified) {
    throw new Error("unverified_read");
  }
  if (read.achievedVerificationLevel < requested) {
    throw new Error(`verification_below_requested:${requested}:${read.achievedVerificationLevel}`);
  }
  return read;
}

export interface IdempotentMutation<T> {
  requestId: bigint;
  key: Uint8Array;
  bodyDigest: Uint8Array;
  operation: T;
}

export type ErrorClass = "TransportFailure" | "Deadline" | "ProtocolIncompatibility" | "UnavailableCapability" | "CoreRejection" | "VerificationFailure" | "PolicyRefusal" | "CapabilityRefusal" | "BudgetRefusal" | "RateLimit" | "IdempotencyConflict" | "InternalFault";
export interface ApiError {
  errorClass: ErrorClass;
  protocolResultCode: number | null;
  retriable: boolean;
  requestId: bigint;
  reason: string;
}
export type Operation = "agent.register" | "approval.approve" | "approval.get" | "approval.list" | "approval.reject" | "availability.fetch" | "budget.create" | "budget.fund" | "budget.list" | "budget.reconciliation" | "budget.revoke" | "budget.state" | "capability.attenuate" | "capability.create" | "capability.list" | "capability.revoke" | "export.offline" | "faucet.claim" | "policy.dry_run" | "prepare" | "program.activity" | "program.call" | "program.deploy" | "program.discover" | "program.interface" | "program.receipt" | "program.simulate" | "program.upgrade" | "program.wind-down" | "project" | "read.account" | "read.balance" | "read.batch" | "read.checkpoint" | "read.history" | "read.module_state" | "read.proof_bundle" | "session.close" | "session.list" | "session.open" | "session.refresh" | "sign" | "submit" | "subscription.acknowledge" | "subscription.create" | "subscription.delete" | "subscription.health" | "subscription.list" | "subscription.pause" | "subscription.resume" | "tenant.readiness" | "track" | "wait";

export const APPROVAL_CONTRACT_INTRODUCED = "1.1" as const;
export const APPROVAL_ENFORCEMENT_NOTICE = "An approval hold is a daemon-enforced restriction. It confers no protocol authority, and bypassing the daemon bypasses the restriction." as const;
export const APPROVAL_STATES = ["Held", "Granted", "Rejected", "Expired", "Defective"] as const;
export const APPROVAL_DECISION_OUTCOMES = ["Granted", "Rejected", "Expired", "Defective", "AlreadyDecided", "Conflict"] as const;
export const APPROVAL_EVENT_KINDS = ["Created", "Granted", "Rejected", "Expired", "Defective"] as const;

export type ApprovalState = (typeof APPROVAL_STATES)[number];
export type ApprovalDecisionOutcome = (typeof APPROVAL_DECISION_OUTCOMES)[number];
export type ApprovalEventKind = (typeof APPROVAL_EVENT_KINDS)[number];

export interface StructuredActivityDisclosure {
  canonicalDigest: string;
  activityType: string;
  actor: string;
  authority: string;
  counterparties: readonly string[];
  amounts: readonly Amount[];
  asset: string;
  feeLimit: Amount;
  expiry: TimestampSeconds;
  idempotencyKey: string;
}

export interface HoldReason { code: string; message: string }
export interface ApprovalRecord {
  approvalId: string;
  tenant: string;
  heldActivity: StructuredActivityDisclosure;
  canonicalBytesDigest: string;
  holdReason: HoldReason;
  createdAt: TimestampSeconds;
  expiresAt: TimestampSeconds;
  state: ApprovalState;
  enforcement: "daemon_enforced";
  authorityNotice: typeof APPROVAL_ENFORCEMENT_NOTICE;
}
export interface ApprovalPage { approvals: readonly ApprovalRecord[]; nextCursor: string | null }
export interface ApprovalListRequest { tenant: string; cursor: string | null; pageLimit: number }
export interface ApprovalGetRequest { tenant: string; approvalId: string }
export interface ApprovalApproveRequest { tenant: string; approvalId: string; idempotencyKey: string }
export interface ApprovalRejectRequest { tenant: string; approvalId: string; idempotencyKey: string; reason: string }
export interface ApprovalDecision {
  outcome: ApprovalDecisionOutcome;
  submissionRef: string | null;
  winningOutcome: ApprovalDecisionOutcome | null;
  enforcement: "daemon_enforced";
  authorityNotice: typeof APPROVAL_ENFORCEMENT_NOTICE;
}
export interface ApprovalLifecycleEvent {
  eventId: string;
  tenant: string;
  approvalId: string;
  kind: ApprovalEventKind;
  at: TimestampSeconds;
  recordDigest: string;
  holdReason?: HoldReason;
  expiresAt?: TimestampSeconds;
  submissionRef?: string;
  reason?: string;
  deterministicExpiry?: boolean;
  defectCode?: string;
}

export interface NativeActivityV1 { version: "1"; module: string; ordinal: string }
export interface NativePurposeV1 {
  version: "1"; tenant: string; agent_did: string; session_id: string; generation: string;
  expires_at_ms: string; capability_id: string; preparation_id: string; canonical_digest: string; commitment: string;
}
export interface NativeLocalGrantV1 {
  version: "1"; capability: string; session_scope: string; expires_at_ms: string; owner_public_key: string; signature: string;
}
export interface NativePrepareRequestV1 {
  variant: "native_v1"; activity: NativeActivityV1; actor: string; authority: string;
  account_sequence: string; not_before: string; not_after: string; idempotency_key: string;
  fee_limit: string; payload: string; payload_hash: string; capability_id: string;
  purpose: { purpose: NativePurposeV1; owner_public_key: string; signature: string };
  local_grant?: NativeLocalGrantV1 | null;
}
export interface NativePrepareResultV1 {
  version: "1"; preparation_id: string; canonical_bytes: string; signing_preimage: string;
  activity: NativeActivityV1; approval_required: boolean; approval_id: string | null;
}
export interface NativeApprovalDecisionV1 {
  variant: "native_v1"; approval_id: string; held_digest: string; current_sequence: string;
}
export interface NativeApprovalResultV1 {
  version: "1"; approval_id: string; held_digest: string; activity: NativeActivityV1;
  state: "Awaiting" | "Granted" | "Rejected" | "Expired" | "Defective" | "NotRequired";
  submission_ref: string | null;
}
export interface NativeApprovalListResultV1 { version: "1"; approvals: NativeApprovalResultV1[] }

function nativeRecord(value: unknown, fields: readonly string[], optional: readonly string[] = []): Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) throw new TypeError("native_v1.object");
  const object = value as Record<string, unknown>;
  if (fields.some((field) => !Object.prototype.hasOwnProperty.call(object, field))
    || Object.keys(object).some((field) => !fields.includes(field) && !optional.includes(field))) throw new TypeError("native_v1.fields");
  return object;
}
function nativeText(value: unknown, maximum: number): string {
  if (typeof value !== "string" || value.length === 0 || new TextEncoder().encode(value).length > maximum || value.includes("\0")) throw new TypeError("native_v1.text");
  return value;
}
function nativeDecimal(value: unknown, maximum = 18446744073709551615n): string {
  if (typeof value !== "string" || !/^(0|[1-9][0-9]*)$/u.test(value) || value.length > 39 || BigInt(value) > maximum) throw new TypeError("native_v1.integer");
  return value;
}
function nativeHex(value: unknown, bytes: number, exact = true): string {
  if (typeof value !== "string" || !/^(?:[0-9a-f]{2})+$/u.test(value) || (exact ? value.length !== bytes * 2 : value.length > bytes * 2)) throw new TypeError("native_v1.hex");
  return value;
}
function nativeVersion(value: unknown): "1" { if (value !== "1") throw new TypeError("native_v1.version"); return value; }
function nativeOptionalId(value: unknown): string | null { return value === null ? null : nativeHex(value, 32); }
function nativeActivity(value: unknown): NativeActivityV1 {
  const item = nativeRecord(value, ["version", "module", "ordinal"]);
  const module = nativeDecimal(item.module, 11n), ordinal = nativeDecimal(item.ordinal, 65535n);
  if (module === "0" || ordinal === "0") throw new TypeError("native_v1.activity");
  return { version: nativeVersion(item.version), module, ordinal };
}

export function encodeNativePrepareRequest(value: NativePrepareRequestV1): NativePrepareRequestV1 {
  const item = nativeRecord(value, ["variant", "activity", "actor", "authority", "account_sequence", "not_before", "not_after", "idempotency_key", "fee_limit", "payload", "payload_hash", "capability_id", "purpose"], ["local_grant"]);
  if (item.variant !== "native_v1") throw new TypeError("native_v1.variant");
  const signed = nativeRecord(item.purpose, ["purpose", "owner_public_key", "signature"]);
  const input = nativeRecord(signed.purpose, ["version", "tenant", "agent_did", "session_id", "generation", "expires_at_ms", "capability_id", "preparation_id", "canonical_digest", "commitment"]);
  const purpose: NativePurposeV1 = { version:nativeVersion(input.version), tenant:nativeText(input.tenant,255),
    agent_did:nativeText(input.agent_did,255), session_id:nativeHex(input.session_id,32), generation:nativeDecimal(input.generation),
    expires_at_ms:nativeDecimal(input.expires_at_ms), capability_id:nativeHex(input.capability_id,32), preparation_id:nativeHex(input.preparation_id,32),
    canonical_digest:nativeHex(input.canonical_digest,32), commitment:nativeHex(input.commitment,32) };
  const activity = nativeActivity(item.activity), actor = nativeText(item.actor,255), capability_id = nativeHex(item.capability_id,32);
  const not_before = nativeDecimal(item.not_before), not_after = nativeDecimal(item.not_after);
  if (activity.module !== "9" || actor !== purpose.agent_did || capability_id !== purpose.capability_id
    || purpose.generation === "0" || purpose.expires_at_ms === "0" || BigInt(not_before) > BigInt(not_after)) throw new TypeError("native_v1.binding");
  let local_grant: NativeLocalGrantV1 | null = null;
  if (item.local_grant !== undefined && item.local_grant !== null) {
    const grant = nativeRecord(item.local_grant, ["version", "capability", "session_scope", "expires_at_ms", "owner_public_key", "signature"]);
    local_grant = { version:nativeVersion(grant.version), capability:nativeHex(grant.capability,1048576,false), session_scope:nativeHex(grant.session_scope,1048576,false),
      expires_at_ms:nativeDecimal(grant.expires_at_ms), owner_public_key:nativeHex(grant.owner_public_key,32), signature:nativeHex(grant.signature,64) };
    if (local_grant.expires_at_ms === "0") throw new TypeError("native_v1.expiry");
  }
  return { variant:"native_v1", activity, actor, authority:nativeText(item.authority,524288),
    account_sequence:nativeDecimal(item.account_sequence), not_before, not_after, idempotency_key:nativeHex(item.idempotency_key,32),
    fee_limit:nativeDecimal(item.fee_limit,340282366920938463463374607431768211455n), payload:nativeHex(item.payload,524288,false),
    payload_hash:nativeHex(item.payload_hash,32), capability_id,
    purpose:{purpose,owner_public_key:nativeHex(signed.owner_public_key,32),signature:nativeHex(signed.signature,64)}, local_grant };
}

export function encodeNativeApprovalDecision(value: NativeApprovalDecisionV1): NativeApprovalDecisionV1 {
  const item = nativeRecord(value,["variant","approval_id","held_digest","current_sequence"]);
  if (item.variant !== "native_v1") throw new TypeError("native_v1.variant");
  return {variant:"native_v1",approval_id:nativeHex(item.approval_id,32),held_digest:nativeHex(item.held_digest,32),current_sequence:nativeDecimal(item.current_sequence)};
}
export function encodeNativeApprovalGet(approval_id: string): {variant:"native_v1";approval_id:string} {
  return {variant:"native_v1",approval_id:nativeHex(approval_id,32)};
}
export function decodeNativePrepareResult(value: unknown): NativePrepareResultV1 {
  const item = nativeRecord(value,["version","preparation_id","canonical_bytes","signing_preimage","activity","approval_required","approval_id"]);
  const approval_id = nativeOptionalId(item.approval_id);
  if (typeof item.approval_required !== "boolean" || item.approval_required !== (approval_id !== null)) throw new TypeError("native_v1.approval");
  return {version:nativeVersion(item.version),preparation_id:nativeHex(item.preparation_id,32),canonical_bytes:nativeHex(item.canonical_bytes,1048576,false),
    signing_preimage:nativeHex(item.signing_preimage,32),activity:nativeActivity(item.activity),approval_required:item.approval_required,approval_id};
}
export function decodeNativeApprovalResult(value: unknown): NativeApprovalResultV1 {
  const item = nativeRecord(value,["version","approval_id","held_digest","activity","state","submission_ref"]);
  const state = item.state;
  if (state !== "Awaiting" && state !== "Granted" && state !== "Rejected" && state !== "Expired" && state !== "Defective" && state !== "NotRequired") throw new TypeError("native_v1.state");
  return {version:nativeVersion(item.version),approval_id:nativeHex(item.approval_id,32),held_digest:nativeHex(item.held_digest,32),activity:nativeActivity(item.activity),state,submission_ref:nativeOptionalId(item.submission_ref)};
}
export function decodeNativeApprovalListResult(value: unknown): NativeApprovalListResultV1 {
  const item = nativeRecord(value,["version","approvals"]);
  if (!Array.isArray(item.approvals) || item.approvals.length > 100) throw new TypeError("native_v1.list");
  return {version:nativeVersion(item.version),approvals:item.approvals.map(decodeNativeApprovalResult)};
}


export interface Transport {
  call<TRequest, TResponse>(operation: Operation, request: TRequest): Promise<TResponse>;
}

export class Client {
  public constructor(private readonly transport: Transport) {}

  public call<TRequest, TResponse>(operation: Operation, request: TRequest): Promise<TResponse> {
    return this.transport.call<TRequest, TResponse>(operation, request);
  }

  public approvalList(request: ApprovalListRequest): Promise<ApprovalPage> {
    return this.call("approval.list", request);
  }

  public approvalGet(request: ApprovalGetRequest): Promise<ApprovalRecord> {
    return this.call("approval.get", request);
  }

  public approvalApprove(request: ApprovalApproveRequest): Promise<ApprovalDecision> {
    return this.call("approval.approve", request);
  }

  public approvalReject(request: ApprovalRejectRequest): Promise<ApprovalDecision> {
    return this.call("approval.reject", request);
  }
}
