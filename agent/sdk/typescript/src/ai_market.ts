import * as http from "node:http";
import * as https from "node:https";
import { mkdir, open, readFile, rename } from "node:fs/promises";
import { join } from "node:path";
import { ed25519 } from "@noble/curves/ed25519.js";
import { sha256 } from "@noble/hashes/sha2.js";

import type { LayerXKeyCredential } from "./agent-http.js";
import { decodeNativePrepareResult, type NativePrepareResultV1 } from "./generated/client.js";
import { decodeNativeProgramCallV1, encodeNativeProgramCallV1, type NativeProgramCallV1 } from "./native-program-call.js";
import { PlatformSdkError, idempotencyKey } from "./production.js";
import { NativeProgramRequestV1, type ProgramOperations, type ProgramSubmission } from "./programs.js";
import type { CheckpointVerification } from "./verifier.js";

/** Values of platform/sdk/schema/paxai-v1.kvx [limits]. */
export const PAXAI_LIMITS = Object.freeze({
  finalizedRank: 4,
  settlementRank: 5,
  defaultPageRows: 16,
  maxPageRows: 32,
  pageMaxBytes: 65_536,
  cursorTtlMs: 900_000,
  cursorMaxBytes: 1_024,
  authorityFreshnessHeights: 8n,
  envelopeMaxBytes: 16_384,
  payloadMaxBytes: 15_965,
} as const);

export const NATIVE_CALL_PROTOCOL_VERSION = 3;
export const PROGRAMS_MODULE = 9;
export const PROGRAM_CALL_ORDINAL = 3;
export const GUEST_ABI: number = 5;
export const ENTRYPOINT = "layerx_call";
export const FUNDING_POLICY_VERSION = 1n;

/** Program application refusals the client reproduces before signing (errors.rs). */
export const AI_APPLICATION_ERRORS = Object.freeze({
  BAD_VERSION: 0x0001,
  NON_CANONICAL: 0x0002,
  WRONG_DOMAIN: 0x0003,
  WRONG_PROGRAM: 0x0004,
  WRONG_MARKET: 0x0005,
  UNAUTHORIZED: 0x0006,
  WRONG_ROSTER: 0x000e,
  CAPACITY: 0x0016,
  UNKNOWN_OPERATION: 0x001f,
  F06_FUNDING_POLICY_MISMATCH: 0x0601,
  F06_INVALID_AMOUNT: 0x0604,
  F06_UNKNOWN_WORKER_ENTITLEMENT: 0x060b,
  F06_NOTHING_TO_CLAIM: 0x060e,
  F06_CONTRIBUTION_CONSENT_REQUIRED: 0x0610,
} as const);

const RESPONSE_MAX_BYTES = 262_144;
const DEFAULT_TIMEOUT_MS = 30_000;
const U64_MAX = 0xffff_ffff_ffff_ffffn;
const U128_MAX = (1n << 128n) - 1n;
const HEX32 = /^[0-9a-f]{64}$/u;
const JSON_ID = /^0x[0-9a-f]{64}$/u;
const DECIMAL = /^(0|[1-9][0-9]*)$/u;
const CURSOR = /^(?:[0-9a-f]{2})+$/u;
const ZERO32 = "0".repeat(64);
const ACTIVITY_TYPE = (PROGRAMS_MODULE << 16) + PROGRAM_CALL_ORDINAL;
const ACTIVITY_DOMAIN = ascii("LXP/v1/activity-id\0");
const PAYLOAD_DOMAIN = ascii("LXP/v1/payload-hash\0");
const SIGNATURE_DOMAIN = ascii("LXP/v1/signature-preimage\0");
const RECORD_MAGIC = ascii("PAXAIOP1");
const ENVELOPE_MAGIC = ascii("PAXAI1");
const TOKEN = Symbol("paxai");

export type Hex32 = string;

export type ReviewField = "action" | "chain" | "program" | "market" | "actor" | "epoch" | "config" | "roster" | "policy"
  | "snapshot" | "amount" | "asset" | "payee" | "worker" | "capabilities" | "access_declaration" | "response_capacity"
  | "resources" | "fee_limit" | "validity" | "expiry" | "idempotency_key" | "authority" | "commitment";

export type OperationState = "prepared" | "reviewed" | "signed" | "submitting" | "pending" | "executed" | "finalized"
  | "failed" | "unknown";
const STATE_CODES: readonly OperationState[] = ["prepared", "reviewed", "signed", "submitting", "pending", "executed",
  "finalized", "failed", "unknown"];
export type DomainStatus = "pending" | "completed";

export type AiMarketErrorCode =
  | "BindingMismatch" | "FinalityUnavailable" | "SnapshotConflict" | "IntegrityFailure" | "CursorExpired"
  | "CursorMismatch" | "ResponseTooLarge"
  | "StaleAuthority" | "NotNativeProgramCall" | "NotMutation" | "ReviewMismatch" | "UnauthorizedKey"
  | "InvalidTransition" | "ReceiptMismatch" | "FinalityMismatch" | "HistoryUnavailable" | "CorruptRecord"
  | "InvalidEncoding" | "Application" | "NativeCall" | "Signature" | "Service" | "Journal";

export interface AiMarketErrorDetail {
  readonly field?: ReviewField;
  readonly lag?: bigint;
  readonly applicationCode?: number;
  readonly category?: string;
  readonly status?: number;
  readonly from?: OperationState;
  readonly epochStatus?: EpochStatus;
  readonly cause?: unknown;
}

export class AiMarketError extends Error {
  public readonly code: AiMarketErrorCode;
  public readonly detail: Readonly<AiMarketErrorDetail>;
  public constructor(code: AiMarketErrorCode, detail: AiMarketErrorDetail = {}) {
    const suffix = detail.field ?? detail.category ?? detail.from ?? detail.epochStatus
      ?? (detail.applicationCode === undefined ? undefined : `0x${detail.applicationCode.toString(16).padStart(4, "0")}`);
    super(suffix === undefined ? code : `${code}: ${suffix}`);
    this.name = "AiMarketError";
    this.code = code;
    this.detail = Object.freeze({ ...detail });
  }
}

function refuse(code: AiMarketErrorCode, detail?: AiMarketErrorDetail): never { throw new AiMarketError(code, detail); }
function application(code: number): never { throw new AiMarketError("Application", { applicationCode: code }); }
function invalid(): never { return refuse("InvalidEncoding"); }

export type CallBoundary = "mutation" | "program-read";
export interface OperationSpec {
  readonly name: string;
  readonly selector: number;
  readonly feature: number;
  readonly boundary: CallBoundary;
  readonly sequence: "role" | "object-local";
  readonly delegation: "native-only" | "delegate";
  readonly payloadMin: number;
  readonly payloadMax: number;
}

type OperationRow = readonly [string, number, number, CallBoundary, OperationSpec["sequence"], OperationSpec["delegation"], number, number];
const OPERATION_ROWS: readonly OperationRow[] = [
  ["CREATE", 0x0101, 1, "mutation", "role", "native-only", 0, 15965],
  ["STAGE_POLICY", 0x0102, 1, "mutation", "role", "native-only", 0, 15965],
  ["CANCEL_POLICY", 0x0103, 1, "mutation", "role", "native-only", 16, 16],
  ["SCHEDULE_ACTIVATION", 0x0104, 1, "mutation", "role", "native-only", 16, 16],
  ["ADVANCE_ACTIVATION", 0x0105, 1, "mutation", "object-local", "native-only", 16, 16],
  ["SUSPEND", 0x0106, 1, "mutation", "role", "native-only", 40, 48],
  ["UPDATE_METADATA", 0x0107, 1, "mutation", "role", "native-only", 40, 48],
  ["APPOINT_OPERATOR", 0x0108, 1, "mutation", "role", "native-only", 49, 49],
  ["REVOKE_OPERATOR", 0x0109, 1, "mutation", "role", "native-only", 16, 16],
  ["REQUEST_CLOSE", 0x010a, 1, "mutation", "role", "native-only", 40, 40],
  ["ADVANCE_CLOSE", 0x010b, 1, "mutation", "object-local", "native-only", 11, 11],
  ["ADMIT_TASK", 0x010c, 1, "mutation", "object-local", "native-only", 248, 248],
  ["ACCEPT_TASK", 0x010d, 1, "mutation", "role", "delegate", 72, 72],
  ["CANCEL_TASK", 0x010e, 1, "mutation", "object-local", "native-only", 32, 32],
  ["COMMIT_TASK_RESULT", 0x010f, 1, "mutation", "role", "delegate", 72, 72],
  ["SEAL_TASK_SET", 0x0110, 1, "mutation", "object-local", "native-only", 48, 48],
  ["OPEN_EPOCH", 0x0111, 1, "mutation", "object-local", "native-only", 0, 15965],
  ["EnrollWorker", 0x0201, 2, "mutation", "role", "native-only", 136, 136],
  ["PublishMetadata", 0x0202, 2, "mutation", "role", "delegate", 0, 15965],
  ["SetDraining", 0x0203, 2, "mutation", "role", "native-only", 0, 15965],
  ["UndoDrain", 0x0204, 2, "mutation", "role", "native-only", 0, 15965],
  ["RotateDelegate", 0x0205, 2, "mutation", "role", "native-only", 0, 15965],
  ["RevokeDelegate", 0x0206, 2, "mutation", "role", "native-only", 0, 15965],
  ["RetireWorker", 0x0207, 2, "mutation", "role", "native-only", 0, 15965],
  ["AcceptEnrollment", 0x0209, 2, "mutation", "role", "native-only", 144, 144],
  ["ExpireEnrollment", 0x020a, 2, "mutation", "role", "native-only", 72, 72],
  ["ScheduleEvaluator", 0x0301, 3, "mutation", "role", "native-only", 160, 160],
  ["RotateEvaluatorKey", 0x0302, 3, "mutation", "role", "native-only", 88, 88],
  ["RevokeEvaluator", 0x0303, 3, "mutation", "role", "native-only", 74, 74],
  ["ChallengeAssessment", 0x0304, 3, "mutation", "object-local", "native-only", 97, 97],
  ["CommitScore", 0x0401, 4, "mutation", "role", "delegate", 224, 224],
  ["RevealScore", 0x0402, 4, "mutation", "role", "delegate", 364, 1480],
  ["BeginAggregation", 0x0501, 5, "mutation", "object-local", "native-only", 0, 0],
  ["ProcessAggregation", 0x0502, 5, "mutation", "object-local", "native-only", 34, 34],
  ["FinalizeAggregation", 0x0503, 5, "mutation", "object-local", "native-only", 32, 32],
  ["FUND", 0x0601, 6, "mutation", "role", "native-only", 57, 57],
  ["CLAIM", 0x0602, 6, "mutation", "object-local", "native-only", 80, 80],
  ["EXPIRE_EPOCH_CLAIMS", 0x0603, 6, "mutation", "object-local", "native-only", 0, 0],
  ["REFUND_FREE", 0x0604, 6, "mutation", "object-local", "native-only", 64, 64],
  ["PRUNE_EPOCH", 0x0605, 6, "mutation", "object-local", "native-only", 0, 0],
  ["ResetHistory", 0x0701, 7, "mutation", "role", "native-only", 73, 73],
  ["SuspendHistory", 0x0702, 7, "mutation", "role", "native-only", 73, 73],
  ["ResumeHistory", 0x0703, 7, "mutation", "role", "native-only", 73, 73],
  ["ApproveAdmission", 0x0801, 8, "mutation", "role", "native-only", 0, 15965],
  ["RevokeAdmissionApproval", 0x0802, 8, "mutation", "role", "native-only", 0, 15965],
  ["AdmitWorker", 0x0803, 8, "mutation", "role", "native-only", 0, 15965],
  ["AdmitEvaluator", 0x0804, 8, "mutation", "role", "native-only", 0, 15965],
  ["Heartbeat", 0x0805, 8, "mutation", "role", "delegate", 0, 15965],
  ["RequestExit", 0x0806, 8, "mutation", "role", "native-only", 0, 15965],
  ["CancelExit", 0x0807, 8, "mutation", "role", "native-only", 0, 15965],
  ["PruneInactive", 0x0808, 8, "mutation", "object-local", "native-only", 0, 15965],
  ["AdministrativeRemove", 0x0809, 8, "mutation", "role", "native-only", 0, 15965],
  ["SealEvidence", 0x0901, 9, "mutation", "role", "delegate", 131, 131],
  ["READ_HEADER", 0x0a01, 10, "program-read", "object-local", "native-only", 0, 0],
  ["READ_STATE_CHUNK", 0x0a02, 10, "program-read", "object-local", "native-only", 46, 46],
];

/** The [operations] table of the shared schema, keyed by selector. */
export const PAXAI_OPERATIONS: ReadonlyMap<number, OperationSpec> = new Map(OPERATION_ROWS.map(
  ([name, selector, feature, boundary, sequence, delegation, payloadMin, payloadMax]) => [selector,
    Object.freeze({ name, selector, feature, boundary, sequence, delegation, payloadMin, payloadMax })]));

export const FUND = 0x0601;
export const CLAIM = 0x0602;
export const REFUND_FREE = 0x0604;

function registryBeforeEpoch(selector: number): boolean {
  return (selector >= 0x0101 && selector <= 0x010b) || selector === 0x0111 || (selector >= 0x0201 && selector <= 0x0207)
    || selector === 0x0209 || selector === 0x020a || (selector >= 0x0301 && selector <= 0x0303) || selector === 0x0601
    || selector === 0x0604 || (selector >= 0x0801 && selector <= 0x0804) || selector === 0x0a01 || selector === 0x0a02;
}

/** Exact H(D, B) = SHA-256(ASCII D || 0x00 || B); an all-zero digest refuses. */
export function domainHash(domain: string, bytes: Uint8Array): Hex32 {
  if (domain.length === 0 || !/^[\x01-\x7f]+$/u.test(domain)) application(AI_APPLICATION_ERRORS.NON_CANONICAL);
  const digest = hex(sha256(concat(ascii(domain), new Uint8Array([0]), bytes)));
  if (digest === ZERO32) application(AI_APPLICATION_ERRORS.NON_CANONICAL);
  return digest;
}

/** Canonical u64 decimal: no sign, no leading zero, at most 20 digits. */
export function parseDecimalU64(text: string): bigint { return decimal(text, U64_MAX); }
/** Canonical u128 decimal: no sign, no leading zero, at most 39 digits. */
export function parseDecimalU128(text: string): bigint { return decimal(text, U128_MAX); }
function decimal(text: unknown, maximum: bigint): bigint {
  if (typeof text !== "string" || text.length > 39 || !DECIMAL.test(text)) invalid();
  const value = BigInt(text);
  return value > maximum ? invalid() : value;
}

class Writer {
  readonly #parts: Uint8Array[] = [];
  public put(bytes: Uint8Array): this { this.#parts.push(bytes.slice()); return this; }
  public u8(value: number): this { return this.unsigned(BigInt(value), 1); }
  public u16(value: number): this { return this.unsigned(BigInt(value), 2); }
  public u32(value: number): this { return this.unsigned(BigInt(value), 4); }
  public u64(value: bigint): this { return this.unsigned(value, 8); }
  public u128(value: bigint): this { return this.unsigned(value, 16); }
  public id(value: Hex32): this { return this.put(idBytes(value)); }
  public sized(bytes: Uint8Array): this { return this.u32(bytes.length).put(bytes); }
  public presence<T>(value: T | null, write: (value: T) => void): this {
    if (value === null) return this.u8(0);
    this.u8(1); write(value); return this;
  }
  public bytes(): Uint8Array { return concat(...this.#parts); }
  private unsigned(value: bigint, length: number): this {
    if (typeof value !== "bigint" || value < 0n || value >= 1n << BigInt(length * 8)) invalid();
    const out = new Uint8Array(length);
    let rest = value;
    for (let index = length - 1; index >= 0; index -= 1) { out[index] = Number(rest & 0xffn); rest >>= 8n; }
    return this.put(out);
  }
}

class Reader {
  #offset = 0;
  public constructor(private readonly input: Uint8Array, private readonly fail: () => never) {}
  public get offset(): number { return this.#offset; }
  public fixed(length: number): Uint8Array {
    const end = this.#offset + length;
    if (end > this.input.length) this.fail();
    const out = this.input.slice(this.#offset, end);
    this.#offset = end;
    return out;
  }
  public u8(): number { return this.fixed(1)[0] ?? this.fail(); }
  public u16(): number { return Number(this.unsigned(2)); }
  public u32(): number { return Number(this.unsigned(4)); }
  public u64(): bigint { return this.unsigned(8); }
  public u128(): bigint { return this.unsigned(16); }
  public id(): Hex32 { const value = hex(this.fixed(32)); return value === ZERO32 ? this.fail() : value; }
  public boolean(): boolean { const value = this.u8(); return value === 0 ? false : value === 1 ? true : this.fail(); }
  public sized(maximum: number, exact?: number): Uint8Array {
    const length = this.u32();
    if (length > maximum || (exact !== undefined && length !== exact)) this.fail();
    return this.fixed(length);
  }
  public end(): void { if (this.#offset !== this.input.length) this.fail(); }
  private unsigned(length: number): bigint {
    let value = 0n;
    for (const byte of this.fixed(length)) value = (value << 8n) | BigInt(byte);
    return value;
  }
}

// ---------------------------------------------------------------------------------------------
// Snapshot binding, views and pagination

export interface SnapshotBinding {
  readonly chain: Hex32;
  readonly program: Hex32;
  readonly market: Hex32;
  readonly observedSequence: bigint;
  readonly executionHeight: bigint;
  readonly batchId: Hex32;
  readonly nativeStateRoot: Hex32;
  readonly revision: bigint;
  readonly stateDigest: Hex32;
  readonly epoch: bigint | null;
  readonly config: bigint;
  readonly policy: Hex32;
  readonly roster: Hex32 | null;
  readonly checkpoint: Hex32;
  readonly settlement: Hex32 | null;
  readonly rank: number;
  readonly publicationTimeMs: bigint;
}

function bindingPrefix(binding: SnapshotBinding): Writer {
  if (binding.revision === 0n || binding.config === 0n || !Number.isInteger(binding.rank) || binding.rank < 0
    || binding.rank > PAXAI_LIMITS.finalizedRank) application(AI_APPLICATION_ERRORS.NON_CANONICAL);
  const w = new Writer().u16(1).id(binding.chain).id(binding.program).id(binding.market).u64(binding.observedSequence)
    .u64(binding.executionHeight).id(binding.batchId).id(binding.nativeStateRoot).u64(binding.revision)
    .id(binding.stateDigest);
  w.presence(binding.epoch, (epoch) => w.u64(epoch));
  w.u64(binding.config).id(binding.policy);
  return w.presence(binding.roster, (roster) => w.id(roster));
}

/** Canonical binding bytes (queries.rs SnapshotBinding::encode). */
export function encodeSnapshotBinding(binding: SnapshotBinding): Uint8Array {
  const w = bindingPrefix(binding).id(binding.checkpoint);
  w.presence(binding.settlement, (settlement) => w.id(settlement));
  return w.u8(binding.rank).u64(binding.publicationTimeMs).put(new Uint8Array(8)).bytes();
}

/** Content identity over the binding prefix; excludes finality evidence, rank and publication time. */
export function snapshotIdOf(binding: SnapshotBinding): Hex32 {
  return domainHash("PAXAI/view/v1", bindingPrefix(binding).bytes());
}

export type Freshness = Readonly<{ label: "current" }> | Readonly<{ label: "stale"; lag: bigint }> | Readonly<{ label: "unknown" }>;

/** Freshness of a binding against an independently verified authority height. */
export function freshnessOf(binding: SnapshotBinding, authorityHeight: bigint | null): Freshness {
  if (authorityHeight === null) return Object.freeze({ label: "unknown" });
  const lag = authorityHeight > binding.executionHeight ? authorityHeight - binding.executionHeight : 0n;
  return Object.freeze(lag > PAXAI_LIMITS.authorityFreshnessHeights ? { label: "stale", lag } : { label: "current" });
}

/** Routing and signing fail closed on an authority more than eight heights ahead. */
export function requireCurrent(binding: SnapshotBinding, authorityHeight: bigint): void {
  const freshness = freshnessOf(binding, authorityHeight);
  if (freshness.label === "stale") refuse("StaleAuthority", { lag: freshness.lag });
}

export type ProjectionState = "observed-unverified" | "evidence-verified-unfinalized" | "finalized-publishable" | "archived" | "quarantined";
export type Availability = "available" | "not-enabled" | "not-yet-produced" | "content-unavailable" | "unsupported-version";
export type ScoreStatus = "present" | "no-admissible-score" | "insufficient-coverage" | "not-produced" | "unavailable" | "unsupported";
export type EpochStatus = "retained" | "never-opened" | "retained-terminal" | "archive-required" | "archive-unavailable" | "unsupported-version";
export type ParticipantKind = "worker" | "evaluator";
export type KindFilter = "all" | ParticipantKind;
export type FeatureName = "F01" | "F02" | "F03" | "F04" | "F05" | "F06" | "F07" | "F08" | "F09" | "F10";
export type Components = Readonly<Record<FeatureName, Availability>>;

const PROJECTIONS: readonly ProjectionState[] = ["observed-unverified", "evidence-verified-unfinalized", "finalized-publishable", "archived", "quarantined"];
const AVAILABILITY: readonly Availability[] = ["available", "not-enabled", "not-yet-produced", "content-unavailable", "unsupported-version"];
const SCORE_STATUS: readonly ScoreStatus[] = ["present", "no-admissible-score", "insufficient-coverage", "not-produced", "unavailable", "unsupported"];
const EPOCH_STATUS: readonly EpochStatus[] = ["retained", "never-opened", "retained-terminal", "archive-required", "archive-unavailable", "unsupported-version"];
const FEATURES: readonly FeatureName[] = ["F01", "F02", "F03", "F04", "F05", "F06", "F07", "F08", "F09", "F10"];
const ELIGIBLE_SERVING = 1;
const PPM_MAX = 1_000_000;

export interface MarketView {
  readonly snapshotId: Hex32;
  readonly projection: ProjectionState;
  readonly binding: SnapshotBinding;
  readonly components: Components;
  readonly sourceActivity: Hex32;
  readonly freshness: Freshness;
}

export interface ParticipantRow {
  readonly kind: ParticipantKind;
  readonly id: Hex32;
  readonly owner: Hex32;
  readonly generation: bigint;
  readonly identityState: number;
  readonly frozenMember: boolean;
  readonly frozenGeneration: bigint | null;
  readonly eligibility: number;
  readonly metadata: Hex32 | null;
  readonly metadataRevision: bigint;
  readonly score: Readonly<{ status: ScoreStatus; epoch: bigint | null; ppm: number | null }>;
  readonly reward: Readonly<{ status: Availability; asset: Hex32 | null; earned: bigint | null; claimed: bigint | null }>;
  readonly history: Readonly<{ status: Availability; digest: Hex32 | null }>;
}

export interface ParticipantQuery {
  readonly market: Hex32;
  readonly snapshot?: Hex32;
  readonly kind?: KindFilter;
  readonly activeOnly?: boolean;
  readonly limit?: number;
}

export interface ParticipantPage {
  readonly snapshotId: Hex32;
  readonly binding: SnapshotBinding;
  readonly components: Components;
  readonly rows: readonly ParticipantRow[];
  readonly next: ParticipantCursor | null;
}

export interface EpochEntry { readonly epoch: bigint; readonly status: EpochStatus; readonly snapshotId: Hex32 | null }
export interface EpochPage { readonly component: Availability; readonly entries: readonly EpochEntry[] }

interface NormalizedQuery { readonly market: Hex32; readonly snapshot: Hex32 | null; readonly kind: KindFilter; readonly activeOnly: boolean; readonly limit: number }

const CURSOR_ISSUERS = new WeakMap<ParticipantCursor, AiMarketViews>();

/** Opaque continuation pinned to one snapshot, filter and reader; it grants no access of its own. */
export class ParticipantCursor {
  public readonly token: string;
  public readonly market: Hex32;
  public readonly snapshotId: Hex32;
  public readonly kind: KindFilter;
  public readonly activeOnly: boolean;
  public readonly binding: SnapshotBinding;
  readonly #after: string;
  public constructor(token: typeof TOKEN, value: Readonly<{ token: string; query: NormalizedQuery; snapshotId: Hex32; binding: SnapshotBinding; after: ParticipantRow }>) {
    if (token !== TOKEN) throw new TypeError("participant cursors are issued by AiMarketViews");
    this.token = value.token; this.market = value.query.market; this.snapshotId = value.snapshotId;
    this.kind = value.query.kind; this.activeOnly = value.query.activeOnly; this.binding = value.binding;
    this.#after = rowKey(value.after);
    Object.freeze(this);
  }
  /** Ordering key of the last row already returned. */
  public after(): string { return this.#after; }
}

function normalizeQuery(query: ParticipantQuery): NormalizedQuery {
  const kind = query.kind ?? "all";
  const limit = query.limit ?? PAXAI_LIMITS.defaultPageRows;
  if (!isId(query.market) || (query.snapshot !== undefined && !isId(query.snapshot))
    || (kind !== "all" && kind !== "worker" && kind !== "evaluator") || (query.activeOnly !== undefined && typeof query.activeOnly !== "boolean")
    || !Number.isInteger(limit) || limit < 1 || limit > PAXAI_LIMITS.maxPageRows) invalid();
  return { market: query.market, snapshot: query.snapshot ?? null, kind, activeOnly: query.activeOnly ?? false, limit };
}

function checkCursorToken(token: unknown): string {
  if (typeof token !== "string") return invalid();
  if (token.length > PAXAI_LIMITS.cursorMaxBytes) refuse("ResponseTooLarge");
  return CURSOR.test(token) ? token : invalid();
}

function exact(value: unknown, keys: readonly string[], optional: readonly string[] = []): Readonly<Record<string, unknown>> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) invalid();
  const item = value as Record<string, unknown>;
  if (keys.some((key) => !Object.prototype.hasOwnProperty.call(item, key))
    || Object.keys(item).some((key) => !keys.includes(key) && !optional.includes(key))) invalid();
  return item;
}
function jsonId(value: unknown): Hex32 {
  if (typeof value !== "string" || !JSON_ID.test(value) || value.slice(2) === ZERO32) invalid();
  return value.slice(2);
}
function jsonOptional<T>(value: unknown, decode: (value: unknown) => T): T | null { return value === null ? null : decode(value); }
function jsonU64(value: unknown): bigint { return decimal(value, U64_MAX); }
function jsonU128(value: unknown): bigint { return decimal(value, U128_MAX); }
function jsonEnum<T extends string>(value: unknown, table: readonly T[]): T {
  return typeof value === "string" && (table as readonly string[]).includes(value) ? value as T : invalid();
}
function jsonInteger(value: unknown, maximum: number): number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0 && value <= maximum ? value : invalid();
}

function decodeBinding(value: unknown): SnapshotBinding {
  const b = exact(value, ["chain", "program", "market", "observed_sequence", "execution_height", "batch_id", "native_state_root",
    "revision", "state_digest", "epoch", "config", "policy", "roster", "checkpoint", "settlement", "rank", "publication_time_ms"]);
  const binding: SnapshotBinding = Object.freeze({
    chain: jsonId(b.chain), program: jsonId(b.program), market: jsonId(b.market),
    observedSequence: jsonU64(b.observed_sequence), executionHeight: jsonU64(b.execution_height),
    batchId: jsonId(b.batch_id), nativeStateRoot: jsonId(b.native_state_root), revision: jsonU64(b.revision),
    stateDigest: jsonId(b.state_digest), epoch: jsonOptional(b.epoch, jsonU64), config: jsonU64(b.config),
    policy: jsonId(b.policy), roster: jsonOptional(b.roster, jsonId), checkpoint: jsonId(b.checkpoint),
    settlement: jsonOptional(b.settlement, jsonId), rank: jsonInteger(b.rank, PAXAI_LIMITS.finalizedRank),
    publicationTimeMs: jsonU64(b.publication_time_ms),
  });
  if (binding.revision === 0n || binding.config === 0n) invalid();
  return binding;
}

function bindSnapshot(binding: SnapshotBinding, servedId: unknown, market: Hex32): Hex32 {
  const snapshotId = jsonId(servedId);
  if (binding.market !== market) refuse("BindingMismatch");
  if (snapshotIdOf(binding) !== snapshotId) refuse("IntegrityFailure");
  if (binding.rank < PAXAI_LIMITS.finalizedRank) refuse("FinalityUnavailable");
  return snapshotId;
}

function decodeComponents(value: unknown): Components {
  const item = exact(value, FEATURES);
  return Object.freeze(Object.fromEntries(FEATURES.map((name) => [name, jsonEnum(item[name], AVAILABILITY)]))) as Components;
}

function decodeFreshness(value: unknown): Freshness {
  const item = exact(value, ["label"], ["lag"]);
  if (item.label === "stale") return Object.freeze({ label: "stale", lag: jsonU64(exact(value, ["label", "lag"]).lag) });
  exact(value, ["label"]);
  return Object.freeze({ label: jsonEnum(item.label, ["current", "unknown"] as const) });
}

/** Strict GET_MARKET_VIEW result for `market`. */
export function decodeMarketView(value: unknown, market: Hex32): MarketView {
  const item = exact(value, ["snapshot_id", "projection", "binding", "components", "source_activity", "freshness"]);
  const binding = decodeBinding(item.binding);
  return Object.freeze({
    snapshotId: bindSnapshot(binding, item.snapshot_id, market), projection: jsonEnum(item.projection, PROJECTIONS), binding,
    components: decodeComponents(item.components), sourceActivity: jsonId(item.source_activity), freshness: decodeFreshness(item.freshness),
  });
}

function decodeRow(value: unknown): ParticipantRow {
  const r = exact(value, ["kind", "id", "owner", "generation", "identity_state", "frozen_member", "frozen_generation", "eligibility",
    "metadata", "metadata_revision", "score", "reward", "history"]);
  const s = exact(r.score, ["status", "epoch", "ppm"]);
  const w = exact(r.reward, ["status", "asset", "earned", "claimed"]);
  const h = exact(r.history, ["status", "digest"]);
  if (typeof r.frozen_member !== "boolean") invalid();
  const score = Object.freeze({ status: jsonEnum(s.status, SCORE_STATUS), epoch: jsonOptional(s.epoch, jsonU64),
    ppm: jsonOptional(s.ppm, (ppm) => jsonInteger(ppm, PPM_MAX)) });
  if (score.status === "present" ? score.epoch === null || score.ppm === null : score.ppm !== null) refuse("IntegrityFailure");
  const reward = Object.freeze({ status: jsonEnum(w.status, AVAILABILITY), asset: jsonOptional(w.asset, jsonId),
    earned: jsonOptional(w.earned, jsonU128), claimed: jsonOptional(w.claimed, jsonU128) });
  const entitled = reward.asset !== null && reward.earned !== null && reward.claimed !== null;
  if (reward.status === "available" ? !entitled || (reward.claimed ?? 0n) > (reward.earned ?? 0n)
    : reward.asset !== null || reward.earned !== null || reward.claimed !== null) refuse("IntegrityFailure");
  const history = Object.freeze({ status: jsonEnum(h.status, AVAILABILITY), digest: jsonOptional(h.digest, jsonId) });
  if ((history.status === "available") !== (history.digest !== null)) refuse("IntegrityFailure");
  return Object.freeze({
    kind: jsonEnum(r.kind, ["worker", "evaluator"] as const), id: jsonId(r.id), owner: jsonId(r.owner),
    generation: jsonU64(r.generation), identityState: jsonInteger(r.identity_state, 255), frozenMember: r.frozen_member,
    frozenGeneration: jsonOptional(r.frozen_generation, jsonU64), eligibility: jsonInteger(r.eligibility, 255),
    metadata: jsonOptional(r.metadata, jsonId), metadataRevision: jsonU64(r.metadata_revision), score, reward, history,
  });
}

function rowKey(row: ParticipantRow): string { return `${row.kind === "worker" ? 1 : 2}${row.id}`; }

function decodePage(value: unknown, query: NormalizedQuery, views: AiMarketViews, after: ParticipantCursor | null): ParticipantPage {
  const item = exact(value, ["snapshot_id", "binding", "components", "rows", "cursor"]);
  const binding = decodeBinding(item.binding);
  const snapshotId = bindSnapshot(binding, item.snapshot_id, query.market);
  if (query.snapshot !== null && snapshotId !== query.snapshot) refuse("SnapshotConflict");
  if (after !== null && !sameBinding(binding, after.binding)) refuse("SnapshotConflict");
  const components = decodeComponents(item.components);
  if (!Array.isArray(item.rows) || item.rows.length > query.limit) invalid();
  const rows = Object.freeze(item.rows.map(decodeRow));
  let previous = after?.after() ?? "";
  for (const row of rows) {
    const key = rowKey(row);
    if (key <= previous || (query.kind !== "all" && row.kind !== query.kind)
      || (query.activeOnly && (row.eligibility & ELIGIBLE_SERVING) === 0)
      || (row.reward.status === "available" && components.F06 !== "available")) refuse("IntegrityFailure");
    previous = key;
  }
  const token = item.cursor === null ? null : checkCursorToken(item.cursor);
  const last = rows[rows.length - 1];
  if (token !== null && last === undefined) refuse("IntegrityFailure");
  const next = token === null || last === undefined ? null
    : new ParticipantCursor(TOKEN, { token, query, snapshotId, binding, after: last });
  if (next !== null) CURSOR_ISSUERS.set(next, views);
  return Object.freeze({ snapshotId, binding, components, rows, next });
}

function sameBinding(left: SnapshotBinding, right: SnapshotBinding): boolean {
  return equal(encodeSnapshotBinding(left), encodeSnapshotBinding(right));
}

/** Strict GET_HISTORY result requested from `from` with `limit`. */
export function decodeEpochPage(value: unknown, from: bigint, limit: number): EpochPage {
  const item = exact(value, ["component", "entries"]);
  if (!Array.isArray(item.entries) || item.entries.length > limit) invalid();
  let previous: bigint | null = null;
  const entries = Object.freeze(item.entries.map((entry: unknown) => {
    const e = exact(entry, ["epoch", "status", "snapshot_id"]);
    const decoded = Object.freeze({ epoch: jsonU64(e.epoch), status: jsonEnum(e.status, EPOCH_STATUS), snapshotId: jsonOptional(e.snapshot_id, jsonId) });
    const sourceless = decoded.status === "never-opened" || decoded.status === "unsupported-version";
    if (decoded.epoch < from || (previous !== null && decoded.epoch <= previous) || sourceless !== (decoded.snapshotId === null)) refuse("IntegrityFailure");
    previous = decoded.epoch;
    return decoded;
  }));
  return Object.freeze({ component: jsonEnum(item.component, AVAILABILITY), entries });
}

/** Historical content is usable only while the epoch's immutable source is retained. */
export function requireRetainedEpoch(entry: EpochEntry): Hex32 {
  if ((entry.status !== "retained" && entry.status !== "retained-terminal") || entry.snapshotId === null) {
    refuse("HistoryUnavailable", { epochStatus: entry.status });
  }
  return entry.snapshotId;
}

const QUERY_CATEGORIES: Readonly<Record<string, AiMarketErrorCode>> = Object.freeze({
  "binding-mismatch": "BindingMismatch", "finality-unavailable": "FinalityUnavailable", "snapshot-conflict": "SnapshotConflict",
  "integrity-failure": "IntegrityFailure", "cursor-expired": "CursorExpired", "cursor-mismatch": "CursorMismatch",
  "response-too-large": "ResponseTooLarge", "invalid-encoding": "InvalidEncoding",
});

function gatewayResult(status: number, value: unknown): unknown {
  const envelope = exact(value, ["ok"], ["result", "error"]);
  if (envelope.ok === true && status === 200) return exact(value, ["ok", "result"]).result;
  if (envelope.ok !== false || status === 200) invalid();
  const error = exact(exact(value, ["ok", "error"]).error, ["code"], ["oldest", "lag"]);
  if (typeof error.code !== "string") invalid();
  if (error.code === "authority-stale") refuse("StaleAuthority", { lag: jsonU64(error.lag), status });
  const code = QUERY_CATEGORIES[error.code];
  return refuse(code ?? "Service", { category: error.code, status });
}

export interface AiMarketViewsOptions {
  readonly endpoint: URL | string;
  readonly credential?: LayerXKeyCredential;
  readonly timeoutMs?: number;
}

/** Authenticated reader of the gateway AI market views. */
export class AiMarketViews {
  readonly #endpoint: URL;
  readonly #credential: LayerXKeyCredential | undefined;
  readonly #timeoutMs: number;

  public constructor(options: AiMarketViewsOptions) {
    const endpoint = new URL(options.endpoint);
    const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
    if ((endpoint.protocol !== "https:" && endpoint.protocol !== "http:") || endpoint.username !== "" || endpoint.password !== ""
      || !Number.isSafeInteger(timeoutMs) || timeoutMs <= 0) throw new TypeError("invalid AI market view options");
    this.#endpoint = endpoint;
    this.#credential = options.credential;
    this.#timeoutMs = timeoutMs;
  }

  public async snapshot(market: Hex32, snapshot?: Hex32): Promise<MarketView> {
    if (!isId(market) || (snapshot !== undefined && !isId(snapshot))) invalid();
    const params = snapshot === undefined ? [] : [["snapshot", snapshot] as const];
    return decodeMarketView(await this.#get(`${market}/snapshot`, params), market);
  }

  /** One page; with `cursor`, the next page of exactly the snapshot, filter and reader it was issued for. */
  public async participants(query: ParticipantQuery, cursor?: ParticipantCursor): Promise<ParticipantPage> {
    const normalized = normalizeQuery(query);
    let pinned = normalized;
    if (cursor !== undefined) {
      if (CURSOR_ISSUERS.get(cursor) !== this || cursor.market !== normalized.market || cursor.kind !== normalized.kind
        || cursor.activeOnly !== normalized.activeOnly || (normalized.snapshot !== null && normalized.snapshot !== cursor.snapshotId)) {
        refuse("CursorMismatch");
      }
      checkCursorToken(cursor.token);
      pinned = { ...normalized, snapshot: cursor.snapshotId };
    }
    const params: (readonly [string, string])[] = [];
    if (pinned.snapshot !== null) params.push(["snapshot", pinned.snapshot]);
    params.push(["kind", pinned.kind], ["active_only", String(pinned.activeOnly)], ["limit", String(pinned.limit)]);
    if (cursor !== undefined) params.push(["cursor", cursor.token]);
    return decodePage(await this.#get(`${pinned.market}/participants`, params), pinned, this, cursor ?? null);
  }

  public async epochs(market: Hex32, from = 0n, limit: number = PAXAI_LIMITS.defaultPageRows): Promise<EpochPage> {
    if (!isId(market) || typeof from !== "bigint" || from < 0n || from > U64_MAX || !Number.isInteger(limit)
      || limit < 1 || limit > PAXAI_LIMITS.maxPageRows) invalid();
    return decodeEpochPage(await this.#get(`${market}/epochs`, [["from", from.toString()], ["limit", String(limit)]]), from, limit);
  }

  #get(view: string, params: readonly (readonly [string, string])[]): Promise<unknown> {
    const url = new URL(`v1/ai/markets/${view}`, this.#endpoint.href.endsWith("/") ? this.#endpoint : `${this.#endpoint.href}/`);
    url.search = params.map(([key, value]) => `${key}=${value}`).join("&");
    const headers: http.OutgoingHttpHeaders = { Accept: "application/json", "User-Agent": "layerx-typescript/0.1.0" };
    this.#credential?.use((authorization) => { headers.Authorization = authorization; });
    return new Promise<unknown>((resolve, reject) => {
      let settled = false;
      const finish = <T>(callback: (value: T) => void, value: T): void => { if (!settled) { settled = true; callback(value); } };
      const driver = url.protocol === "https:" ? https : http;
      const request = driver.request(url, { method: "GET", headers, timeout: this.#timeoutMs }, (response) => {
        const chunks: Buffer[] = [];
        let received = 0;
        response.on("data", (chunk: Buffer) => {
          received += chunk.length;
          if (received > RESPONSE_MAX_BYTES) { response.destroy(); finish(reject, new AiMarketError("ResponseTooLarge")); return; }
          chunks.push(Buffer.from(chunk));
        });
        response.on("end", () => {
          try {
            if (response.headers["content-type"]?.split(";")[0]?.trim() !== "application/json") invalid();
            let parsed: unknown;
            try { parsed = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(Buffer.concat(chunks))); } catch { invalid(); }
            finish(resolve, gatewayResult(response.statusCode ?? 0, parsed));
          } catch (error) { finish(reject, error); }
        });
        response.on("error", () => finish(reject, new PlatformSdkError({ code: "transport-failure", retry: "safe" })));
      });
      request.on("timeout", () => request.destroy());
      request.on("error", () => finish(reject, new PlatformSdkError({ code: "transport-failure", retry: "safe" })));
      request.end();
    });
  }
}

// ---------------------------------------------------------------------------------------------
// PAXAI request envelope and F06 payloads (codec.rs, rewards.rs)

export interface RequestEnvelope {
  readonly selector: number;
  readonly chain: Hex32;
  readonly program: Hex32;
  readonly market: Hex32;
  readonly actor: Hex32;
  readonly epoch: bigint;
  readonly config: bigint;
  readonly roster: Hex32 | null;
  readonly sequence: bigint;
  readonly expiry: bigint;
  readonly request: Hex32;
  readonly payload: Uint8Array;
  readonly delegated: boolean;
}

export interface DecodedEnvelope { readonly envelope: RequestEnvelope; readonly unsigned: Uint8Array }

function operationOf(selector: number): OperationSpec {
  return PAXAI_OPERATIONS.get(selector) ?? application(AI_APPLICATION_ERRORS.UNKNOWN_OPERATION);
}

function validateEnvelope(envelope: RequestEnvelope): OperationSpec {
  const spec = operationOf(envelope.selector);
  const length = envelope.payload.length;
  const metadataLength = (envelope.selector === 0x0106 || envelope.selector === 0x0107) && length !== 40 && length !== 48;
  if (metadataLength || length < spec.payloadMin || length > spec.payloadMax || length > PAXAI_LIMITS.payloadMaxBytes
    || envelope.expiry === 0n || (spec.sequence === "role" ? envelope.sequence === 0n : envelope.sequence !== 0n)) {
    application(AI_APPLICATION_ERRORS.NON_CANONICAL);
  }
  if (envelope.delegated && spec.delegation !== "delegate") application(AI_APPLICATION_ERRORS.UNAUTHORIZED);
  if (spec.boundary === "program-read"
    ? envelope.epoch !== 0n || envelope.config !== 0n || envelope.roster !== null || envelope.delegated
    : envelope.config === 0n) application(AI_APPLICATION_ERRORS.NON_CANONICAL);
  if (envelope.roster === null && (envelope.epoch !== 0n || !registryBeforeEpoch(envelope.selector))) {
    application(AI_APPLICATION_ERRORS.WRONG_ROSTER);
  }
  return spec;
}

/** Natively authenticated request envelope bytes. */
export function encodeRequestEnvelope(envelope: RequestEnvelope): Uint8Array {
  if (envelope.delegated) application(AI_APPLICATION_ERRORS.UNAUTHORIZED);
  validateEnvelope(envelope);
  return new Writer().put(ENVELOPE_MAGIC).u16(1).u16(envelope.selector).id(envelope.chain).id(envelope.program)
    .id(envelope.market).id(envelope.actor).u64(envelope.epoch).u64(envelope.config)
    .put(envelope.roster === null ? new Uint8Array(32) : idBytes(envelope.roster)).u64(envelope.sequence)
    .u64(envelope.expiry).id(envelope.request).sized(envelope.payload).u8(0).bytes();
}

export function decodeRequestEnvelope(input: Uint8Array): DecodedEnvelope {
  if (input.length > PAXAI_LIMITS.envelopeMaxBytes) application(AI_APPLICATION_ERRORS.CAPACITY);
  const r = new Reader(input, () => application(AI_APPLICATION_ERRORS.NON_CANONICAL));
  if (!equal(r.fixed(ENVELOPE_MAGIC.length), ENVELOPE_MAGIC)) application(AI_APPLICATION_ERRORS.NON_CANONICAL);
  if (r.u16() !== 1) application(AI_APPLICATION_ERRORS.BAD_VERSION);
  const selector = r.u16();
  operationOf(selector);
  const chain = r.id(), program = r.id(), market = r.id(), actor = r.id(), epoch = r.u64(), config = r.u64();
  const rosterBytes = hex(r.fixed(32));
  const sequence = r.u64(), expiry = r.u64(), request = r.id();
  const payloadLength = r.u32();
  if (payloadLength > PAXAI_LIMITS.payloadMaxBytes) application(AI_APPLICATION_ERRORS.CAPACITY);
  const payload = r.fixed(payloadLength);
  const unsigned = input.slice(0, r.offset);
  const authentication = r.u8();
  if (authentication === 1) r.fixed(96);
  else if (authentication !== 0) application(AI_APPLICATION_ERRORS.NON_CANONICAL);
  r.end();
  const envelope: RequestEnvelope = Object.freeze({ selector, chain, program, market, actor, epoch, config,
    roster: rosterBytes === ZERO32 ? null : rosterBytes, sequence, expiry, request, payload, delegated: authentication === 1 });
  validateEnvelope(envelope);
  return Object.freeze({ envelope, unsigned });
}

/** Request intent: H("PAXAI/request/v1", envelope bytes before authentication). */
export function requestIntent(unsigned: Uint8Array): Hex32 { return domainHash("PAXAI/request/v1", unsigned); }

function checkDomain(envelope: RequestEnvelope, binding: SnapshotBinding): void {
  if (envelope.chain !== binding.chain) application(AI_APPLICATION_ERRORS.WRONG_DOMAIN);
  if (envelope.program !== binding.program) application(AI_APPLICATION_ERRORS.WRONG_PROGRAM);
  if (envelope.market !== binding.market) application(AI_APPLICATION_ERRORS.WRONG_MARKET);
}

export interface FundRequest { readonly amount: bigint; readonly refundRecipient: Hex32; readonly policyVersion: bigint; readonly consent: boolean }
export interface ClaimRequest { readonly worker: Hex32; readonly recipient: Hex32; readonly amount: bigint }
export interface RefundRequest { readonly expectedRefunded: bigint; readonly amount: bigint; readonly recipient: Hex32 }

export function encodeFundRequest(value: FundRequest): Uint8Array {
  return new Writer().u128(value.amount).id(value.refundRecipient).u64(value.policyVersion).u8(value.consent ? 1 : 0).bytes();
}
export function encodeClaimRequest(value: ClaimRequest): Uint8Array {
  return new Writer().id(value.worker).id(value.recipient).u128(value.amount).bytes();
}
export function encodeRefundRequest(value: RefundRequest): Uint8Array {
  return new Writer().u128(value.expectedRefunded).u128(value.amount).id(value.recipient).bytes();
}

function payloadReader(bytes: Uint8Array, length: number): Reader {
  if (bytes.length !== length) application(AI_APPLICATION_ERRORS.NON_CANONICAL);
  return new Reader(bytes, () => application(AI_APPLICATION_ERRORS.NON_CANONICAL));
}
export function decodeFundRequest(bytes: Uint8Array): FundRequest {
  const r = payloadReader(bytes, 57);
  const value = Object.freeze({ amount: r.u128(), refundRecipient: r.id(), policyVersion: r.u64(), consent: r.boolean() });
  r.end();
  return value;
}
export function decodeClaimRequest(bytes: Uint8Array): ClaimRequest {
  const r = payloadReader(bytes, 80);
  const value = Object.freeze({ worker: r.id(), recipient: r.id(), amount: r.u128() });
  r.end();
  return value;
}
export function decodeRefundRequest(bytes: Uint8Array): RefundRequest {
  const r = payloadReader(bytes, 64);
  const value = Object.freeze({ expectedRefunded: r.u128(), amount: r.u128(), recipient: r.id() });
  r.end();
  return value;
}

// ---------------------------------------------------------------------------------------------
// Native Programs call activity (protocol 3, module 9 ordinal 3)

export interface ProgramCallActivity {
  readonly networkId: number;
  readonly actorDid: Uint8Array;
  readonly authority: Uint8Array;
  readonly accountSequence: bigint;
  readonly notBefore: bigint;
  readonly notAfter: bigint;
  readonly idempotencyKey: Uint8Array;
  readonly feeLimit: bigint;
  readonly payload: Uint8Array;
  readonly signature: Uint8Array | null;
}

/** Unsigned (eleven fields) or signed (twelve fields) canonical activity bytes. */
export function encodeProgramCallActivity(activity: ProgramCallActivity): Uint8Array {
  if (!Number.isInteger(activity.networkId) || activity.networkId < 0 || activity.networkId > 0xffff_ffff
    || activity.actorDid.length === 0 || activity.actorDid.length > 255 || activity.authority.length !== 32
    || activity.idempotencyKey.length !== 32 || activity.notAfter < activity.notBefore || activity.payload.length > 524_288
    || (activity.signature !== null && activity.signature.length !== 64)) invalid();
  const w = new Writer().u16(NATIVE_CALL_PROTOCOL_VERSION).u16(0x1001).u8(activity.signature === null ? 11 : 12)
    .u8(1).u16(NATIVE_CALL_PROTOCOL_VERSION).u8(2).u32(activity.networkId).u8(3).u32(ACTIVITY_TYPE)
    .u8(4).sized(activity.actorDid).u8(5).sized(activity.authority).u8(6).u64(activity.accountSequence)
    .u8(7).u64(activity.notBefore).u64(activity.notAfter).u8(8).sized(activity.idempotencyKey).u8(9).u128(activity.feeLimit)
    .u8(10).sized(sha256(concat(PAYLOAD_DOMAIN, activity.payload))).u8(11).sized(activity.payload);
  if (activity.signature !== null) w.u8(12).sized(activity.signature);
  return w.bytes();
}

export function decodeProgramCallActivity(bytes: Uint8Array): ProgramCallActivity {
  const r = new Reader(bytes, invalid);
  const tag = (expected: number): void => { if (r.u8() !== expected) invalid(); };
  if (r.u16() !== NATIVE_CALL_PROTOCOL_VERSION || r.u16() !== 0x1001) invalid();
  const count = r.u8();
  if (count !== 11 && count !== 12) invalid();
  tag(1); if (r.u16() !== NATIVE_CALL_PROTOCOL_VERSION) invalid();
  tag(2); const networkId = r.u32();
  tag(3); if (r.u32() !== ACTIVITY_TYPE) refuse("NotNativeProgramCall");
  tag(4); const actorDid = r.sized(255);
  tag(5); const authority = r.sized(32, 32);
  tag(6); const accountSequence = r.u64();
  tag(7); const notBefore = r.u64(); const notAfter = r.u64();
  tag(8); const idempotency = r.sized(32, 32);
  tag(9); const feeLimit = r.u128();
  tag(10); const payloadHash = r.sized(32, 32);
  tag(11); const payload = r.sized(524_288);
  let signature: Uint8Array | null = null;
  if (count === 12) { tag(12); signature = r.sized(64, 64); }
  r.end();
  if (actorDid.length === 0 || notAfter < notBefore || !equal(payloadHash, sha256(concat(PAYLOAD_DOMAIN, payload)))) invalid();
  return Object.freeze({ networkId, actorDid, authority, accountSequence, notBefore, notAfter, idempotencyKey: idempotency,
    feeLimit, payload, signature });
}

/** The 32-byte digest an owner key signs for these unsigned activity bytes. */
export function signingPreimage(unsigned: Uint8Array): Uint8Array { return sha256(concat(SIGNATURE_DOMAIN, unsigned)); }
export function activityIdOf(signed: Uint8Array): Hex32 { return hex(sha256(concat(ACTIVITY_DOMAIN, signed))); }

// ---------------------------------------------------------------------------------------------
// Prepare, review, approve and sign

export interface OperationRequest {
  readonly selector: number;
  readonly payload: Uint8Array;
  readonly actor: Hex32;
  /** "bound" carries the snapshot's frozen epoch and roster; "absent" is the pre-epoch registry form. */
  readonly roster: "bound" | "absent";
  readonly sequence: bigint;
  readonly expiry: bigint;
  readonly request: Hex32;
  readonly requiredRank: number;
  /** For CLAIM: a participant page of the same snapshot holding the worker's F06 entitlement row. */
  readonly entitlements?: ParticipantPage;
}

export interface NativeTerms {
  readonly networkId: number;
  readonly actorDid: string;
  readonly ownerPublicKey: Uint8Array;
  readonly accountSequence: bigint;
  readonly idempotencyKey: Hex32;
  readonly notBefore: bigint;
  readonly notAfter: bigint;
  readonly feeLimit: bigint;
  readonly capabilities: Uint8Array;
  readonly accessDeclaration: Uint8Array;
  readonly responseCapacity: number;
  readonly resources: NativeProgramCallV1["resources"];
}

/** Previewed semantic effect; advisory, re-checked by the program at execution. */
export type OperationEffect =
  | Readonly<{ kind: "fund"; amount: bigint; asset: Hex32 | null; refundRecipient: Hex32 }>
  | Readonly<{ kind: "claim"; worker: Hex32; recipient: Hex32; amount: bigint; asset: Hex32 | null }>
  | Readonly<{ kind: "refund-free"; expectedRefunded: bigint; amount: bigint; recipient: Hex32; asset: Hex32 | null }>
  | Readonly<{ kind: "other"; selector: number }>;

function effectOf(envelope: RequestEnvelope, claimAsset: Hex32 | null): OperationEffect {
  switch (envelope.selector) {
    case FUND: {
      const fund = decodeFundRequest(envelope.payload);
      return Object.freeze({ kind: "fund", amount: fund.amount, asset: null, refundRecipient: fund.refundRecipient });
    }
    case CLAIM: {
      const claim = decodeClaimRequest(envelope.payload);
      return Object.freeze({ kind: "claim", worker: claim.worker, recipient: claim.recipient, amount: claim.amount, asset: claimAsset });
    }
    case REFUND_FREE: {
      const refund = decodeRefundRequest(envelope.payload);
      return Object.freeze({ kind: "refund-free", expectedRefunded: refund.expectedRefunded, amount: refund.amount,
        recipient: refund.recipient, asset: null });
    }
    default:
      return Object.freeze({ kind: "other", selector: envelope.selector });
  }
}

/** The program's F06 refusal order for the checks the served views expose. */
function preview(envelope: RequestEnvelope, snapshotId: Hex32, entitlements: ParticipantPage | undefined): OperationEffect {
  if (envelope.selector === FUND) {
    const fund = decodeFundRequest(envelope.payload);
    if (fund.policyVersion !== FUNDING_POLICY_VERSION) application(AI_APPLICATION_ERRORS.F06_FUNDING_POLICY_MISMATCH);
    if (!fund.consent) application(AI_APPLICATION_ERRORS.F06_CONTRIBUTION_CONSENT_REQUIRED);
    if (fund.amount === 0n) application(AI_APPLICATION_ERRORS.F06_INVALID_AMOUNT);
  } else if (envelope.selector === CLAIM) {
    const claim = decodeClaimRequest(envelope.payload);
    if (entitlements !== undefined && entitlements.snapshotId !== snapshotId) refuse("SnapshotConflict");
    const row = entitlements?.rows.find((candidate) => candidate.kind === "worker" && candidate.id === claim.worker);
    if (row === undefined || row.reward.status !== "available") application(AI_APPLICATION_ERRORS.F06_UNKNOWN_WORKER_ENTITLEMENT);
    if (claim.amount === 0n) application(AI_APPLICATION_ERRORS.F06_NOTHING_TO_CLAIM);
    return effectOf(envelope, row.reward.asset);
  } else if (envelope.selector === REFUND_FREE) {
    if (decodeRefundRequest(envelope.payload).amount === 0n) application(AI_APPLICATION_ERRORS.F06_INVALID_AMOUNT);
  }
  return effectOf(envelope, null);
}

/**
 * Plans one native AI market mutation on a finalized snapshot: the exact envelope, its intent, the
 * previewed effect, the Programs call and the unsigned protocol 3 activity bytes.
 */
export function prepareOperation(view: Pick<MarketView, "binding" | "snapshotId">, authorityHeight: bigint,
  request: OperationRequest, terms: NativeTerms): PreparedOperation {
  const binding = view.binding;
  if (snapshotIdOf(binding) !== view.snapshotId) refuse("IntegrityFailure");
  requireCurrent(binding, authorityHeight);
  if (!Number.isInteger(request.requiredRank) || request.requiredRank < PAXAI_LIMITS.finalizedRank
    || request.requiredRank > PAXAI_LIMITS.settlementRank) application(AI_APPLICATION_ERRORS.NON_CANONICAL);
  if (request.requiredRank > binding.rank) refuse("FinalityUnavailable");
  if (operationOf(request.selector).boundary !== "mutation") refuse("NotMutation");
  let epoch = 0n;
  let roster: Hex32 | null = null;
  if (request.roster === "bound") {
    if (binding.epoch === null || binding.roster === null) application(AI_APPLICATION_ERRORS.WRONG_ROSTER);
    epoch = binding.epoch; roster = binding.roster;
  } else if (request.roster !== "absent") invalid();
  const envelope: RequestEnvelope = Object.freeze({ selector: request.selector, chain: binding.chain, program: binding.program,
    market: binding.market, actor: request.actor, epoch, config: binding.config, roster, sequence: request.sequence,
    expiry: request.expiry, request: request.request, payload: new Uint8Array(request.payload), delegated: false });
  const calldata = encodeRequestEnvelope(envelope);
  const intent = requestIntent(decodeRequestEnvelope(calldata).unsigned);
  const effect = preview(envelope, view.snapshotId, request.entitlements);
  let call: Uint8Array;
  try {
    call = encodeNativeProgramCallV1({ programId: idBytes(binding.program), guestAbi: GUEST_ABI as NativeProgramCallV1["guestAbi"],
      entrypoint: ENTRYPOINT, calldata, capabilities: terms.capabilities, accessDeclaration: terms.accessDeclaration,
      responseCapacity: terms.responseCapacity, resources: terms.resources });
  } catch (cause) { return refuse("NativeCall", { cause }); }
  if (!isId(terms.idempotencyKey) || terms.ownerPublicKey.length !== 32) invalid();
  const canonical = encodeProgramCallActivity({ networkId: terms.networkId, actorDid: new TextEncoder().encode(terms.actorDid),
    authority: new Uint8Array(terms.ownerPublicKey), accountSequence: terms.accountSequence, notBefore: terms.notBefore,
    notAfter: terms.notAfter, idempotencyKey: idBytes(terms.idempotencyKey), feeLimit: terms.feeLimit, payload: call, signature: null });
  return new PreparedOperation(TOKEN, { canonical, binding, snapshotId: view.snapshotId, intent, effect });
}

interface PlannedOperation {
  readonly canonical: Uint8Array;
  readonly binding: SnapshotBinding;
  readonly snapshotId: Hex32;
  readonly intent: Hex32;
  readonly effect: OperationEffect;
}

/** Planned operation: exact unsigned activity bytes plus the binding they were planned on. */
export class PreparedOperation {
  readonly #plan: PlannedOperation;
  public constructor(token: typeof TOKEN, plan: PlannedOperation) {
    if (token !== TOKEN) throw new TypeError("operations are prepared by prepareOperation");
    this.#plan = plan;
  }
  public get binding(): SnapshotBinding { return this.#plan.binding; }
  public get snapshotId(): Hex32 { return this.#plan.snapshotId; }
  public get intent(): Hex32 { return this.#plan.intent; }
  public get effect(): OperationEffect { return this.#plan.effect; }
  public get expectedRevision(): bigint { return this.#plan.binding.revision; }
  public canonicalBytes(): Uint8Array { return this.#plan.canonical.slice(); }
  /** Content identity of the canonical bytes; equals the native disclosure's preparation id. */
  public get preparationId(): Hex32 { return hex(sha256(this.#plan.canonical)); }
  public get signingPreimage(): Hex32 { return hex(signingPreimage(this.#plan.canonical)); }

  /**
   * Derives every approval term from the native disclosure of the exact canonical bytes, never from
   * the planning inputs.
   */
  public review(disclosure: NativePrepareResultV1): Review {
    let decoded: NativePrepareResultV1;
    try { decoded = decodeNativePrepareResult(disclosure); } catch (cause) { return refuse("InvalidEncoding", { cause }); }
    if (decoded.activity.module !== String(PROGRAMS_MODULE) || decoded.activity.ordinal !== String(PROGRAM_CALL_ORDINAL)) {
      refuse("NotNativeProgramCall");
    }
    const canonical = fromHex(decoded.canonical_bytes);
    if (decoded.preparation_id !== hex(sha256(canonical)) || decoded.signing_preimage !== hex(signingPreimage(canonical))
      || !equal(canonical, this.#plan.canonical)) refuse("IntegrityFailure");
    const activity = decodeProgramCallActivity(canonical);
    if (activity.signature !== null) refuse("IntegrityFailure");
    let call: NativeProgramCallV1;
    try { call = decodeNativeProgramCallV1(activity.payload); } catch (cause) { return refuse("NativeCall", { cause }); }
    const binding = this.#plan.binding;
    if (call.guestAbi !== GUEST_ABI || call.entrypoint !== ENTRYPOINT || hex(call.programId) !== binding.program) {
      refuse("NotNativeProgramCall");
    }
    const validated = decodeRequestEnvelope(call.calldata);
    const envelope = validated.envelope;
    checkDomain(envelope, binding);
    const claimAsset = this.#plan.effect.kind === "claim" ? this.#plan.effect.asset : null;
    if (requestIntent(validated.unsigned) !== this.#plan.intent || !sameEffect(effectOf(envelope, claimAsset), this.#plan.effect)) {
      refuse("IntegrityFailure");
    }
    const terms: ApprovalTerms = Object.freeze({
      action: envelope.selector, chain: envelope.chain, program: envelope.program, market: envelope.market, actor: envelope.actor,
      epoch: envelope.epoch, config: envelope.config, roster: envelope.roster, policy: binding.policy, snapshot: this.#plan.snapshotId,
      effect: this.#plan.effect, capabilities: hex(call.capabilities), accessDeclaration: hex(call.accessDeclaration),
      responseCapacity: call.responseCapacity, resources: Object.freeze([...call.resources]) as ApprovalTerms["resources"],
      feeLimit: activity.feeLimit, notBefore: activity.notBefore, notAfter: activity.notAfter, expiry: envelope.expiry,
      idempotencyKey: hex(activity.idempotencyKey), authority: hex(activity.authority), commitment: decoded.preparation_id,
    });
    return new Review(TOKEN, this.#plan, activity, terms);
  }
}

/** Every term the owner approves, derived from the disclosed canonical bytes. */
export interface ApprovalTerms {
  readonly action: number;
  readonly chain: Hex32;
  readonly program: Hex32;
  readonly market: Hex32;
  readonly actor: Hex32;
  readonly epoch: bigint;
  readonly config: bigint;
  readonly roster: Hex32 | null;
  readonly policy: Hex32;
  readonly snapshot: Hex32;
  readonly effect: OperationEffect;
  readonly capabilities: string;
  readonly accessDeclaration: string;
  readonly responseCapacity: number;
  readonly resources: NativeProgramCallV1["resources"];
  readonly feeLimit: bigint;
  readonly notBefore: bigint;
  readonly notAfter: bigint;
  readonly expiry: bigint;
  readonly idempotencyKey: Hex32;
  readonly authority: Hex32;
  readonly commitment: Hex32;
}

function sameEffect(left: OperationEffect, right: OperationEffect): boolean { return effectDifference(left, right) === null; }

function effectDifference(actual: OperationEffect, approved: OperationEffect): ReviewField | null {
  let checks: readonly (readonly [boolean, ReviewField])[];
  if (actual.kind === "fund" && approved.kind === "fund") {
    checks = [[actual.amount === approved.amount, "amount"], [actual.asset === approved.asset, "asset"],
      [actual.refundRecipient === approved.refundRecipient, "payee"]];
  } else if (actual.kind === "claim" && approved.kind === "claim") {
    checks = [[actual.amount === approved.amount, "amount"], [actual.asset === approved.asset, "asset"],
      [actual.recipient === approved.recipient, "payee"], [actual.worker === approved.worker, "worker"]];
  } else if (actual.kind === "refund-free" && approved.kind === "refund-free") {
    checks = [[actual.amount === approved.amount && actual.expectedRefunded === approved.expectedRefunded, "amount"],
      [actual.asset === approved.asset, "asset"], [actual.recipient === approved.recipient, "payee"]];
  } else if (actual.kind === "other" && approved.kind === "other") {
    checks = [[actual.selector === approved.selector, "action"]];
  } else checks = [[false, "action"]];
  return checks.find(([same]) => !same)?.[1] ?? null;
}

function firstDifference(actual: ApprovalTerms, approved: ApprovalTerms): ReviewField | null {
  const binding: readonly (readonly [boolean, ReviewField])[] = [
    [actual.action === approved.action, "action"], [actual.chain === approved.chain, "chain"],
    [actual.program === approved.program, "program"], [actual.market === approved.market, "market"],
    [actual.actor === approved.actor, "actor"], [actual.epoch === approved.epoch, "epoch"],
    [actual.config === approved.config, "config"], [actual.roster === approved.roster, "roster"],
    [actual.policy === approved.policy, "policy"], [actual.snapshot === approved.snapshot, "snapshot"],
  ];
  const native: readonly (readonly [boolean, ReviewField])[] = [
    [actual.capabilities === approved.capabilities, "capabilities"],
    [actual.accessDeclaration === approved.accessDeclaration, "access_declaration"],
    [actual.responseCapacity === approved.responseCapacity, "response_capacity"],
    [actual.resources.length === approved.resources.length && actual.resources.every((value, index) => value === approved.resources[index]), "resources"],
    [actual.feeLimit === approved.feeLimit, "fee_limit"],
    [actual.notBefore === approved.notBefore && actual.notAfter === approved.notAfter, "validity"],
    [actual.expiry === approved.expiry, "expiry"], [actual.idempotencyKey === approved.idempotencyKey, "idempotency_key"],
    [actual.authority === approved.authority, "authority"], [actual.commitment === approved.commitment, "commitment"],
  ];
  return binding.find(([same]) => !same)?.[1] ?? effectDifference(actual.effect, approved.effect)
    ?? native.find(([same]) => !same)?.[1] ?? null;
}

/** The disclosure-derived terms presented to the owner for one prepared operation. */
export class Review {
  public readonly terms: ApprovalTerms;
  readonly #plan: PlannedOperation;
  readonly #activity: ProgramCallActivity;
  public constructor(token: typeof TOKEN, plan: PlannedOperation, activity: ProgramCallActivity, terms: ApprovalTerms) {
    if (token !== TOKEN) throw new TypeError("reviews are derived by PreparedOperation.review");
    this.#plan = plan; this.#activity = activity; this.terms = terms;
  }
  /** Accepts the owner's approved terms only when every term equals the disclosed bytes and the key is the activity owner. */
  public approve(approved: ApprovalTerms, key: Uint8Array): Approval {
    const field = firstDifference(this.terms, approved);
    if (field !== null) refuse("ReviewMismatch", { field });
    if (!equal(this.#activity.authority, key)) refuse("UnauthorizedKey");
    return new Approval(TOKEN, this.#plan, this.#activity, new Uint8Array(key));
  }
}

/** Owner signer of the 32-byte signing preimage. */
export interface OperationSigner {
  readonly publicKey: Uint8Array;
  sign(preimage: Uint8Array): Uint8Array | Promise<Uint8Array>;
}

/** Owner approval of exactly the reviewed terms by the key the activity names. */
export class Approval {
  readonly #plan: PlannedOperation;
  readonly #activity: ProgramCallActivity;
  readonly #key: Uint8Array;
  public constructor(token: typeof TOKEN, plan: PlannedOperation, activity: ProgramCallActivity, key: Uint8Array) {
    if (token !== TOKEN) throw new TypeError("approvals are issued by Review.approve");
    this.#plan = plan; this.#activity = activity; this.#key = key;
  }
  /** Signs exactly the approved canonical bytes, verifies the signature and returns the durable Signed record. */
  public async sign(signer: OperationSigner, authorityHeight: bigint): Promise<OperationRecord> {
    requireCurrent(this.#plan.binding, authorityHeight);
    if (!equal(signer.publicKey, this.#key)) refuse("UnauthorizedKey");
    const preimage = signingPreimage(this.#plan.canonical);
    const signature = new Uint8Array(await signer.sign(preimage.slice()));
    if (signature.length !== 64 || !verifySignature(signature, preimage, this.#key)) refuse("Signature");
    const signed = encodeProgramCallActivity({ ...this.#activity, signature });
    return OperationRecord.signed(signed, this.#key, this.#plan.intent);
  }
}

function verifySignature(signature: Uint8Array, preimage: Uint8Array, key: Uint8Array): boolean {
  try { return ed25519.verify(signature, preimage, key); } catch { return false; }
}

// ---------------------------------------------------------------------------------------------
// Durable operation record and journal

interface RecordFields {
  readonly state: OperationState;
  readonly domain: DomainStatus;
  readonly protocolVersion: number;
  readonly networkId: number;
  readonly activityId: Hex32;
  readonly idempotencyKey: Hex32;
  readonly intent: Hex32;
  readonly signerPublicKey: Hex32;
  readonly notAfter: bigint;
  readonly attempt: number;
  readonly resultCode: number | null;
  readonly globalSequence: bigint | null;
  readonly checkpoint: Hex32 | null;
  readonly signedBytes: Uint8Array;
}

function signedIdentity(signed: Uint8Array, key: Uint8Array): Pick<RecordFields, "activityId" | "idempotencyKey" | "protocolVersion" | "networkId" | "notAfter"> {
  let activity: ProgramCallActivity;
  try { activity = decodeProgramCallActivity(signed); } catch { return refuse("CorruptRecord"); }
  if (activity.signature === null || !equal(encodeProgramCallActivity(activity), signed) || !equal(activity.authority, key)
    || !verifySignature(activity.signature, signingPreimage(encodeProgramCallActivity({ ...activity, signature: null })), key)) {
    refuse("CorruptRecord");
  }
  return { activityId: activityIdOf(signed), idempotencyKey: hex(activity.idempotencyKey),
    protocolVersion: NATIVE_CALL_PROTOCOL_VERSION, networkId: activity.networkId, notAfter: activity.notAfter };
}

/** SDK lifecycle of one signed native operation; distinct from the AI domain job status. */
export class OperationRecord implements RecordFields {
  public readonly state!: OperationState;
  public readonly domain!: DomainStatus;
  public readonly protocolVersion!: number;
  public readonly networkId!: number;
  public readonly activityId!: Hex32;
  public readonly idempotencyKey!: Hex32;
  public readonly intent!: Hex32;
  public readonly signerPublicKey!: Hex32;
  public readonly notAfter!: bigint;
  public readonly attempt!: number;
  public readonly resultCode!: number | null;
  public readonly globalSequence!: bigint | null;
  public readonly checkpoint!: Hex32 | null;
  readonly #signed: Uint8Array;

  private constructor(fields: RecordFields) {
    const { signedBytes, ...rest } = fields;
    Object.assign(this, rest);
    this.#signed = signedBytes.slice();
    Object.freeze(this);
  }

  public static signed(signed: Uint8Array, key: Uint8Array, intent: Hex32): OperationRecord {
    const identity = signedIdentity(signed, key);
    return new OperationRecord({ ...identity, state: "signed", domain: "pending", intent, signerPublicKey: hex(key), attempt: 0,
      resultCode: null, globalSequence: null, checkpoint: null, signedBytes: signed });
  }

  public get signedBytes(): Uint8Array { return this.#signed.slice(); }

  public with(changes: Partial<Omit<RecordFields, "signedBytes" | "activityId" | "idempotencyKey" | "intent" | "signerPublicKey">>, token: typeof TOKEN): OperationRecord {
    if (token !== TOKEN) throw new TypeError("record transitions are owned by OperationJournal");
    return new OperationRecord({ ...this.#fields(), ...changes });
  }

  public encode(): Uint8Array {
    const w = new Writer().put(RECORD_MAGIC).u8(STATE_CODES.indexOf(this.state) + 1).u8(this.domain === "pending" ? 1 : 2)
      .u16(this.protocolVersion).u32(this.networkId).id(this.activityId).id(this.idempotencyKey).id(this.intent)
      .id(this.signerPublicKey).u64(this.notAfter).u32(this.attempt);
    w.presence(this.resultCode, (code) => w.put(int32(code)));
    w.presence(this.globalSequence, (sequence) => w.u64(sequence));
    w.presence(this.checkpoint, (checkpoint) => w.id(checkpoint));
    return w.sized(this.#signed).bytes();
  }

  public static decode(bytes: Uint8Array): OperationRecord {
    const corrupt = (): never => refuse("CorruptRecord");
    const r = new Reader(bytes, corrupt);
    if (!equal(r.fixed(RECORD_MAGIC.length), RECORD_MAGIC)) corrupt();
    const state = STATE_CODES[r.u8() - 1] ?? corrupt();
    const domainCode = r.u8();
    const domain: DomainStatus = domainCode === 1 ? "pending" : domainCode === 2 ? "completed" : corrupt();
    const protocolVersion = r.u16(), networkId = r.u32(), activityId = r.id(), idempotencyKey = r.id(), intent = r.id();
    const signerPublicKey = r.id(), notAfter = r.u64(), attempt = r.u32();
    const optional = <T>(read: () => T): T | null => (r.boolean() ? read() : null);
    const resultCode = optional(() => new DataView(r.fixed(4).buffer).getInt32(0));
    const globalSequence = optional(() => r.u64());
    const checkpoint = optional(() => hex(r.fixed(32)));
    const signedBytes = r.fixed(r.u32());
    r.end();
    const identity = signedIdentity(signedBytes, idBytes(signerPublicKey));
    const record = new OperationRecord({ state, domain, protocolVersion, networkId, activityId, idempotencyKey, intent,
      signerPublicKey, notAfter, attempt, resultCode, globalSequence, checkpoint, signedBytes });
    if (identity.activityId !== activityId || identity.idempotencyKey !== idempotencyKey || identity.protocolVersion !== protocolVersion
      || identity.networkId !== networkId || identity.notAfter !== notAfter || !record.#consistent()) corrupt();
    return record;
  }

  #fields(): RecordFields {
    return { state: this.state, domain: this.domain, protocolVersion: this.protocolVersion, networkId: this.networkId,
      activityId: this.activityId, idempotencyKey: this.idempotencyKey, intent: this.intent, signerPublicKey: this.signerPublicKey,
      notAfter: this.notAfter, attempt: this.attempt, resultCode: this.resultCode, globalSequence: this.globalSequence,
      checkpoint: this.checkpoint, signedBytes: this.#signed };
  }

  #consistent(): boolean {
    const pristine = this.resultCode === null && this.globalSequence === null && this.checkpoint === null;
    const executed = this.resultCode === 0 && this.globalSequence !== null;
    switch (this.state) {
      case "prepared": case "reviewed": return false;
      case "signed": return pristine && this.attempt === 0;
      case "submitting": case "pending": case "unknown": return pristine && this.attempt > 0;
      case "executed": return executed && this.checkpoint === null && this.attempt > 0;
      case "finalized": return executed && this.checkpoint !== null && this.attempt > 0;
      case "failed": return this.checkpoint === null && (this.resultCode === null ? this.globalSequence === null : this.resultCode !== 0);
    }
  }
}

function int32(value: number): Uint8Array {
  const out = new Uint8Array(4);
  new DataView(out.buffer).setInt32(0, value);
  return out;
}

function requireState(record: OperationRecord, allowed: readonly OperationState[]): void {
  if (!allowed.includes(record.state)) refuse("InvalidTransition", { from: record.state });
}

function errorCode(error: unknown): string | undefined {
  return error !== null && typeof error === "object" && "code" in error && typeof error.code === "string" ? error.code : undefined;
}

const DEFINITIVE_REFUSALS: readonly string[] = ["invalid-argument", "idempotency-required", "protocol-incompatibility",
  "unavailable-capability", "core-rejection", "policy-refusal", "capability-refusal", "budget-refusal", "idempotency-conflict"];

/** Crash-safe journal of signed operations; one file per activity, replaced atomically. */
export class OperationJournal {
  readonly #directory: string;
  private constructor(directory: string) { this.#directory = directory; }

  public static async open(directory: string): Promise<OperationJournal> {
    try { await mkdir(directory, { recursive: true }); } catch (cause) { return refuse("Journal", { cause }); }
    return new OperationJournal(directory);
  }

  public recordPath(activityId: Hex32): string {
    if (!isId(activityId)) invalid();
    return join(this.#directory, `${activityId}.op`);
  }

  public async persist(record: OperationRecord): Promise<void> {
    const path = this.recordPath(record.activityId);
    const partial = `${path}.partial`;
    try {
      const file = await open(partial, "w");
      try { await file.writeFile(record.encode()); await file.sync(); } finally { await file.close(); }
      await rename(partial, path);
      const directory = await open(this.#directory, "r");
      try { await directory.sync(); } finally { await directory.close(); }
    } catch (cause) { refuse("Journal", { cause }); }
  }

  /** Loads a record; an interrupted `submitting` attempt is recovered as `unknown`. */
  public async load(activityId: Hex32): Promise<OperationRecord> {
    let bytes: Uint8Array;
    try { bytes = await readFile(this.recordPath(activityId)); } catch (cause) { return refuse("Journal", { cause }); }
    const record = OperationRecord.decode(bytes);
    if (record.activityId !== activityId) refuse("CorruptRecord");
    if (record.state !== "submitting") return record;
    const recovered = record.with({ state: "unknown" }, TOKEN);
    await this.persist(recovered);
    return recovered;
  }

  /** Durably records a fresh signature; never over an activity that already left `signed`. */
  public async recordSigned(record: OperationRecord): Promise<void> {
    requireState(record, ["signed"]);
    let existing: Uint8Array | null = null;
    try { existing = await readFile(this.recordPath(record.activityId)); } catch (cause) {
      if (errorCode(cause) !== "ENOENT") refuse("Journal", { cause });
    }
    if (existing !== null) requireState(OperationRecord.decode(existing), ["signed"]);
    await this.persist(record);
  }

  public async submit(record: OperationRecord, operations: ProgramOperations): Promise<OperationRecord> {
    requireState(record, ["signed"]);
    return await this.#deliver(record, operations);
  }

  /** Re-sends the exact retained signed bytes of an operation whose outcome is unknown. */
  public async resendExact(record: OperationRecord, operations: ProgramOperations): Promise<OperationRecord> {
    requireState(record, ["unknown"]);
    return await this.#deliver(record, operations);
  }

  /** Applies a verified receipt; an absent or still-unknown outcome leaves the record unchanged. */
  public async resolve(record: OperationRecord, submission: ProgramSubmission | null): Promise<OperationRecord> {
    requireState(record, ["pending", "unknown"]);
    if (submission === null || submission.state === "unknown") return record;
    const resolved = applyReceipt(record, submission);
    await this.persist(resolved);
    return resolved;
  }

  /** Looks the activity up by its idempotency key; a retryable lookup failure leaves the record unchanged. */
  public async resolveThrough(record: OperationRecord, operations: ProgramOperations): Promise<OperationRecord> {
    requireState(record, ["pending", "unknown"]);
    let submission: ProgramSubmission;
    try { submission = await operations.receipt(record.idempotencyKey, record.activityId); } catch (error) {
      if (error instanceof PlatformSdkError && error.retry !== "never") return record;
      throw error;
    }
    return await this.resolve(record, submission);
  }

  public async finalize(record: OperationRecord, finality: CheckpointVerification): Promise<OperationRecord> {
    requireState(record, ["executed"]);
    const sequence = record.globalSequence;
    if (finality.header.networkId !== record.networkId || finality.checkpointId.length !== 32
      || finality.checkpointId.every((byte) => byte === 0) || sequence === null
      || sequence < finality.header.firstSequence || sequence > finality.header.lastSequence) refuse("FinalityMismatch");
    const finalized = record.with({ state: "finalized", checkpoint: hex(finality.checkpointId) }, TOKEN);
    await this.persist(finalized);
    return finalized;
  }

  /** A signed operation never sent becomes `failed` once its validity window has passed. */
  public async expire(record: OperationRecord, now: bigint): Promise<OperationRecord> {
    requireState(record, ["signed"]);
    if (now <= record.notAfter) return record;
    const failed = record.with({ state: "failed" }, TOKEN);
    await this.persist(failed);
    return failed;
  }

  async #deliver(record: OperationRecord, operations: ProgramOperations): Promise<OperationRecord> {
    if (record.attempt >= 0xffff_ffff) refuse("CorruptRecord");
    const submitting = record.with({ state: "submitting", attempt: record.attempt + 1 }, TOKEN);
    await this.persist(submitting);
    let submission: ProgramSubmission;
    try {
      submission = await operations.submit(programRequest(record.signedBytes), idempotencyKey(record.idempotencyKey));
    } catch (error) {
      if (!(error instanceof PlatformSdkError) || !DEFINITIVE_REFUSALS.includes(error.code)) {
        await this.persist(record);
        if (error instanceof AiMarketError) throw error;
        return refuse("NativeCall", { cause: error });
      }
      const terminal = submitting.attempt === 1 && error.retry === "never" && error.code !== "idempotency-conflict";
      const code = error.protocolResultCode;
      const next = submitting.with(terminal
        ? { state: "failed", resultCode: code === undefined || code === 0 ? null : code }
        : { state: "unknown" }, TOKEN);
      await this.persist(next);
      return next;
    }
    if (submission.state === "unknown") {
      const unknown = submitting.with({ state: "unknown" }, TOKEN);
      await this.persist(unknown);
      return unknown;
    }
    let resolved: OperationRecord;
    try { resolved = applyReceipt(submitting, submission); } catch (error) {
      await this.persist(submitting.with({ state: "unknown" }, TOKEN));
      throw error;
    }
    await this.persist(resolved);
    return resolved;
  }
}

function programRequest(signed: Uint8Array): NativeProgramRequestV1 {
  const activity = decodeProgramCallActivity(signed);
  try { return new NativeProgramRequestV1(decodeNativeProgramCallV1(activity.payload), activity.feeLimit, signed); } catch (cause) {
    return refuse("NativeCall", { cause });
  }
}

function applyReceipt(record: OperationRecord, execution: Exclude<ProgramSubmission, Readonly<{ state: "unknown" }>>): OperationRecord {
  if (execution.activity_id !== record.activityId || !Number.isInteger(execution.result_code)
    || execution.result_code < -0x8000_0000 || execution.result_code > 0x7fff_ffff) refuse("ReceiptMismatch");
  let globalSequence: bigint;
  try { globalSequence = parseDecimalU64(execution.global_sequence); } catch { return refuse("ReceiptMismatch"); }
  const code = execution.result_code;
  if (execution.state === "refused" && code === 0) refuse("ReceiptMismatch");
  return record.with({ state: code === 0 ? "executed" : "failed", resultCode: code, globalSequence }, TOKEN);
}

// ---------------------------------------------------------------------------------------------
// Bytes

function ascii(value: string): Uint8Array { return new TextEncoder().encode(value); }
function concat(...parts: readonly Uint8Array[]): Uint8Array {
  const out = new Uint8Array(parts.reduce((total, part) => total + part.length, 0));
  let offset = 0;
  for (const part of parts) { out.set(part, offset); offset += part.length; }
  return out;
}
function hex(bytes: Uint8Array): string { return Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join(""); }
function fromHex(value: string): Uint8Array {
  if (value.length % 2 !== 0 || !/^[0-9a-f]*$/u.test(value)) invalid();
  return Uint8Array.from(value.match(/../gu) ?? [], (pair) => Number.parseInt(pair, 16));
}
function isId(value: unknown): value is Hex32 { return typeof value === "string" && HEX32.test(value) && value !== ZERO32; }
function idBytes(value: Hex32): Uint8Array { return isId(value) ? fromHex(value) : invalid(); }
function equal(left: Uint8Array, right: Uint8Array): boolean {
  return left.length === right.length && left.every((value, index) => value === right[index]);
}
