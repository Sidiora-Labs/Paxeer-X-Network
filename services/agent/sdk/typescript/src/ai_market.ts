import * as http from "node:http";
import * as https from "node:https";
import { mkdir, open, readFile, rename } from "node:fs/promises";
import { join } from "node:path";
import { ed25519 } from "@noble/curves/ed25519.js";
import { secp256k1 } from "@noble/curves/secp256k1.js";
import { sha256 } from "@noble/hashes/sha2.js";
import { keccak_256 } from "@noble/hashes/sha3.js";

import type { LayerXKeyCredential } from "./agent-http.js";
import {
  decodeNativePrepareResult,
  type NativePrepareResultV1,
} from "./generated/client.js";
import {
  decodeNativeProgramCallV1,
  encodeNativeProgramCallV1,
  type NativeProgramCallV1,
} from "./native-program-call.js";
import { PlatformSdkError } from "./production.js";
import {
  readLayerXBindState,
  requireLayerXBinding,
  type BindFees,
  type Eip1193Requester,
} from "./account-derivation.js";
import { abiSelector, type PrecompileCall } from "./exchange.js";
import { decodeRevertReason, RevertReasonError } from "./revert.js";
import type {
  ProtocolReceipt,
  ReceiptEffect,
  SequencerAuthorization,
} from "./verifier.js";

/** Values of sdk/platform/schema/paxai-v1.kvx [limits]. */
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

export const NATIVE_CALL_PROTOCOL_VERSION = 2;
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
export const PROGRAMS_PRECOMPILE = "0x000000000000000000000000000000000000101d";
const PROGRAMS_CALL_SELECTOR = fromHex(
  abiSelector(
    "call(bytes32,uint16,string,bytes,bytes,bytes,uint32,uint64[7],uint128)",
  ).slice(2),
);
const MAX_CALL_BYTES = 2_163_428;
const RECORD_MAGIC = ascii("PAXAIOP1");
const ENVELOPE_MAGIC = ascii("PAXAI1");
const TOKEN = Symbol("paxai");

export type Hex32 = string;

export type ReviewField =
  | "action"
  | "chain"
  | "program"
  | "market"
  | "actor"
  | "epoch"
  | "config"
  | "roster"
  | "policy"
  | "snapshot"
  | "amount"
  | "asset"
  | "payee"
  | "worker"
  | "capabilities"
  | "access_declaration"
  | "response_capacity"
  | "resources"
  | "fee_limit"
  | "validity"
  | "expiry"
  | "idempotency_key"
  | "authority"
  | "commitment"
  | "gas"
  | "nonce";

export type OperationState =
  | "prepared"
  | "reviewed"
  | "signed"
  | "submitting"
  | "pending"
  | "executed"
  | "finalized"
  | "failed"
  | "unknown";
const STATE_CODES: readonly OperationState[] = [
  "prepared",
  "reviewed",
  "signed",
  "submitting",
  "pending",
  "executed",
  "finalized",
  "failed",
  "unknown",
];
export type DomainStatus = "pending" | "completed";

export type AiMarketErrorCode =
  | "BindingMismatch"
  | "FinalityUnavailable"
  | "SnapshotConflict"
  | "IntegrityFailure"
  | "CursorExpired"
  | "CursorMismatch"
  | "ResponseTooLarge"
  | "StaleAuthority"
  | "NotNativeProgramCall"
  | "NotMutation"
  | "ReviewMismatch"
  | "UnauthorizedKey"
  | "InvalidTransition"
  | "ReceiptMismatch"
  | "FinalityMismatch"
  | "HistoryUnavailable"
  | "CorruptRecord"
  | "InvalidEncoding"
  | "Application"
  | "NativeCall"
  | "Signature"
  | "Service"
  | "Journal";

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
  public constructor(
    code: AiMarketErrorCode,
    detail: AiMarketErrorDetail = {},
  ) {
    const suffix =
      detail.field ??
      detail.category ??
      detail.from ??
      detail.epochStatus ??
      (detail.applicationCode === undefined
        ? undefined
        : `0x${detail.applicationCode.toString(16).padStart(4, "0")}`);
    super(suffix === undefined ? code : `${code}: ${suffix}`);
    this.name = "AiMarketError";
    this.code = code;
    this.detail = Object.freeze({ ...detail });
  }
}

function refuse(code: AiMarketErrorCode, detail?: AiMarketErrorDetail): never {
  throw new AiMarketError(code, detail);
}
function application(code: number): never {
  throw new AiMarketError("Application", { applicationCode: code });
}
function invalid(): never {
  return refuse("InvalidEncoding");
}

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

type OperationRow = readonly [
  string,
  number,
  number,
  CallBoundary,
  OperationSpec["sequence"],
  OperationSpec["delegation"],
  number,
  number,
];
const OPERATION_ROWS: readonly OperationRow[] = [
  ["CREATE", 0x0101, 1, "mutation", "role", "native-only", 0, 15965],
  ["STAGE_POLICY", 0x0102, 1, "mutation", "role", "native-only", 0, 15965],
  ["CANCEL_POLICY", 0x0103, 1, "mutation", "role", "native-only", 16, 16],
  ["SCHEDULE_ACTIVATION", 0x0104, 1, "mutation", "role", "native-only", 16, 16],
  [
    "ADVANCE_ACTIVATION",
    0x0105,
    1,
    "mutation",
    "object-local",
    "native-only",
    16,
    16,
  ],
  ["SUSPEND", 0x0106, 1, "mutation", "role", "native-only", 40, 48],
  ["UPDATE_METADATA", 0x0107, 1, "mutation", "role", "native-only", 40, 48],
  ["APPOINT_OPERATOR", 0x0108, 1, "mutation", "role", "native-only", 49, 49],
  ["REVOKE_OPERATOR", 0x0109, 1, "mutation", "role", "native-only", 16, 16],
  ["REQUEST_CLOSE", 0x010a, 1, "mutation", "role", "native-only", 40, 40],
  [
    "ADVANCE_CLOSE",
    0x010b,
    1,
    "mutation",
    "object-local",
    "native-only",
    11,
    11,
  ],
  [
    "ADMIT_TASK",
    0x010c,
    1,
    "mutation",
    "object-local",
    "native-only",
    248,
    248,
  ],
  ["ACCEPT_TASK", 0x010d, 1, "mutation", "role", "delegate", 72, 72],
  ["CANCEL_TASK", 0x010e, 1, "mutation", "object-local", "native-only", 32, 32],
  ["COMMIT_TASK_RESULT", 0x010f, 1, "mutation", "role", "delegate", 72, 72],
  [
    "SEAL_TASK_SET",
    0x0110,
    1,
    "mutation",
    "object-local",
    "native-only",
    48,
    48,
  ],
  [
    "OPEN_EPOCH",
    0x0111,
    1,
    "mutation",
    "object-local",
    "native-only",
    0,
    15965,
  ],
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
  [
    "ChallengeAssessment",
    0x0304,
    3,
    "mutation",
    "object-local",
    "native-only",
    97,
    97,
  ],
  ["CommitScore", 0x0401, 4, "mutation", "role", "delegate", 224, 224],
  ["RevealScore", 0x0402, 4, "mutation", "role", "delegate", 364, 1480],
  [
    "BeginAggregation",
    0x0501,
    5,
    "mutation",
    "object-local",
    "native-only",
    0,
    0,
  ],
  [
    "ProcessAggregation",
    0x0502,
    5,
    "mutation",
    "object-local",
    "native-only",
    34,
    34,
  ],
  [
    "FinalizeAggregation",
    0x0503,
    5,
    "mutation",
    "object-local",
    "native-only",
    32,
    32,
  ],
  ["FUND", 0x0601, 6, "mutation", "role", "native-only", 57, 57],
  ["CLAIM", 0x0602, 6, "mutation", "object-local", "native-only", 80, 80],
  [
    "EXPIRE_EPOCH_CLAIMS",
    0x0603,
    6,
    "mutation",
    "object-local",
    "native-only",
    0,
    0,
  ],
  ["REFUND_FREE", 0x0604, 6, "mutation", "object-local", "native-only", 64, 64],
  ["PRUNE_EPOCH", 0x0605, 6, "mutation", "object-local", "native-only", 0, 0],
  ["ResetHistory", 0x0701, 7, "mutation", "role", "native-only", 73, 73],
  ["SuspendHistory", 0x0702, 7, "mutation", "role", "native-only", 73, 73],
  ["ResumeHistory", 0x0703, 7, "mutation", "role", "native-only", 73, 73],
  ["ApproveAdmission", 0x0801, 8, "mutation", "role", "native-only", 0, 15965],
  [
    "RevokeAdmissionApproval",
    0x0802,
    8,
    "mutation",
    "role",
    "native-only",
    0,
    15965,
  ],
  ["AdmitWorker", 0x0803, 8, "mutation", "role", "native-only", 0, 15965],
  ["AdmitEvaluator", 0x0804, 8, "mutation", "role", "native-only", 0, 15965],
  ["Heartbeat", 0x0805, 8, "mutation", "role", "delegate", 0, 15965],
  ["RequestExit", 0x0806, 8, "mutation", "role", "native-only", 0, 15965],
  ["CancelExit", 0x0807, 8, "mutation", "role", "native-only", 0, 15965],
  [
    "PruneInactive",
    0x0808,
    8,
    "mutation",
    "object-local",
    "native-only",
    0,
    15965,
  ],
  [
    "AdministrativeRemove",
    0x0809,
    8,
    "mutation",
    "role",
    "native-only",
    0,
    15965,
  ],
  ["SealEvidence", 0x0901, 9, "mutation", "role", "delegate", 131, 131],
  [
    "READ_HEADER",
    0x0a01,
    10,
    "program-read",
    "object-local",
    "native-only",
    0,
    0,
  ],
  [
    "READ_STATE_CHUNK",
    0x0a02,
    10,
    "program-read",
    "object-local",
    "native-only",
    46,
    46,
  ],
];

/** The [operations] table of the shared schema, keyed by selector. */
export const PAXAI_OPERATIONS: ReadonlyMap<number, OperationSpec> = new Map(
  OPERATION_ROWS.map(
    ([
      name,
      selector,
      feature,
      boundary,
      sequence,
      delegation,
      payloadMin,
      payloadMax,
    ]) => [
      selector,
      Object.freeze({
        name,
        selector,
        feature,
        boundary,
        sequence,
        delegation,
        payloadMin,
        payloadMax,
      }),
    ],
  ),
);

export const FUND = 0x0601;
export const CLAIM = 0x0602;
export const REFUND_FREE = 0x0604;

function registryBeforeEpoch(selector: number): boolean {
  return (
    (selector >= 0x0101 && selector <= 0x010b) ||
    selector === 0x0111 ||
    (selector >= 0x0201 && selector <= 0x0207) ||
    selector === 0x0209 ||
    selector === 0x020a ||
    (selector >= 0x0301 && selector <= 0x0303) ||
    selector === 0x0601 ||
    selector === 0x0604 ||
    (selector >= 0x0801 && selector <= 0x0804) ||
    selector === 0x0a01 ||
    selector === 0x0a02
  );
}

/** Exact H(D, B) = SHA-256(ASCII D || 0x00 || B); an all-zero digest refuses. */
export function domainHash(domain: string, bytes: Uint8Array): Hex32 {
  if (domain.length === 0 || !/^[\x01-\x7f]+$/u.test(domain))
    application(AI_APPLICATION_ERRORS.NON_CANONICAL);
  const digest = hex(sha256(concat(ascii(domain), new Uint8Array([0]), bytes)));
  if (digest === ZERO32) application(AI_APPLICATION_ERRORS.NON_CANONICAL);
  return digest;
}

/** Canonical u64 decimal: no sign, no leading zero, at most 20 digits. */
export function parseDecimalU64(text: string): bigint {
  return decimal(text, U64_MAX);
}
/** Canonical u128 decimal: no sign, no leading zero, at most 39 digits. */
export function parseDecimalU128(text: string): bigint {
  return decimal(text, U128_MAX);
}
function decimal(text: unknown, maximum: bigint): bigint {
  if (typeof text !== "string" || text.length > 39 || !DECIMAL.test(text))
    invalid();
  const value = BigInt(text);
  return value > maximum ? invalid() : value;
}

class Writer {
  readonly #parts: Uint8Array[] = [];
  public put(bytes: Uint8Array): this {
    this.#parts.push(bytes.slice());
    return this;
  }
  public u8(value: number): this {
    return this.unsigned(BigInt(value), 1);
  }
  public u16(value: number): this {
    return this.unsigned(BigInt(value), 2);
  }
  public u32(value: number): this {
    return this.unsigned(BigInt(value), 4);
  }
  public u64(value: bigint): this {
    return this.unsigned(value, 8);
  }
  public u128(value: bigint): this {
    return this.unsigned(value, 16);
  }
  public id(value: Hex32): this {
    return this.put(idBytes(value));
  }
  public sized(bytes: Uint8Array): this {
    return this.u32(bytes.length).put(bytes);
  }
  public presence<T>(value: T | null, write: (value: T) => void): this {
    if (value === null) return this.u8(0);
    this.u8(1);
    write(value);
    return this;
  }
  public bytes(): Uint8Array {
    return concat(...this.#parts);
  }
  private unsigned(value: bigint, length: number): this {
    if (
      typeof value !== "bigint" ||
      value < 0n ||
      value >= 1n << BigInt(length * 8)
    )
      invalid();
    const out = new Uint8Array(length);
    let rest = value;
    for (let index = length - 1; index >= 0; index -= 1) {
      out[index] = Number(rest & 0xffn);
      rest >>= 8n;
    }
    return this.put(out);
  }
}

class Reader {
  #offset = 0;
  public constructor(
    private readonly input: Uint8Array,
    private readonly fail: () => never,
  ) {}
  public get offset(): number {
    return this.#offset;
  }
  public fixed(length: number): Uint8Array {
    const end = this.#offset + length;
    if (end > this.input.length) this.fail();
    const out = new Uint8Array(this.input.subarray(this.#offset, end));
    this.#offset = end;
    return out;
  }
  public u8(): number {
    return this.fixed(1)[0] ?? this.fail();
  }
  public u16(): number {
    return Number(this.unsigned(2));
  }
  public u32(): number {
    return Number(this.unsigned(4));
  }
  public u64(): bigint {
    return this.unsigned(8);
  }
  public u128(): bigint {
    return this.unsigned(16);
  }
  public id(): Hex32 {
    const value = hex(this.fixed(32));
    return value === ZERO32 ? this.fail() : value;
  }
  public boolean(): boolean {
    const value = this.u8();
    return value === 0 ? false : value === 1 ? true : this.fail();
  }
  public sized(maximum: number, exact?: number): Uint8Array {
    const length = this.u32();
    if (length > maximum || (exact !== undefined && length !== exact))
      this.fail();
    return this.fixed(length);
  }
  public end(): void {
    if (this.#offset !== this.input.length) this.fail();
  }
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
  if (
    binding.revision === 0n ||
    binding.config === 0n ||
    !Number.isInteger(binding.rank) ||
    binding.rank < 0 ||
    binding.rank > PAXAI_LIMITS.finalizedRank
  )
    application(AI_APPLICATION_ERRORS.NON_CANONICAL);
  const w = new Writer()
    .u16(1)
    .id(binding.chain)
    .id(binding.program)
    .id(binding.market)
    .u64(binding.observedSequence)
    .u64(binding.executionHeight)
    .id(binding.batchId)
    .id(binding.nativeStateRoot)
    .u64(binding.revision)
    .id(binding.stateDigest);
  w.presence(binding.epoch, (epoch) => w.u64(epoch));
  w.u64(binding.config).id(binding.policy);
  return w.presence(binding.roster, (roster) => w.id(roster));
}

/** Canonical binding bytes (queries.rs SnapshotBinding::encode). */
export function encodeSnapshotBinding(binding: SnapshotBinding): Uint8Array {
  const w = bindingPrefix(binding).id(binding.checkpoint);
  w.presence(binding.settlement, (settlement) => w.id(settlement));
  return w
    .u8(binding.rank)
    .u64(binding.publicationTimeMs)
    .put(new Uint8Array(8))
    .bytes();
}

/** Content identity over the binding prefix; excludes finality evidence, rank and publication time. */
export function snapshotIdOf(binding: SnapshotBinding): Hex32 {
  return domainHash("PAXAI/view/v1", bindingPrefix(binding).bytes());
}

export type Freshness =
  | Readonly<{ label: "current" }>
  | Readonly<{ label: "stale"; lag: bigint }>
  | Readonly<{ label: "unknown" }>;

/** Freshness of a binding against an independently verified authority height. */
export function freshnessOf(
  binding: SnapshotBinding,
  authorityHeight: bigint | null,
): Freshness {
  if (authorityHeight === null) return Object.freeze({ label: "unknown" });
  const lag =
    authorityHeight > binding.executionHeight
      ? authorityHeight - binding.executionHeight
      : 0n;
  return Object.freeze(
    lag > PAXAI_LIMITS.authorityFreshnessHeights
      ? { label: "stale", lag }
      : { label: "current" },
  );
}

/** Routing and signing fail closed on an authority more than eight heights ahead. */
export function requireCurrent(
  binding: SnapshotBinding,
  authorityHeight: bigint,
): void {
  const freshness = freshnessOf(binding, authorityHeight);
  if (freshness.label === "stale")
    refuse("StaleAuthority", { lag: freshness.lag });
}

export type ProjectionState =
  | "observed-unverified"
  | "evidence-verified-unfinalized"
  | "finalized-publishable"
  | "archived"
  | "quarantined";
export type Availability =
  | "available"
  | "not-enabled"
  | "not-yet-produced"
  | "content-unavailable"
  | "unsupported-version";
export type ScoreStatus =
  | "present"
  | "no-admissible-score"
  | "insufficient-coverage"
  | "not-produced"
  | "unavailable"
  | "unsupported";
export type EpochStatus =
  | "retained"
  | "never-opened"
  | "retained-terminal"
  | "archive-required"
  | "archive-unavailable"
  | "unsupported-version";
export type ParticipantKind = "worker" | "evaluator";
export type KindFilter = "all" | ParticipantKind;
export type FeatureName =
  | "F01"
  | "F02"
  | "F03"
  | "F04"
  | "F05"
  | "F06"
  | "F07"
  | "F08"
  | "F09"
  | "F10";
export type Components = Readonly<Record<FeatureName, Availability>>;

const PROJECTIONS: readonly ProjectionState[] = [
  "observed-unverified",
  "evidence-verified-unfinalized",
  "finalized-publishable",
  "archived",
  "quarantined",
];
const AVAILABILITY: readonly Availability[] = [
  "available",
  "not-enabled",
  "not-yet-produced",
  "content-unavailable",
  "unsupported-version",
];
const SCORE_STATUS: readonly ScoreStatus[] = [
  "present",
  "no-admissible-score",
  "insufficient-coverage",
  "not-produced",
  "unavailable",
  "unsupported",
];
const EPOCH_STATUS: readonly EpochStatus[] = [
  "retained",
  "never-opened",
  "retained-terminal",
  "archive-required",
  "archive-unavailable",
  "unsupported-version",
];
const FEATURES: readonly FeatureName[] = [
  "F01",
  "F02",
  "F03",
  "F04",
  "F05",
  "F06",
  "F07",
  "F08",
  "F09",
  "F10",
];
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
  readonly score: Readonly<{
    status: ScoreStatus;
    epoch: bigint | null;
    ppm: number | null;
  }>;
  readonly reward: Readonly<{
    status: Availability;
    asset: Hex32 | null;
    earned: bigint | null;
    claimed: bigint | null;
  }>;
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

export interface EpochEntry {
  readonly epoch: bigint;
  readonly status: EpochStatus;
  readonly snapshotId: Hex32 | null;
}
export interface EpochPage {
  readonly component: Availability;
  readonly entries: readonly EpochEntry[];
}

interface NormalizedQuery {
  readonly market: Hex32;
  readonly snapshot: Hex32 | null;
  readonly kind: KindFilter;
  readonly activeOnly: boolean;
  readonly limit: number;
}

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
  public constructor(
    token: typeof TOKEN,
    value: Readonly<{
      token: string;
      query: NormalizedQuery;
      snapshotId: Hex32;
      binding: SnapshotBinding;
      after: ParticipantRow;
    }>,
  ) {
    if (token !== TOKEN)
      throw new TypeError("participant cursors are issued by AiMarketViews");
    this.token = value.token;
    this.market = value.query.market;
    this.snapshotId = value.snapshotId;
    this.kind = value.query.kind;
    this.activeOnly = value.query.activeOnly;
    this.binding = value.binding;
    this.#after = rowKey(value.after);
    Object.freeze(this);
  }
  /** Ordering key of the last row already returned. */
  public after(): string {
    return this.#after;
  }
}

function normalizeQuery(query: ParticipantQuery): NormalizedQuery {
  const kind = query.kind ?? "all";
  const limit = query.limit ?? PAXAI_LIMITS.defaultPageRows;
  if (
    !isId(query.market) ||
    (query.snapshot !== undefined && !isId(query.snapshot)) ||
    (kind !== "all" && kind !== "worker" && kind !== "evaluator") ||
    (query.activeOnly !== undefined && typeof query.activeOnly !== "boolean") ||
    !Number.isInteger(limit) ||
    limit < 1 ||
    limit > PAXAI_LIMITS.maxPageRows
  )
    invalid();
  return {
    market: query.market,
    snapshot: query.snapshot ?? null,
    kind,
    activeOnly: query.activeOnly ?? false,
    limit,
  };
}

function checkCursorToken(token: unknown): string {
  if (typeof token !== "string") return invalid();
  if (token.length > PAXAI_LIMITS.cursorMaxBytes) refuse("ResponseTooLarge");
  return CURSOR.test(token) ? token : invalid();
}

function exact(
  value: unknown,
  keys: readonly string[],
  optional: readonly string[] = [],
): Readonly<Record<string, unknown>> {
  if (value === null || typeof value !== "object" || Array.isArray(value))
    invalid();
  const item = value as Record<string, unknown>;
  if (
    keys.some((key) => !Object.prototype.hasOwnProperty.call(item, key)) ||
    Object.keys(item).some(
      (key) => !keys.includes(key) && !optional.includes(key),
    )
  )
    invalid();
  return item;
}
function jsonId(value: unknown): Hex32 {
  if (
    typeof value !== "string" ||
    !JSON_ID.test(value) ||
    value.slice(2) === ZERO32
  )
    invalid();
  return value.slice(2);
}
function jsonOptional<T>(
  value: unknown,
  decode: (value: unknown) => T,
): T | null {
  return value === null ? null : decode(value);
}
function jsonU64(value: unknown): bigint {
  return decimal(value, U64_MAX);
}
function jsonU128(value: unknown): bigint {
  return decimal(value, U128_MAX);
}
function jsonEnum<T extends string>(value: unknown, table: readonly T[]): T {
  return typeof value === "string" &&
    (table as readonly string[]).includes(value)
    ? (value as T)
    : invalid();
}
function jsonInteger(value: unknown, maximum: number): number {
  return typeof value === "number" &&
    Number.isSafeInteger(value) &&
    value >= 0 &&
    value <= maximum
    ? value
    : invalid();
}

function decodeBinding(value: unknown): SnapshotBinding {
  const b = exact(value, [
    "chain",
    "program",
    "market",
    "observed_sequence",
    "execution_height",
    "batch_id",
    "native_state_root",
    "revision",
    "state_digest",
    "epoch",
    "config",
    "policy",
    "roster",
    "checkpoint",
    "settlement",
    "rank",
    "publication_time_ms",
  ]);
  const binding: SnapshotBinding = Object.freeze({
    chain: jsonId(b.chain),
    program: jsonId(b.program),
    market: jsonId(b.market),
    observedSequence: jsonU64(b.observed_sequence),
    executionHeight: jsonU64(b.execution_height),
    batchId: jsonId(b.batch_id),
    nativeStateRoot: jsonId(b.native_state_root),
    revision: jsonU64(b.revision),
    stateDigest: jsonId(b.state_digest),
    epoch: jsonOptional(b.epoch, jsonU64),
    config: jsonU64(b.config),
    policy: jsonId(b.policy),
    roster: jsonOptional(b.roster, jsonId),
    checkpoint: jsonId(b.checkpoint),
    settlement: jsonOptional(b.settlement, jsonId),
    rank: jsonInteger(b.rank, PAXAI_LIMITS.finalizedRank),
    publicationTimeMs: jsonU64(b.publication_time_ms),
  });
  if (binding.revision === 0n || binding.config === 0n) invalid();
  return binding;
}

function bindSnapshot(
  binding: SnapshotBinding,
  servedId: unknown,
  market: Hex32,
): Hex32 {
  const snapshotId = jsonId(servedId);
  if (binding.market !== market) refuse("BindingMismatch");
  if (snapshotIdOf(binding) !== snapshotId) refuse("IntegrityFailure");
  if (binding.rank < PAXAI_LIMITS.finalizedRank) refuse("FinalityUnavailable");
  return snapshotId;
}

function decodeComponents(value: unknown): Components {
  const item = exact(value, FEATURES);
  return Object.freeze(
    Object.fromEntries(
      FEATURES.map((name) => [name, jsonEnum(item[name], AVAILABILITY)]),
    ),
  ) as Components;
}

function decodeFreshness(value: unknown): Freshness {
  const item = exact(value, ["label"], ["lag"]);
  if (item.label === "stale")
    return Object.freeze({
      label: "stale",
      lag: jsonU64(exact(value, ["label", "lag"]).lag),
    });
  exact(value, ["label"]);
  return Object.freeze({
    label: jsonEnum(item.label, ["current", "unknown"] as const),
  });
}

/** Strict GET_MARKET_VIEW result for `market`. */
export function decodeMarketView(value: unknown, market: Hex32): MarketView {
  const item = exact(value, [
    "snapshot_id",
    "projection",
    "binding",
    "components",
    "source_activity",
    "freshness",
  ]);
  const binding = decodeBinding(item.binding);
  return Object.freeze({
    snapshotId: bindSnapshot(binding, item.snapshot_id, market),
    projection: jsonEnum(item.projection, PROJECTIONS),
    binding,
    components: decodeComponents(item.components),
    sourceActivity: jsonId(item.source_activity),
    freshness: decodeFreshness(item.freshness),
  });
}

function decodeRow(value: unknown): ParticipantRow {
  const r = exact(value, [
    "kind",
    "id",
    "owner",
    "generation",
    "identity_state",
    "frozen_member",
    "frozen_generation",
    "eligibility",
    "metadata",
    "metadata_revision",
    "score",
    "reward",
    "history",
  ]);
  const s = exact(r.score, ["status", "epoch", "ppm"]);
  const w = exact(r.reward, ["status", "asset", "earned", "claimed"]);
  const h = exact(r.history, ["status", "digest"]);
  if (typeof r.frozen_member !== "boolean") invalid();
  const score = Object.freeze({
    status: jsonEnum(s.status, SCORE_STATUS),
    epoch: jsonOptional(s.epoch, jsonU64),
    ppm: jsonOptional(s.ppm, (ppm) => jsonInteger(ppm, PPM_MAX)),
  });
  if (
    score.status === "present"
      ? score.epoch === null || score.ppm === null
      : score.ppm !== null
  )
    refuse("IntegrityFailure");
  const reward = Object.freeze({
    status: jsonEnum(w.status, AVAILABILITY),
    asset: jsonOptional(w.asset, jsonId),
    earned: jsonOptional(w.earned, jsonU128),
    claimed: jsonOptional(w.claimed, jsonU128),
  });
  const entitled =
    reward.asset !== null && reward.earned !== null && reward.claimed !== null;
  if (
    reward.status === "available"
      ? !entitled || (reward.claimed ?? 0n) > (reward.earned ?? 0n)
      : reward.asset !== null ||
        reward.earned !== null ||
        reward.claimed !== null
  )
    refuse("IntegrityFailure");
  const history = Object.freeze({
    status: jsonEnum(h.status, AVAILABILITY),
    digest: jsonOptional(h.digest, jsonId),
  });
  if ((history.status === "available") !== (history.digest !== null))
    refuse("IntegrityFailure");
  return Object.freeze({
    kind: jsonEnum(r.kind, ["worker", "evaluator"] as const),
    id: jsonId(r.id),
    owner: jsonId(r.owner),
    generation: jsonU64(r.generation),
    identityState: jsonInteger(r.identity_state, 255),
    frozenMember: r.frozen_member,
    frozenGeneration: jsonOptional(r.frozen_generation, jsonU64),
    eligibility: jsonInteger(r.eligibility, 255),
    metadata: jsonOptional(r.metadata, jsonId),
    metadataRevision: jsonU64(r.metadata_revision),
    score,
    reward,
    history,
  });
}

function rowKey(row: ParticipantRow): string {
  return `${row.kind === "worker" ? 1 : 2}${row.id}`;
}

function decodePage(
  value: unknown,
  query: NormalizedQuery,
  views: AiMarketViews,
  after: ParticipantCursor | null,
): ParticipantPage {
  const item = exact(value, [
    "snapshot_id",
    "binding",
    "components",
    "rows",
    "cursor",
  ]);
  const binding = decodeBinding(item.binding);
  const snapshotId = bindSnapshot(binding, item.snapshot_id, query.market);
  if (query.snapshot !== null && snapshotId !== query.snapshot)
    refuse("SnapshotConflict");
  if (after !== null && !sameBinding(binding, after.binding))
    refuse("SnapshotConflict");
  const components = decodeComponents(item.components);
  if (!Array.isArray(item.rows) || item.rows.length > query.limit) invalid();
  const rows = Object.freeze(item.rows.map(decodeRow));
  let previous = after?.after() ?? "";
  for (const row of rows) {
    const key = rowKey(row);
    if (
      key <= previous ||
      (query.kind !== "all" && row.kind !== query.kind) ||
      (query.activeOnly && (row.eligibility & ELIGIBLE_SERVING) === 0) ||
      (row.reward.status === "available" && components.F06 !== "available")
    )
      refuse("IntegrityFailure");
    previous = key;
  }
  const token = item.cursor === null ? null : checkCursorToken(item.cursor);
  const last = rows[rows.length - 1];
  if (token !== null && last === undefined) refuse("IntegrityFailure");
  const next =
    token === null || last === undefined
      ? null
      : new ParticipantCursor(TOKEN, {
          token,
          query,
          snapshotId,
          binding,
          after: last,
        });
  if (next !== null) CURSOR_ISSUERS.set(next, views);
  return Object.freeze({ snapshotId, binding, components, rows, next });
}

function sameBinding(left: SnapshotBinding, right: SnapshotBinding): boolean {
  return equal(encodeSnapshotBinding(left), encodeSnapshotBinding(right));
}

/** Strict GET_HISTORY result requested from `from` with `limit`. */
export function decodeEpochPage(
  value: unknown,
  from: bigint,
  limit: number,
): EpochPage {
  const item = exact(value, ["component", "entries"]);
  if (!Array.isArray(item.entries) || item.entries.length > limit) invalid();
  let previous: bigint | null = null;
  const entries = Object.freeze(
    item.entries.map((entry: unknown) => {
      const e = exact(entry, ["epoch", "status", "snapshot_id"]);
      const decoded = Object.freeze({
        epoch: jsonU64(e.epoch),
        status: jsonEnum(e.status, EPOCH_STATUS),
        snapshotId: jsonOptional(e.snapshot_id, jsonId),
      });
      const sourceless =
        decoded.status === "never-opened" ||
        decoded.status === "unsupported-version";
      if (
        decoded.epoch < from ||
        (previous !== null && decoded.epoch <= previous) ||
        sourceless !== (decoded.snapshotId === null)
      )
        refuse("IntegrityFailure");
      previous = decoded.epoch;
      return decoded;
    }),
  );
  return Object.freeze({
    component: jsonEnum(item.component, AVAILABILITY),
    entries,
  });
}

/** Historical content is usable only while the epoch's immutable source is retained. */
export function requireRetainedEpoch(entry: EpochEntry): Hex32 {
  if (
    (entry.status !== "retained" && entry.status !== "retained-terminal") ||
    entry.snapshotId === null
  ) {
    refuse("HistoryUnavailable", { epochStatus: entry.status });
  }
  return entry.snapshotId;
}

const QUERY_CATEGORIES: Readonly<Record<string, AiMarketErrorCode>> =
  Object.freeze({
    "binding-mismatch": "BindingMismatch",
    "finality-unavailable": "FinalityUnavailable",
    "snapshot-conflict": "SnapshotConflict",
    "integrity-failure": "IntegrityFailure",
    "cursor-expired": "CursorExpired",
    "cursor-mismatch": "CursorMismatch",
    "response-too-large": "ResponseTooLarge",
    "invalid-encoding": "InvalidEncoding",
  });

function gatewayResult(status: number, value: unknown): unknown {
  const envelope = exact(value, ["ok"], ["result", "error"]);
  if (envelope.ok === true && status === 200)
    return exact(value, ["ok", "result"]).result;
  if (envelope.ok !== false || status === 200) invalid();
  const error = exact(
    exact(value, ["ok", "error"]).error,
    ["code"],
    ["oldest", "lag"],
  );
  if (typeof error.code !== "string") invalid();
  if (error.code === "authority-stale")
    refuse("StaleAuthority", { lag: jsonU64(error.lag), status });
  const code = QUERY_CATEGORIES[error.code];
  return refuse(code ?? "Service", { category: error.code, status });
}

export function decodeProgramsRpcRevert(
  cause: unknown,
): RevertReasonError | null {
  if (cause instanceof RevertReasonError) return cause;
  if (
    cause === null ||
    typeof cause !== "object" ||
    !("code" in cause) ||
    cause.code !== 3
  )
    return null;
  return decodeRevertReason(
    "data" in cause && typeof cause.data === "string"
      ? cause.data
      : new Uint8Array(),
  );
}

export function encodeProgramsStateHeadCall(program: Hex32): PrecompileCall {
  if (!isId(program)) invalid();
  return Object.freeze({
    to: PROGRAMS_PRECOMPILE,
    data: `${abiSelector("getStateHead(bytes32)")}${program}`,
    value: 0n,
  });
}

export function decodeProgramsStateHead(
  value: string | Uint8Array,
): Uint8Array {
  let bytes: Uint8Array;
  if (typeof value === "string") {
    if (!/^0x(?:[0-9a-fA-F]{2})*$/u.test(value)) invalid();
    if (value.length > 2 + (64 + RESPONSE_MAX_BYTES) * 2)
      refuse("ResponseTooLarge");
    bytes = fromHex(value.slice(2).toLowerCase());
  } else {
    bytes = value;
  }
  if (bytes.length < 64 || bytes.length % 32 !== 0) invalid();
  if (BigInt(`0x${hex(bytes.subarray(0, 32))}`) !== 32n) invalid();
  const length = BigInt(`0x${hex(bytes.subarray(32, 64))}`);
  if (length > BigInt(RESPONSE_MAX_BYTES)) refuse("ResponseTooLarge");
  const size = Number(length);
  if (
    bytes.length !== 64 + Math.ceil(size / 32) * 32 ||
    bytes.subarray(64 + size).some((byte) => byte !== 0)
  )
    invalid();
  return new Uint8Array(bytes.subarray(64, 64 + size));
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
    if (
      (endpoint.protocol !== "https:" && endpoint.protocol !== "http:") ||
      endpoint.username !== "" ||
      endpoint.password !== "" ||
      !Number.isSafeInteger(timeoutMs) ||
      timeoutMs <= 0
    )
      throw new TypeError("invalid AI market view options");
    this.#endpoint = endpoint;
    this.#credential = options.credential;
    this.#timeoutMs = timeoutMs;
  }

  public async stateHead(
    program: Hex32,
    operations: Eip1193Requester,
  ): Promise<Uint8Array> {
    const call = encodeProgramsStateHeadCall(program);
    let result: unknown;
    try {
      result = await operations.request({
        method: "eth_call",
        params: [{ to: call.to, data: call.data }, "latest"],
      });
    } catch (cause) {
      throw decodeProgramsRpcRevert(cause) ?? cause;
    }
    if (typeof result !== "string") invalid();
    return decodeProgramsStateHead(result);
  }

  public async snapshot(market: Hex32, snapshot?: Hex32): Promise<MarketView> {
    if (!isId(market) || (snapshot !== undefined && !isId(snapshot))) invalid();
    const params =
      snapshot === undefined ? [] : [["snapshot", snapshot] as const];
    return decodeMarketView(
      await this.#get(`${market}/snapshot`, params),
      market,
    );
  }

  /** One page; with `cursor`, the next page of exactly the snapshot, filter and reader it was issued for. */
  public async participants(
    query: ParticipantQuery,
    cursor?: ParticipantCursor,
  ): Promise<ParticipantPage> {
    const normalized = normalizeQuery(query);
    let pinned = normalized;
    if (cursor !== undefined) {
      if (
        CURSOR_ISSUERS.get(cursor) !== this ||
        cursor.market !== normalized.market ||
        cursor.kind !== normalized.kind ||
        cursor.activeOnly !== normalized.activeOnly ||
        (normalized.snapshot !== null &&
          normalized.snapshot !== cursor.snapshotId)
      ) {
        refuse("CursorMismatch");
      }
      checkCursorToken(cursor.token);
      pinned = { ...normalized, snapshot: cursor.snapshotId };
    }
    const params: (readonly [string, string])[] = [];
    if (pinned.snapshot !== null) params.push(["snapshot", pinned.snapshot]);
    params.push(
      ["kind", pinned.kind],
      ["active_only", String(pinned.activeOnly)],
      ["limit", String(pinned.limit)],
    );
    if (cursor !== undefined) params.push(["cursor", cursor.token]);
    return decodePage(
      await this.#get(`${pinned.market}/participants`, params),
      pinned,
      this,
      cursor ?? null,
    );
  }

  public async epochs(
    market: Hex32,
    from = 0n,
    limit: number = PAXAI_LIMITS.defaultPageRows,
  ): Promise<EpochPage> {
    if (
      !isId(market) ||
      typeof from !== "bigint" ||
      from < 0n ||
      from > U64_MAX ||
      !Number.isInteger(limit) ||
      limit < 1 ||
      limit > PAXAI_LIMITS.maxPageRows
    )
      invalid();
    return decodeEpochPage(
      await this.#get(`${market}/epochs`, [
        ["from", from.toString()],
        ["limit", String(limit)],
      ]),
      from,
      limit,
    );
  }

  #get(
    view: string,
    params: readonly (readonly [string, string])[],
  ): Promise<unknown> {
    const url = new URL(
      `v1/ai/markets/${view}`,
      this.#endpoint.href.endsWith("/")
        ? this.#endpoint
        : `${this.#endpoint.href}/`,
    );
    url.search = params.map(([key, value]) => `${key}=${value}`).join("&");
    const headers: http.OutgoingHttpHeaders = {
      Accept: "application/json",
      "User-Agent": "layerx-typescript/0.1.0",
    };
    this.#credential?.use((authorization) => {
      headers.Authorization = authorization;
    });
    return new Promise<unknown>((resolve, reject) => {
      let settled = false;
      const finish = <T>(callback: (value: T) => void, value: T): void => {
        if (!settled) {
          settled = true;
          callback(value);
        }
      };
      const driver = url.protocol === "https:" ? https : http;
      const request = driver.request(
        url,
        { method: "GET", headers, timeout: this.#timeoutMs },
        (response) => {
          const chunks: Buffer[] = [];
          let received = 0;
          response.on("data", (chunk: Buffer) => {
            received += chunk.length;
            if (received > RESPONSE_MAX_BYTES) {
              response.destroy();
              finish(reject, new AiMarketError("ResponseTooLarge"));
              return;
            }
            chunks.push(Buffer.from(chunk));
          });
          response.on("end", () => {
            try {
              if (
                response.headers["content-type"]?.split(";")[0]?.trim() !==
                "application/json"
              )
                invalid();
              let parsed: unknown;
              try {
                parsed = JSON.parse(
                  new TextDecoder("utf-8", { fatal: true }).decode(
                    Buffer.concat(chunks),
                  ),
                );
              } catch {
                invalid();
              }
              finish(resolve, gatewayResult(response.statusCode ?? 0, parsed));
            } catch (error) {
              finish(reject, error);
            }
          });
          response.on("error", () =>
            finish(
              reject,
              new PlatformSdkError({
                code: "transport-failure",
                retry: "safe",
              }),
            ),
          );
        },
      );
      request.on("timeout", () => request.destroy());
      request.on("error", () =>
        finish(
          reject,
          new PlatformSdkError({ code: "transport-failure", retry: "safe" }),
        ),
      );
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

export interface DecodedEnvelope {
  readonly envelope: RequestEnvelope;
  readonly unsigned: Uint8Array;
}

function operationOf(selector: number): OperationSpec {
  return (
    PAXAI_OPERATIONS.get(selector) ??
    application(AI_APPLICATION_ERRORS.UNKNOWN_OPERATION)
  );
}

function validateEnvelope(envelope: RequestEnvelope): OperationSpec {
  const spec = operationOf(envelope.selector);
  const length = envelope.payload.length;
  const metadataLength =
    (envelope.selector === 0x0106 || envelope.selector === 0x0107) &&
    length !== 40 &&
    length !== 48;
  if (
    metadataLength ||
    length < spec.payloadMin ||
    length > spec.payloadMax ||
    length > PAXAI_LIMITS.payloadMaxBytes ||
    envelope.expiry === 0n ||
    (spec.sequence === "role"
      ? envelope.sequence === 0n
      : envelope.sequence !== 0n)
  ) {
    application(AI_APPLICATION_ERRORS.NON_CANONICAL);
  }
  if (envelope.delegated && spec.delegation !== "delegate")
    application(AI_APPLICATION_ERRORS.UNAUTHORIZED);
  if (
    spec.boundary === "program-read"
      ? envelope.epoch !== 0n ||
        envelope.config !== 0n ||
        envelope.roster !== null ||
        envelope.delegated
      : envelope.config === 0n
  )
    application(AI_APPLICATION_ERRORS.NON_CANONICAL);
  if (
    envelope.roster === null &&
    (envelope.epoch !== 0n || !registryBeforeEpoch(envelope.selector))
  ) {
    application(AI_APPLICATION_ERRORS.WRONG_ROSTER);
  }
  return spec;
}

/** Natively authenticated request envelope bytes. */
export function encodeRequestEnvelope(envelope: RequestEnvelope): Uint8Array {
  if (envelope.delegated) application(AI_APPLICATION_ERRORS.UNAUTHORIZED);
  validateEnvelope(envelope);
  return new Writer()
    .put(ENVELOPE_MAGIC)
    .u16(1)
    .u16(envelope.selector)
    .id(envelope.chain)
    .id(envelope.program)
    .id(envelope.market)
    .id(envelope.actor)
    .u64(envelope.epoch)
    .u64(envelope.config)
    .put(
      envelope.roster === null ? new Uint8Array(32) : idBytes(envelope.roster),
    )
    .u64(envelope.sequence)
    .u64(envelope.expiry)
    .id(envelope.request)
    .sized(envelope.payload)
    .u8(0)
    .bytes();
}

export function decodeRequestEnvelope(input: Uint8Array): DecodedEnvelope {
  if (input.length > PAXAI_LIMITS.envelopeMaxBytes)
    application(AI_APPLICATION_ERRORS.CAPACITY);
  const r = new Reader(input, () =>
    application(AI_APPLICATION_ERRORS.NON_CANONICAL),
  );
  if (!equal(r.fixed(ENVELOPE_MAGIC.length), ENVELOPE_MAGIC))
    application(AI_APPLICATION_ERRORS.NON_CANONICAL);
  if (r.u16() !== 1) application(AI_APPLICATION_ERRORS.BAD_VERSION);
  const selector = r.u16();
  operationOf(selector);
  const chain = r.id(),
    program = r.id(),
    market = r.id(),
    actor = r.id(),
    epoch = r.u64(),
    config = r.u64();
  const rosterBytes = hex(r.fixed(32));
  const sequence = r.u64(),
    expiry = r.u64(),
    request = r.id();
  const payloadLength = r.u32();
  if (payloadLength > PAXAI_LIMITS.payloadMaxBytes)
    application(AI_APPLICATION_ERRORS.CAPACITY);
  const payload = r.fixed(payloadLength);
  const unsigned = input.slice(0, r.offset);
  const authentication = r.u8();
  if (authentication === 1) r.fixed(96);
  else if (authentication !== 0)
    application(AI_APPLICATION_ERRORS.NON_CANONICAL);
  r.end();
  const envelope: RequestEnvelope = Object.freeze({
    selector,
    chain,
    program,
    market,
    actor,
    epoch,
    config,
    roster: rosterBytes === ZERO32 ? null : rosterBytes,
    sequence,
    expiry,
    request,
    payload,
    delegated: authentication === 1,
  });
  validateEnvelope(envelope);
  return Object.freeze({ envelope, unsigned });
}

/** Request intent: H("PAXAI/request/v1", envelope bytes before authentication). */
export function requestIntent(unsigned: Uint8Array): Hex32 {
  return domainHash("PAXAI/request/v1", unsigned);
}

function checkDomain(
  envelope: RequestEnvelope,
  binding: SnapshotBinding,
): void {
  if (envelope.chain !== binding.chain)
    application(AI_APPLICATION_ERRORS.WRONG_DOMAIN);
  if (envelope.program !== binding.program)
    application(AI_APPLICATION_ERRORS.WRONG_PROGRAM);
  if (envelope.market !== binding.market)
    application(AI_APPLICATION_ERRORS.WRONG_MARKET);
}

export interface FundRequest {
  readonly amount: bigint;
  readonly refundRecipient: Hex32;
  readonly policyVersion: bigint;
  readonly consent: boolean;
}
export interface ClaimRequest {
  readonly worker: Hex32;
  readonly recipient: Hex32;
  readonly amount: bigint;
}
export interface RefundRequest {
  readonly expectedRefunded: bigint;
  readonly amount: bigint;
  readonly recipient: Hex32;
}

export function encodeFundRequest(value: FundRequest): Uint8Array {
  return new Writer()
    .u128(value.amount)
    .id(value.refundRecipient)
    .u64(value.policyVersion)
    .u8(value.consent ? 1 : 0)
    .bytes();
}
export function encodeClaimRequest(value: ClaimRequest): Uint8Array {
  return new Writer()
    .id(value.worker)
    .id(value.recipient)
    .u128(value.amount)
    .bytes();
}
export function encodeRefundRequest(value: RefundRequest): Uint8Array {
  return new Writer()
    .u128(value.expectedRefunded)
    .u128(value.amount)
    .id(value.recipient)
    .bytes();
}

function payloadReader(bytes: Uint8Array, length: number): Reader {
  if (bytes.length !== length) application(AI_APPLICATION_ERRORS.NON_CANONICAL);
  return new Reader(bytes, () =>
    application(AI_APPLICATION_ERRORS.NON_CANONICAL),
  );
}
export function decodeFundRequest(bytes: Uint8Array): FundRequest {
  const r = payloadReader(bytes, 57);
  const value = Object.freeze({
    amount: r.u128(),
    refundRecipient: r.id(),
    policyVersion: r.u64(),
    consent: r.boolean(),
  });
  r.end();
  return value;
}
export function decodeClaimRequest(bytes: Uint8Array): ClaimRequest {
  const r = payloadReader(bytes, 80);
  const value = Object.freeze({
    worker: r.id(),
    recipient: r.id(),
    amount: r.u128(),
  });
  r.end();
  return value;
}
export function decodeRefundRequest(bytes: Uint8Array): RefundRequest {
  const r = payloadReader(bytes, 64);
  const value = Object.freeze({
    expectedRefunded: r.u128(),
    amount: r.u128(),
    recipient: r.id(),
  });
  r.end();
  return value;
}

// ---------------------------------------------------------------------------------------------

export interface ProgramCallActivity {
  readonly payload: Uint8Array;
  readonly feeLimit: bigint;
}

function abiWord(value: bigint, bits = 256): Uint8Array {
  if (typeof value !== "bigint" || value < 0n || value >= 1n << BigInt(bits))
    invalid();
  return fromHex(value.toString(16).padStart(64, "0"));
}

function abiBytes(bytes: Uint8Array): Uint8Array {
  const padded = new Uint8Array(Math.ceil(bytes.length / 32) * 32);
  padded.set(bytes);
  return concat(abiWord(BigInt(bytes.length)), padded);
}

export function encodeProgramCallActivity(
  activity: ProgramCallActivity,
): Uint8Array {
  const call = decodeNativeProgramCallV1(activity.payload);
  const tails = [
    ascii(call.entrypoint),
    call.calldata,
    call.capabilities,
    call.accessDeclaration,
  ].map(abiBytes);
  let offset = 15 * 32;
  const offsets = tails.map((tail) => {
    const word = abiWord(BigInt(offset));
    offset += tail.length;
    return word;
  });
  return concat(
    PROGRAMS_CALL_SELECTOR,
    call.programId,
    abiWord(BigInt(call.guestAbi), 16),
    ...offsets,
    abiWord(BigInt(call.responseCapacity), 32),
    ...call.resources.map((value) => abiWord(value, 64)),
    abiWord(activity.feeLimit, 128),
    ...tails,
  );
}

export function decodeProgramCallActivity(
  bytes: Uint8Array,
): ProgramCallActivity {
  if (
    bytes.length < 484 ||
    bytes.length > MAX_CALL_BYTES ||
    !equal(bytes.slice(0, 4), PROGRAMS_CALL_SELECTOR)
  ) {
    refuse("NotNativeProgramCall");
  }
  const word = (index: number, bits = 256): bigint => {
    const value = BigInt(
      `0x${hex(bytes.slice(4 + index * 32, 36 + index * 32))}`,
    );
    if (value >= 1n << BigInt(bits)) invalid();
    return value;
  };
  const dynamic = (index: number, maximum: number): Uint8Array => {
    const offset = word(index);
    if (
      offset < 480n ||
      offset % 32n !== 0n ||
      offset > BigInt(bytes.length - 36)
    )
      invalid();
    const start = Number(offset) + 4;
    const length = BigInt(`0x${hex(bytes.slice(start, start + 32))}`);
    if (length > BigInt(maximum) || length > BigInt(bytes.length - start - 32))
      invalid();
    return bytes.slice(start + 32, start + 32 + Number(length));
  };
  let entrypoint: string;
  try {
    entrypoint = new TextDecoder("utf-8", { fatal: true }).decode(
      dynamic(2, 128),
    );
  } catch {
    return invalid();
  }
  const resources: NativeProgramCallV1["resources"] = [
    word(7, 64),
    word(8, 64),
    word(9, 64),
    word(10, 64),
    word(11, 64),
    word(12, 64),
    word(13, 64),
  ];
  let payload: Uint8Array;
  try {
    payload = encodeNativeProgramCallV1({
      programId: bytes.slice(4, 36),
      guestAbi: Number(word(1, 16)) as NativeProgramCallV1["guestAbi"],
      entrypoint,
      calldata: dynamic(3, 1_048_576),
      capabilities: dynamic(4, 65_535),
      accessDeclaration: dynamic(5, 1_048_576),
      responseCapacity: Number(word(6, 32)),
      resources,
    });
  } catch (cause) {
    return refuse("NativeCall", { cause });
  }
  const activity = Object.freeze({ payload, feeLimit: word(14, 128) });
  if (!equal(encodeProgramCallActivity(activity), bytes)) invalid();
  return activity;
}

type Rlp = Uint8Array | readonly Rlp[];

function rlpEncode(value: Rlp): Uint8Array {
  const list = Array.isArray(value);
  const bytes = list
    ? concat(...(value as readonly Rlp[]).map(rlpEncode))
    : (value as Uint8Array);
  if (!list && bytes.length === 1 && bytes[0] !== undefined && bytes[0] < 128)
    return bytes.slice();
  const base = list ? 192 : 128;
  if (bytes.length < 56)
    return concat(Uint8Array.of(base + bytes.length), bytes);
  const text = bytes.length.toString(16);
  const size = fromHex(text.padStart(text.length + (text.length % 2), "0"));
  return concat(Uint8Array.of(base + 55 + size.length), size, bytes);
}

function rlpUint(value: bigint): Uint8Array {
  if (value < 0n || value >= 1n << 256n) invalid();
  if (value === 0n) return new Uint8Array();
  const text = value.toString(16);
  return fromHex(text.padStart(text.length + (text.length % 2), "0"));
}

function rlpDecode(bytes: Uint8Array): Rlp {
  let offset = 0;
  const take = (length: number, end: number): Uint8Array => {
    if (!Number.isSafeInteger(length) || length < 0 || offset + length > end)
      invalid();
    const out = bytes.slice(offset, offset + length);
    offset += length;
    return out;
  };
  const item = (end: number, depth: number): Rlp => {
    if (depth > 2) invalid();
    const prefix = take(1, end)[0];
    if (prefix === undefined) invalid();
    if (prefix < 128) return Uint8Array.of(prefix);
    const list = prefix >= 192;
    const base = list ? 192 : 128;
    let length = prefix - base;
    if (length > 55) {
      const size = take(length - 55, end);
      if (size[0] === 0 || size.length > 4) invalid();
      length = Number(BigInt(`0x${hex(size)}`));
      if (length < 56) invalid();
    }
    if (!list) {
      const out = take(length, end);
      if (length === 1 && out[0] !== undefined && out[0] < 128) invalid();
      return out;
    }
    const listEnd = offset + length;
    if (listEnd > end) invalid();
    const out: Rlp[] = [];
    while (offset < listEnd) {
      out.push(item(listEnd, depth + 1));
      if (out.length > 12) invalid();
    }
    return out;
  };
  const decoded = item(bytes.length, 0);
  if (offset !== bytes.length || !equal(rlpEncode(decoded), bytes)) invalid();
  return decoded;
}

function transactionFields(
  data: Uint8Array,
  nonce: bigint,
  fees: Omit<BindFees, "evmNonce">,
): readonly Rlp[] {
  if (
    nonce > U64_MAX ||
    fees.gasLimit <= 0n ||
    fees.gasLimit > U64_MAX ||
    fees.maxPriorityFeePerGas > fees.maxFeePerGas
  )
    invalid();
  return [
    rlpUint(125n),
    rlpUint(nonce),
    rlpUint(fees.maxPriorityFeePerGas),
    rlpUint(fees.maxFeePerGas),
    rlpUint(fees.gasLimit),
    fromHex(PROGRAMS_PRECOMPILE.slice(2)),
    rlpUint(0n),
    data,
    [],
  ];
}

function encodeTransaction(
  fields: readonly Rlp[],
  signature: Uint8Array | null = null,
): Uint8Array {
  if (signature === null) return concat(Uint8Array.of(2), rlpEncode(fields));
  const parity = signature[64];
  if (signature.length !== 65 || (parity !== 27 && parity !== 28))
    refuse("Signature");
  return concat(
    Uint8Array.of(2),
    rlpEncode([
      ...fields,
      rlpUint(BigInt(parity - 27)),
      rlpUint(BigInt(`0x${hex(signature.slice(0, 32))}`)),
      rlpUint(BigInt(`0x${hex(signature.slice(32, 64))}`)),
    ]),
  );
}

function decodeTransaction(bytes: Uint8Array) {
  if (bytes[0] !== 2 || bytes.length > MAX_CALL_BYTES + 256) invalid();
  const decoded = rlpDecode(bytes.slice(1));
  if (
    !Array.isArray(decoded) ||
    (decoded.length !== 9 && decoded.length !== 12)
  )
    invalid();
  const fields = decoded as readonly Rlp[];
  const raw = (index: number): Uint8Array => {
    const field = fields[index];
    if (!(field instanceof Uint8Array)) invalid();
    return field;
  };
  const integer = (index: number): bigint => {
    const bytes = raw(index);
    if (bytes.length > 32 || bytes[0] === 0) invalid();
    return bytes.length === 0 ? 0n : BigInt(`0x${hex(bytes)}`);
  };
  if (
    integer(0) !== 125n ||
    !equal(raw(5), fromHex(PROGRAMS_PRECOMPILE.slice(2))) ||
    integer(6) !== 0n ||
    !Array.isArray(fields[8]) ||
    fields[8].length !== 0
  )
    invalid();
  const fees = {
    gasLimit: integer(4),
    maxFeePerGas: integer(3),
    maxPriorityFeePerGas: integer(2),
  };
  const nonce = integer(1);
  const canonical = encodeTransaction(transactionFields(raw(7), nonce, fees));
  let signature: Uint8Array | null = null;
  if (fields.length === 12) {
    if (integer(9) > 1n) refuse("Signature");
    signature = concat(
      abiWord(integer(10)),
      abiWord(integer(11)),
      Uint8Array.of(27 + Number(integer(9))),
    );
    if (!equal(encodeTransaction(fields.slice(0, 9), signature), bytes))
      invalid();
  }
  const activity = decodeProgramCallActivity(raw(7));
  return {
    fields: fields.slice(0, 9),
    canonical,
    nonce,
    fees,
    activity,
    signature,
  };
}

function recoveredKey(signature: Uint8Array, preimage: Uint8Array): Uint8Array {
  try {
    const parity = signature[64];
    if (signature.length !== 65 || (parity !== 27 && parity !== 28))
      return refuse("Signature");
    const parsed = secp256k1.Signature.fromBytes(
      signature.slice(0, 64),
      "compact",
    );
    if (parsed.hasHighS()) return refuse("Signature");
    return parsed
      .addRecoveryBit(parity - 27)
      .recoverPublicKey(preimage)
      .toBytes(true);
  } catch {
    return refuse("Signature");
  }
}

export function signingPreimage(unsigned: Uint8Array): Uint8Array {
  const transaction = decodeTransaction(unsigned);
  if (transaction.signature !== null) invalid();
  return keccak_256(unsigned);
}
export function activityIdOf(signed: Uint8Array): Hex32 {
  return hex(keccak_256(signed));
}

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

export interface NativeTerms extends Omit<BindFees, "evmNonce"> {
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
  | Readonly<{
      kind: "fund";
      amount: bigint;
      asset: Hex32 | null;
      refundRecipient: Hex32;
    }>
  | Readonly<{
      kind: "claim";
      worker: Hex32;
      recipient: Hex32;
      amount: bigint;
      asset: Hex32 | null;
    }>
  | Readonly<{
      kind: "refund-free";
      expectedRefunded: bigint;
      amount: bigint;
      recipient: Hex32;
      asset: Hex32 | null;
    }>
  | Readonly<{ kind: "other"; selector: number }>;

function effectOf(
  envelope: RequestEnvelope,
  claimAsset: Hex32 | null,
): OperationEffect {
  switch (envelope.selector) {
    case FUND: {
      const fund = decodeFundRequest(envelope.payload);
      return Object.freeze({
        kind: "fund",
        amount: fund.amount,
        asset: null,
        refundRecipient: fund.refundRecipient,
      });
    }
    case CLAIM: {
      const claim = decodeClaimRequest(envelope.payload);
      return Object.freeze({
        kind: "claim",
        worker: claim.worker,
        recipient: claim.recipient,
        amount: claim.amount,
        asset: claimAsset,
      });
    }
    case REFUND_FREE: {
      const refund = decodeRefundRequest(envelope.payload);
      return Object.freeze({
        kind: "refund-free",
        expectedRefunded: refund.expectedRefunded,
        amount: refund.amount,
        recipient: refund.recipient,
        asset: null,
      });
    }
    default:
      return Object.freeze({ kind: "other", selector: envelope.selector });
  }
}

/** The program's F06 refusal order for the checks the served views expose. */
function preview(
  envelope: RequestEnvelope,
  snapshotId: Hex32,
  entitlements: ParticipantPage | undefined,
): OperationEffect {
  if (envelope.selector === FUND) {
    const fund = decodeFundRequest(envelope.payload);
    if (fund.policyVersion !== FUNDING_POLICY_VERSION)
      application(AI_APPLICATION_ERRORS.F06_FUNDING_POLICY_MISMATCH);
    if (!fund.consent)
      application(AI_APPLICATION_ERRORS.F06_CONTRIBUTION_CONSENT_REQUIRED);
    if (fund.amount === 0n)
      application(AI_APPLICATION_ERRORS.F06_INVALID_AMOUNT);
  } else if (envelope.selector === CLAIM) {
    const claim = decodeClaimRequest(envelope.payload);
    if (entitlements !== undefined && entitlements.snapshotId !== snapshotId)
      refuse("SnapshotConflict");
    const row = entitlements?.rows.find(
      (candidate) =>
        candidate.kind === "worker" && candidate.id === claim.worker,
    );
    if (row === undefined || row.reward.status !== "available")
      application(AI_APPLICATION_ERRORS.F06_UNKNOWN_WORKER_ENTITLEMENT);
    if (claim.amount === 0n)
      application(AI_APPLICATION_ERRORS.F06_NOTHING_TO_CLAIM);
    return effectOf(envelope, row.reward.asset);
  } else if (envelope.selector === REFUND_FREE) {
    if (decodeRefundRequest(envelope.payload).amount === 0n)
      application(AI_APPLICATION_ERRORS.F06_INVALID_AMOUNT);
  }
  return effectOf(envelope, null);
}

export function prepareOperation(
  view: Pick<MarketView, "binding" | "snapshotId">,
  authorityHeight: bigint,
  request: OperationRequest,
  terms: NativeTerms,
): PreparedOperation {
  const binding = view.binding;
  if (snapshotIdOf(binding) !== view.snapshotId) refuse("IntegrityFailure");
  requireCurrent(binding, authorityHeight);
  if (
    !Number.isInteger(request.requiredRank) ||
    request.requiredRank < PAXAI_LIMITS.finalizedRank ||
    request.requiredRank > PAXAI_LIMITS.settlementRank
  )
    application(AI_APPLICATION_ERRORS.NON_CANONICAL);
  if (request.requiredRank > binding.rank) refuse("FinalityUnavailable");
  if (operationOf(request.selector).boundary !== "mutation")
    refuse("NotMutation");
  let epoch = 0n;
  let roster: Hex32 | null = null;
  if (request.roster === "bound") {
    if (binding.epoch === null || binding.roster === null)
      application(AI_APPLICATION_ERRORS.WRONG_ROSTER);
    epoch = binding.epoch;
    roster = binding.roster;
  } else if (request.roster !== "absent") invalid();
  const envelope: RequestEnvelope = Object.freeze({
    selector: request.selector,
    chain: binding.chain,
    program: binding.program,
    market: binding.market,
    actor: request.actor,
    epoch,
    config: binding.config,
    roster,
    sequence: request.sequence,
    expiry: request.expiry,
    request: request.request,
    payload: new Uint8Array(request.payload),
    delegated: false,
  });
  const calldata = encodeRequestEnvelope(envelope);
  const intent = requestIntent(decodeRequestEnvelope(calldata).unsigned);
  const effect = preview(envelope, view.snapshotId, request.entitlements);
  let call: Uint8Array;
  try {
    call = encodeNativeProgramCallV1({
      programId: idBytes(binding.program),
      guestAbi: GUEST_ABI as NativeProgramCallV1["guestAbi"],
      entrypoint: ENTRYPOINT,
      calldata,
      capabilities: terms.capabilities,
      accessDeclaration: terms.accessDeclaration,
      responseCapacity: terms.responseCapacity,
      resources: terms.resources,
    });
  } catch (cause) {
    return refuse("NativeCall", { cause });
  }
  if (
    terms.networkId !== 125 ||
    terms.ownerPublicKey.length !== 33 ||
    terms.notBefore !== 0n ||
    terms.notAfter !== request.expiry ||
    terms.idempotencyKey !== request.request ||
    new TextEncoder().encode(terms.actorDid).length === 0 ||
    new TextEncoder().encode(terms.actorDid).length > 255
  )
    invalid();
  try {
    secp256k1.Point.fromBytes(terms.ownerPublicKey).assertValidity();
  } catch {
    return invalid();
  }
  const data = encodeProgramCallActivity({
    payload: call,
    feeLimit: terms.feeLimit,
  });
  const canonical = encodeTransaction(
    transactionFields(data, terms.accountSequence, terms),
  );
  return new PreparedOperation(TOKEN, {
    canonical,
    binding,
    snapshotId: view.snapshotId,
    intent,
    effect,
    ownerPublicKey: terms.ownerPublicKey.slice(),
  });
}

interface PlannedOperation {
  readonly ownerPublicKey: Uint8Array;
  readonly canonical: Uint8Array;
  readonly binding: SnapshotBinding;
  readonly snapshotId: Hex32;
  readonly intent: Hex32;
  readonly effect: OperationEffect;
}

export class PreparedOperation {
  readonly #plan: PlannedOperation;
  public constructor(token: typeof TOKEN, plan: PlannedOperation) {
    if (token !== TOKEN)
      throw new TypeError("operations are prepared by prepareOperation");
    this.#plan = plan;
  }
  public get binding(): SnapshotBinding {
    return this.#plan.binding;
  }
  public get snapshotId(): Hex32 {
    return this.#plan.snapshotId;
  }
  public get intent(): Hex32 {
    return this.#plan.intent;
  }
  public get effect(): OperationEffect {
    return this.#plan.effect;
  }
  public get expectedRevision(): bigint {
    return this.#plan.binding.revision;
  }
  public canonicalBytes(): Uint8Array {
    return this.#plan.canonical.slice();
  }
  /** Content identity of the canonical bytes; equals the native disclosure's preparation id. */
  public get preparationId(): Hex32 {
    return hex(sha256(this.#plan.canonical));
  }
  public get signingPreimage(): Hex32 {
    return hex(signingPreimage(this.#plan.canonical));
  }

  /**
   * Derives every approval term from the native disclosure of the exact canonical bytes, never from
   * the planning inputs.
   */
  public review(disclosure: NativePrepareResultV1): Review {
    let decoded: NativePrepareResultV1;
    try {
      decoded = decodeNativePrepareResult(disclosure);
    } catch (cause) {
      return refuse("InvalidEncoding", { cause });
    }
    if (
      decoded.activity.module !== String(PROGRAMS_MODULE) ||
      decoded.activity.ordinal !== String(PROGRAM_CALL_ORDINAL)
    ) {
      refuse("NotNativeProgramCall");
    }
    const canonical = fromHex(decoded.canonical_bytes);
    if (
      !equal(canonical, this.#plan.canonical) ||
      decoded.preparation_id !== hex(sha256(canonical)) ||
      decoded.signing_preimage !== hex(signingPreimage(canonical))
    )
      refuse("IntegrityFailure");
    const transaction = decodeTransaction(canonical);
    if (transaction.signature !== null) refuse("IntegrityFailure");
    const activity = transaction.activity;
    let call: NativeProgramCallV1;
    try {
      call = decodeNativeProgramCallV1(activity.payload);
    } catch (cause) {
      return refuse("NativeCall", { cause });
    }
    const binding = this.#plan.binding;
    if (
      call.guestAbi !== GUEST_ABI ||
      call.entrypoint !== ENTRYPOINT ||
      hex(call.programId) !== binding.program
    ) {
      refuse("NotNativeProgramCall");
    }
    const validated = decodeRequestEnvelope(call.calldata);
    const envelope = validated.envelope;
    checkDomain(envelope, binding);
    const claimAsset =
      this.#plan.effect.kind === "claim" ? this.#plan.effect.asset : null;
    if (
      requestIntent(validated.unsigned) !== this.#plan.intent ||
      !sameEffect(effectOf(envelope, claimAsset), this.#plan.effect)
    ) {
      refuse("IntegrityFailure");
    }
    const terms: ApprovalTerms = Object.freeze({
      action: envelope.selector,
      chain: envelope.chain,
      program: envelope.program,
      market: envelope.market,
      actor: envelope.actor,
      epoch: envelope.epoch,
      config: envelope.config,
      roster: envelope.roster,
      policy: binding.policy,
      snapshot: this.#plan.snapshotId,
      effect: this.#plan.effect,
      capabilities: hex(call.capabilities),
      accessDeclaration: hex(call.accessDeclaration),
      responseCapacity: call.responseCapacity,
      resources: Object.freeze([
        ...call.resources,
      ]) as ApprovalTerms["resources"],
      feeLimit: activity.feeLimit,
      notBefore: 0n,
      notAfter: envelope.expiry,
      expiry: envelope.expiry,
      idempotencyKey: envelope.request,
      authority: hex(sha256(this.#plan.ownerPublicKey)),
      commitment: decoded.preparation_id,
      evmNonce: transaction.nonce,
      ...transaction.fees,
    });
    return new Review(TOKEN, this.#plan, terms);
  }
}

/** Every term the owner approves, derived from the disclosed canonical bytes. */
export interface ApprovalTerms extends BindFees {
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

function sameEffect(left: OperationEffect, right: OperationEffect): boolean {
  return effectDifference(left, right) === null;
}

function effectDifference(
  actual: OperationEffect,
  approved: OperationEffect,
): ReviewField | null {
  let checks: readonly (readonly [boolean, ReviewField])[];
  if (actual.kind === "fund" && approved.kind === "fund") {
    checks = [
      [actual.amount === approved.amount, "amount"],
      [actual.asset === approved.asset, "asset"],
      [actual.refundRecipient === approved.refundRecipient, "payee"],
    ];
  } else if (actual.kind === "claim" && approved.kind === "claim") {
    checks = [
      [actual.amount === approved.amount, "amount"],
      [actual.asset === approved.asset, "asset"],
      [actual.recipient === approved.recipient, "payee"],
      [actual.worker === approved.worker, "worker"],
    ];
  } else if (actual.kind === "refund-free" && approved.kind === "refund-free") {
    checks = [
      [
        actual.amount === approved.amount &&
          actual.expectedRefunded === approved.expectedRefunded,
        "amount",
      ],
      [actual.asset === approved.asset, "asset"],
      [actual.recipient === approved.recipient, "payee"],
    ];
  } else if (actual.kind === "other" && approved.kind === "other") {
    checks = [[actual.selector === approved.selector, "action"]];
  } else checks = [[false, "action"]];
  return checks.find(([same]) => !same)?.[1] ?? null;
}

function firstDifference(
  actual: ApprovalTerms,
  approved: ApprovalTerms,
): ReviewField | null {
  const binding: readonly (readonly [boolean, ReviewField])[] = [
    [actual.action === approved.action, "action"],
    [actual.chain === approved.chain, "chain"],
    [actual.program === approved.program, "program"],
    [actual.market === approved.market, "market"],
    [actual.actor === approved.actor, "actor"],
    [actual.epoch === approved.epoch, "epoch"],
    [actual.config === approved.config, "config"],
    [actual.roster === approved.roster, "roster"],
    [actual.policy === approved.policy, "policy"],
    [actual.snapshot === approved.snapshot, "snapshot"],
  ];
  const native: readonly (readonly [boolean, ReviewField])[] = [
    [actual.capabilities === approved.capabilities, "capabilities"],
    [
      actual.accessDeclaration === approved.accessDeclaration,
      "access_declaration",
    ],
    [
      actual.responseCapacity === approved.responseCapacity,
      "response_capacity",
    ],
    [
      actual.resources.length === approved.resources.length &&
        actual.resources.every(
          (value, index) => value === approved.resources[index],
        ),
      "resources",
    ],
    [actual.feeLimit === approved.feeLimit, "fee_limit"],
    [
      actual.notBefore === approved.notBefore &&
        actual.notAfter === approved.notAfter,
      "validity",
    ],
    [actual.expiry === approved.expiry, "expiry"],
    [actual.idempotencyKey === approved.idempotencyKey, "idempotency_key"],
    [actual.authority === approved.authority, "authority"],
    [actual.commitment === approved.commitment, "commitment"],
    [actual.evmNonce === approved.evmNonce, "nonce"],
    [
      actual.gasLimit === approved.gasLimit &&
        actual.maxFeePerGas === approved.maxFeePerGas &&
        actual.maxPriorityFeePerGas === approved.maxPriorityFeePerGas,
      "gas",
    ],
  ];
  return (
    binding.find(([same]) => !same)?.[1] ??
    effectDifference(actual.effect, approved.effect) ??
    native.find(([same]) => !same)?.[1] ??
    null
  );
}

/** The disclosure-derived terms presented to the owner for one prepared operation. */
export class Review {
  public readonly terms: ApprovalTerms;
  readonly #plan: PlannedOperation;
  public constructor(
    token: typeof TOKEN,
    plan: PlannedOperation,
    terms: ApprovalTerms,
  ) {
    if (token !== TOKEN)
      throw new TypeError("reviews are derived by PreparedOperation.review");
    this.#plan = plan;
    this.terms = terms;
  }
  /** Accepts the owner's approved terms only when every term equals the disclosed bytes and the key is the activity owner. */
  public approve(approved: ApprovalTerms, key: Uint8Array): Approval {
    const field = firstDifference(this.terms, approved);
    if (field !== null) refuse("ReviewMismatch", { field });
    if (!equal(this.#plan.ownerPublicKey, key)) refuse("UnauthorizedKey");
    return new Approval(TOKEN, this.#plan, new Uint8Array(key));
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
  readonly #key: Uint8Array;
  public constructor(
    token: typeof TOKEN,
    plan: PlannedOperation,
    key: Uint8Array,
  ) {
    if (token !== TOKEN)
      throw new TypeError("approvals are issued by Review.approve");
    this.#plan = plan;
    this.#key = key;
  }
  /**
   * Signs exactly the approved canonical bytes, verifies the signature and returns the durable Signed record.
   * The approving key's EVM address must be bound to `layerxPublicKey` on the `addr` precompile read through
   * `binding`; an unbound or foreign-bound address is refused before the signer is invoked.
   */
  public async sign(
    signer: OperationSigner,
    authorityHeight: bigint,
    binding: Eip1193Requester,
    layerxPublicKey: string,
  ): Promise<OperationRecord> {
    requireCurrent(this.#plan.binding, authorityHeight);
    if (!equal(signer.publicKey, this.#key)) refuse("UnauthorizedKey");
    const uncompressed = secp256k1.Point.fromBytes(this.#key).toBytes(false);
    const address = `0x${hex(keccak_256(uncompressed.subarray(1)).subarray(12))}`;
    requireLayerXBinding(
      { layerxPublicKey },
      await readLayerXBindState(binding, address),
    );
    const preimage = signingPreimage(this.#plan.canonical);
    const signature = new Uint8Array(await signer.sign(preimage.slice()));
    if (!equal(recoveredKey(signature, preimage), this.#key))
      refuse("Signature");
    const signed = encodeTransaction(
      decodeTransaction(this.#plan.canonical).fields,
      signature,
    );
    return OperationRecord.signed(signed, this.#key, this.#plan.intent);
  }
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

function signedIdentity(
  signed: Uint8Array,
  ownerHash: Uint8Array,
): Pick<
  RecordFields,
  "activityId" | "idempotencyKey" | "protocolVersion" | "networkId" | "notAfter"
> {
  try {
    const transaction = decodeTransaction(signed);
    if (
      transaction.signature === null ||
      !equal(
        sha256(
          recoveredKey(
            transaction.signature,
            keccak_256(transaction.canonical),
          ),
        ),
        ownerHash,
      )
    ) {
      return refuse("CorruptRecord");
    }
    const call = decodeNativeProgramCallV1(transaction.activity.payload);
    if (call.guestAbi !== GUEST_ABI || call.entrypoint !== ENTRYPOINT)
      return refuse("CorruptRecord");
    const envelope = decodeRequestEnvelope(call.calldata).envelope;
    if (operationOf(envelope.selector).boundary !== "mutation")
      return refuse("CorruptRecord");
    return {
      activityId: activityIdOf(signed),
      idempotencyKey: envelope.request,
      protocolVersion: 2,
      networkId: 125,
      notAfter: envelope.expiry,
    };
  } catch {
    return refuse("CorruptRecord");
  }
}

function signedIntent(signed: Uint8Array): Hex32 {
  try {
    const transaction = decodeTransaction(signed);
    const call = decodeNativeProgramCallV1(transaction.activity.payload);
    const envelope = decodeRequestEnvelope(call.calldata);
    if (hex(call.programId) !== envelope.envelope.program)
      return refuse("CorruptRecord");
    return requestIntent(envelope.unsigned);
  } catch {
    return refuse("CorruptRecord");
  }
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

  public static signed(
    signed: Uint8Array,
    key: Uint8Array,
    intent: Hex32,
  ): OperationRecord {
    if (key.length !== 33 || signedIntent(signed) !== intent)
      refuse("CorruptRecord");
    const ownerHash = sha256(key);
    const identity = signedIdentity(signed, ownerHash);
    return new OperationRecord({
      ...identity,
      state: "signed",
      domain: "pending",
      intent,
      signerPublicKey: hex(ownerHash),
      attempt: 0,
      resultCode: null,
      globalSequence: null,
      checkpoint: null,
      signedBytes: signed,
    });
  }

  public get signedBytes(): Uint8Array {
    return this.#signed.slice();
  }

  public get txHash(): string {
    return `0x${this.activityId}`;
  }

  public get rung(): "submitted" | "receipted" | null {
    if (this.globalSequence !== null) return "receipted";
    return this.state === "pending" || this.state === "unknown"
      ? "submitted"
      : null;
  }

  public with(
    changes: Partial<
      Omit<
        RecordFields,
        | "signedBytes"
        | "activityId"
        | "idempotencyKey"
        | "intent"
        | "signerPublicKey"
      >
    >,
    token: typeof TOKEN,
  ): OperationRecord {
    if (token !== TOKEN)
      throw new TypeError("record transitions are owned by OperationJournal");
    return new OperationRecord({ ...this.#fields(), ...changes });
  }

  public encode(): Uint8Array {
    const w = new Writer()
      .put(RECORD_MAGIC)
      .u8(STATE_CODES.indexOf(this.state) + 1)
      .u8(this.domain === "pending" ? 1 : 2)
      .u16(this.protocolVersion)
      .u32(this.networkId)
      .id(this.activityId)
      .id(this.idempotencyKey)
      .id(this.intent)
      .id(this.signerPublicKey)
      .u64(this.notAfter)
      .u32(this.attempt);
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
    const domain: DomainStatus =
      domainCode === 1 ? "pending" : domainCode === 2 ? "completed" : corrupt();
    const protocolVersion = r.u16(),
      networkId = r.u32(),
      activityId = r.id(),
      idempotencyKey = r.id(),
      intent = r.id();
    const signerPublicKey = r.id(),
      notAfter = r.u64(),
      attempt = r.u32();
    const optional = <T>(read: () => T): T | null =>
      r.boolean() ? read() : null;
    const resultCode = optional(() =>
      new DataView(r.fixed(4).buffer).getInt32(0),
    );
    const globalSequence = optional(() => r.u64());
    const checkpoint = optional(() => hex(r.fixed(32)));
    const signedBytes = r.fixed(r.u32());
    r.end();
    const identity = signedIdentity(signedBytes, idBytes(signerPublicKey));
    const record = new OperationRecord({
      state,
      domain,
      protocolVersion,
      networkId,
      activityId,
      idempotencyKey,
      intent,
      signerPublicKey,
      notAfter,
      attempt,
      resultCode,
      globalSequence,
      checkpoint,
      signedBytes,
    });
    if (
      identity.activityId !== activityId ||
      identity.idempotencyKey !== idempotencyKey ||
      identity.protocolVersion !== protocolVersion ||
      identity.networkId !== networkId ||
      identity.notAfter !== notAfter ||
      signedIntent(signedBytes) !== intent ||
      !record.#consistent()
    )
      corrupt();
    return record;
  }

  #fields(): RecordFields {
    return {
      state: this.state,
      domain: this.domain,
      protocolVersion: this.protocolVersion,
      networkId: this.networkId,
      activityId: this.activityId,
      idempotencyKey: this.idempotencyKey,
      intent: this.intent,
      signerPublicKey: this.signerPublicKey,
      notAfter: this.notAfter,
      attempt: this.attempt,
      resultCode: this.resultCode,
      globalSequence: this.globalSequence,
      checkpoint: this.checkpoint,
      signedBytes: this.#signed,
    };
  }

  #consistent(): boolean {
    const pristine =
      this.resultCode === null &&
      this.globalSequence === null &&
      this.checkpoint === null;
    const executed = this.resultCode === 0 && this.globalSequence !== null;
    switch (this.state) {
      case "prepared":
      case "reviewed":
        return false;
      case "signed":
        return pristine && this.attempt === 0;
      case "submitting":
      case "pending":
      case "unknown":
        return pristine && this.attempt > 0;
      case "executed":
        return executed && this.checkpoint === null && this.attempt > 0;
      case "finalized":
        return executed && this.checkpoint !== null && this.attempt > 0;
      case "failed":
        return (
          this.checkpoint === null &&
          (this.resultCode === null
            ? this.globalSequence === null
            : this.resultCode !== 0)
        );
    }
  }
}

function int32(value: number): Uint8Array {
  const out = new Uint8Array(4);
  new DataView(out.buffer).setInt32(0, value);
  return out;
}

function requireState(
  record: OperationRecord,
  allowed: readonly OperationState[],
): void {
  if (!allowed.includes(record.state))
    refuse("InvalidTransition", { from: record.state });
}

function errorCode(error: unknown): string | undefined {
  return error !== null &&
    typeof error === "object" &&
    "code" in error &&
    typeof error.code === "string"
    ? error.code
    : undefined;
}

/** Crash-safe journal of signed operations; one file per activity, replaced atomically. */
export class OperationJournal {
  readonly #directory: string;
  readonly #receiptKeys: readonly SequencerAuthorization[];
  private constructor(
    directory: string,
    receiptKeys: readonly SequencerAuthorization[],
  ) {
    this.#directory = directory;
    this.#receiptKeys = receiptKeys;
  }

  public static async open(
    directory: string,
    receiptKeys: readonly SequencerAuthorization[],
  ): Promise<OperationJournal> {
    if (receiptKeys.length === 0) refuse("ReceiptMismatch");
    const seen = new Set<string>();
    const keys = receiptKeys.map((key) => {
      if (
        key.sequencerId.length !== 32 ||
        key.sequencerId.every((byte) => byte === 0) ||
        key.publicKey.length !== 32 ||
        key.firstBatchNumber < 1n ||
        key.lastBatchNumber > U64_MAX ||
        key.firstBatchNumber > key.lastBatchNumber ||
        seen.has(hex(key.sequencerId))
      )
        refuse("ReceiptMismatch");
      try {
        const point = ed25519.Point.fromBytes(key.publicKey, false);
        if (point.isSmallOrder() || !point.isTorsionFree())
          return refuse("ReceiptMismatch");
      } catch {
        return refuse("ReceiptMismatch");
      }
      seen.add(hex(key.sequencerId));
      return Object.freeze({
        ...key,
        sequencerId: key.sequencerId.slice(),
        publicKey: key.publicKey.slice(),
      });
    });
    try {
      await mkdir(directory, { recursive: true });
    } catch (cause) {
      return refuse("Journal", { cause });
    }
    return new OperationJournal(directory, keys);
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
      try {
        await file.writeFile(record.encode());
        await file.sync();
      } finally {
        await file.close();
      }
      await rename(partial, path);
      const directory = await open(this.#directory, "r");
      try {
        await directory.sync();
      } finally {
        await directory.close();
      }
    } catch (cause) {
      refuse("Journal", { cause });
    }
  }

  /** Loads a record; an interrupted `submitting` attempt is recovered as `unknown`. */
  public async load(activityId: Hex32): Promise<OperationRecord> {
    let bytes: Uint8Array;
    try {
      bytes = await readFile(this.recordPath(activityId));
    } catch (cause) {
      return refuse("Journal", { cause });
    }
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
    try {
      existing = await readFile(this.recordPath(record.activityId));
    } catch (cause) {
      if (errorCode(cause) !== "ENOENT") refuse("Journal", { cause });
    }
    if (existing !== null)
      requireState(OperationRecord.decode(existing), ["signed"]);
    await this.persist(record);
  }

  public async submit(
    record: OperationRecord,
    operations: Eip1193Requester,
  ): Promise<OperationRecord> {
    requireState(record, ["signed"]);
    return await this.#deliver(record, operations);
  }

  /** Re-sends the exact retained signed bytes of an operation whose outcome is unknown. */
  public async resendExact(
    record: OperationRecord,
    operations: Eip1193Requester,
  ): Promise<OperationRecord> {
    requireState(record, ["unknown"]);
    return await this.#deliver(record, operations);
  }

  public async resolve(
    record: OperationRecord,
    submission: unknown,
  ): Promise<OperationRecord> {
    requireState(record, ["pending", "unknown"]);
    const execution = decodeProgramsReceipt(
      submission,
      record.signedBytes,
      this.#receiptKeys,
    );
    if (execution === null) return record;
    const code =
      execution.programResult?.terminalResult ?? execution.receipt.resultCode;
    const resolved = record.with(
      {
        state: code === 0 ? "executed" : "failed",
        resultCode: code,
        globalSequence: execution.receipt.globalSequence,
      },
      TOKEN,
    );
    await this.persist(resolved);
    if (execution.revert !== null) throw execution.revert;
    return resolved;
  }

  public async resolveThrough(
    record: OperationRecord,
    operations: Eip1193Requester,
  ): Promise<OperationRecord> {
    requireState(record, ["pending", "unknown"]);
    let submission: unknown;
    try {
      submission = await operations.request({
        method: "pax_sequencerReceipt",
        params: [`0x${record.activityId}`],
      });
    } catch {
      return record;
    }
    return await this.resolve(record, submission);
  }

  /** A signed operation never sent becomes `failed` once its validity window has passed. */
  public async expire(
    record: OperationRecord,
    now: bigint,
  ): Promise<OperationRecord> {
    requireState(record, ["signed"]);
    if (now <= record.notAfter) return record;
    const failed = record.with({ state: "failed" }, TOKEN);
    await this.persist(failed);
    return failed;
  }

  async #deliver(
    record: OperationRecord,
    operations: Eip1193Requester,
  ): Promise<OperationRecord> {
    if (record.attempt >= 0xffff_ffff) refuse("CorruptRecord");
    const submitting = record.with(
      { state: "submitting", attempt: record.attempt + 1 },
      TOKEN,
    );
    await this.persist(submitting);
    let hash: unknown;
    try {
      hash = await operations.request({
        method: "eth_sendRawTransaction",
        params: [`0x${hex(record.signedBytes)}`],
      });
    } catch (cause) {
      let revert: RevertReasonError | null;
      try {
        revert = decodeProgramsRpcRevert(cause);
      } catch (error) {
        await this.persist(submitting.with({ state: "failed" }, TOKEN));
        throw error;
      }
      if (revert !== null) {
        await this.persist(submitting.with({ state: "failed" }, TOKEN));
        throw revert;
      }
      const unknown = submitting.with({ state: "unknown" }, TOKEN);
      await this.persist(unknown);
      return unknown;
    }
    if (
      typeof hash !== "string" ||
      hash.toLowerCase() !== `0x${record.activityId}`
    ) {
      await this.persist(submitting.with({ state: "unknown" }, TOKEN));
      return refuse("ReceiptMismatch");
    }
    const pending = submitting.with({ state: "pending" }, TOKEN);
    await this.persist(pending);
    return await this.resolveThrough(pending, operations);
  }
}

export function decodeProgramsCallResult(
  bytes: Uint8Array,
  responseCapacity: number,
) {
  if (
    !Number.isInteger(responseCapacity) ||
    responseCapacity < 0 ||
    responseCapacity > 1_048_576 ||
    bytes.length < 128 ||
    bytes.length % 32 !== 0
  )
    refuse("ReceiptMismatch");
  const word = (index: number): bigint =>
    BigInt(`0x${hex(bytes.slice(index * 32, (index + 1) * 32))}`);
  const value = word(0);
  const signed = BigInt.asIntN(256, value);
  if (
    signed < -0x8000_0000n ||
    signed > 0x7fff_ffffn ||
    word(1) < 1n ||
    word(1) > 3n ||
    word(2) !== 96n ||
    (word(1) === 1n) !== (signed === 0n)
  )
    refuse("ReceiptMismatch");
  const length = word(3);
  if (
    length > BigInt(responseCapacity) ||
    bytes.length !== 128 + Math.ceil(Number(length) / 32) * 32
  )
    refuse("ReceiptMismatch");
  const response = bytes.slice(128, 128 + Number(length));
  if (bytes.slice(128 + Number(length)).some((byte) => byte !== 0))
    refuse("ReceiptMismatch");
  return Object.freeze({
    terminalResult: Number(signed),
    terminalKind: Number(word(1)),
    response,
  });
}

function receiptObject(value: unknown): Readonly<Record<string, unknown>> {
  if (typeof value !== "object" || value === null || Array.isArray(value))
    refuse("ReceiptMismatch");
  return value as Readonly<Record<string, unknown>>;
}

function rpcBytes(value: unknown, exact?: number): Uint8Array {
  if (typeof value !== "string" || !/^0x(?:[0-9a-fA-F]{2})*$/u.test(value))
    refuse("ReceiptMismatch");
  const bytes = fromHex(value.slice(2).toLowerCase());
  if (exact !== undefined && bytes.length !== exact) refuse("ReceiptMismatch");
  return bytes;
}

function rpcQuantity(value: unknown): bigint {
  if (
    typeof value !== "string" ||
    !/^0x(?:0|[1-9a-fA-F][0-9a-fA-F]*)$/u.test(value)
  )
    refuse("ReceiptMismatch");
  return BigInt(value);
}

export function decodeProgramsReceipt(
  value: unknown,
  signed: Uint8Array,
  receiptKeys: readonly SequencerAuthorization[],
) {
  if (value === null) return null;
  const v = receiptObject(value);
  const transaction = decodeTransaction(signed);
  if (transaction.signature === null) refuse("ReceiptMismatch");
  const trader = keccak_256(
    secp256k1.Point.fromBytes(
      recoveredKey(transaction.signature, keccak_256(transaction.canonical)),
    )
      .toBytes(false)
      .slice(1),
  ).slice(12);
  if (
    !equal(rpcBytes(v.txHash, 32), keccak_256(signed)) ||
    rpcQuantity(v.nonce) !== transaction.nonce
  )
    refuse("ReceiptMismatch");
  if (v.rung === "submitted") {
    if (
      Object.keys(v).some((key) => !["rung", "txHash", "nonce"].includes(key))
    )
      refuse("ReceiptMismatch");
    return null;
  }
  if (v.rung !== "receipted" && v.rung !== "committed")
    refuse("ReceiptMismatch");
  const canonical = rpcBytes(v.receipt);
  if (canonical.length > 1_048_576) refuse("ReceiptMismatch");
  const r = new Reader(canonical, () => refuse("ReceiptMismatch"));
  if (r.u16() !== 2 || r.u16() !== 0x5201 || r.u16() !== 2)
    refuse("ReceiptMismatch");
  const arr = (): Uint8Array => r.sized(32, 32);
  const activityId = arr(),
    globalSequence = r.u64(),
    previousStateRoot = arr(),
    resultingStateRoot = arr(),
    activityRoot = arr();
  const resultCode = new DataView(r.fixed(4).buffer).getInt32(0);
  const effectCount = r.u32();
  if (effectCount === 0 || effectCount > Math.floor(1_048_576 / 48))
    refuse("ReceiptMismatch");
  const effects: ReceiptEffect[] = [];
  for (let index = 0; index < effectCount; index += 1) {
    const moduleId = r.u16(),
      ordinal = r.u16(),
      eventType = r.u16(),
      kind = r.u8(),
      monetary = r.u8();
    const transferSetRoot = arr(),
      body = r.sized(256);
    if (
      ordinal !== index ||
      eventType !== 0 ||
      kind !== (index === 0 ? 1 : 3) ||
      monetary !== 0 ||
      transferSetRoot.some((byte) => byte !== 0)
    )
      refuse("ReceiptMismatch");
    effects.push({
      moduleId,
      ordinal,
      eventType,
      kind: index === 0 ? 1 : 3,
      monetary: false,
      transferSetRoot,
      body,
    });
  }
  const feeCharged = r.u128(),
    batchId = arr(),
    moduleId = r.u16(),
    moduleVersion = r.u32(),
    parameterVersion = r.u32(),
    operation = r.u8();
  const asset = arr(),
    amount = r.u128(),
    from = arr(),
    fromBalanceBefore = r.u128(),
    fromBalanceAfter = r.u128(),
    fromSequence = r.u64();
  const to = arr(),
    toBalanceBefore = r.u128(),
    toBalanceAfter = r.u128(),
    transferSetRoot = arr(),
    authorizationHash = arr(),
    contextHash = arr(),
    timestamp = r.u64();
  if (r.u8() !== 1) refuse("ReceiptMismatch");
  const sequencerSignature = r.sized(64, 64);
  r.end();
  const receipt: ProtocolReceipt = Object.freeze({
    protocolVersion: 2,
    activityId,
    globalSequence,
    previousStateRoot,
    resultingStateRoot,
    activityRoot,
    resultCode,
    effects,
    feeCharged,
    batchId,
    moduleId,
    moduleVersion,
    parameterVersion,
    operation,
    asset,
    amount,
    from,
    fromBalanceBefore,
    fromBalanceAfter,
    fromSequence,
    to,
    toBalanceBefore,
    toBalanceAfter,
    transferSetRoot,
    authorizationHash,
    contextHash,
    timestamp,
    sequencerSignature,
  });
  const height = new DataView(
    batchId.buffer,
    batchId.byteOffset,
    batchId.byteLength,
  ).getBigUint64(24);
  const key = receiptKeys.find(
    (entry) =>
      equal(entry.sequencerId, contextHash) &&
      entry.firstBatchNumber <= height &&
      height <= entry.lastBatchNumber,
  );
  if (
    key === undefined ||
    !ed25519.verify(
      sequencerSignature,
      sha256(
        concat(
          ascii("LXP/v1/receipt\0"),
          canonical.slice(0, -69),
          Uint8Array.of(0),
        ),
      ),
      key.publicKey,
      { zip215: false },
    )
  ) {
    refuse("ReceiptMismatch");
  }
  if (
    height === 0n ||
    batchId.slice(0, 24).some((byte) => byte !== 0) ||
    globalSequence === 0n ||
    timestamp === 0n ||
    moduleId <= 11 ||
    moduleVersion !== 1 ||
    parameterVersion !== 0 ||
    operation !== 2 ||
    amount !== 0n ||
    !equal(activityId, keccak_256(signed)) ||
    fromSequence !== transaction.nonce ||
    !equal(from, concat(new Uint8Array(12), trader)) ||
    !equal(
      to,
      concat(new Uint8Array(12), fromHex(PROGRAMS_PRECOMPILE.slice(2))),
    ) ||
    transferSetRoot.some((byte) => byte !== 0) ||
    authorizationHash.some((byte) => byte !== 0) ||
    effects.some((effect) => effect.moduleId !== moduleId) ||
    !equal(
      resultingStateRoot,
      sha256(
        concat(
          ascii("PAXEER/v1/sequence\0"),
          previousStateRoot,
          new Writer().u64(globalSequence).bytes(),
          activityId,
        ),
      ),
    )
  )
    refuse("ReceiptMismatch");
  const summary = effects[0];
  if (summary === undefined) refuse("ReceiptMismatch");
  const sr = new Reader(summary.body, () => refuse("ReceiptMismatch"));
  const gasUsed = sr.u64(),
    cumulativeGasUsed = sr.u64(),
    effectiveGasPrice = sr.u64(),
    index = sr.u32();
  const contractAddress = sr.fixed(20),
    sponsor = sr.fixed(20),
    vmErrorDigest = sr.fixed(32),
    returnDataLength = sr.u32(),
    logCount = sr.u32();
  const denomLength = sr.u8();
  if (denomLength === 0 || denomLength > 64) refuse("ReceiptMismatch");
  let feeDenom: string;
  try {
    feeDenom = new TextDecoder("utf-8", { fatal: true }).decode(
      sr.fixed(denomLength),
    );
  } catch {
    return refuse("ReceiptMismatch");
  }
  sr.end();
  const returnData = rpcBytes(v.returnData);
  if (
    typeof v.vmError !== "string" ||
    typeof v.feeDenom !== "string" ||
    v.feeDenom !== feeDenom ||
    !equal(sha256(ascii(v.vmError)), vmErrorDigest) ||
    !equal(sha256(ascii(feeDenom)), asset) ||
    returnData.length !== returnDataLength ||
    !equal(sha256(returnData), activityRoot) ||
    gasUsed > transaction.fees.gasLimit ||
    effectiveGasPrice > transaction.fees.maxFeePerGas ||
    (resultCode !== 0 && resultCode !== 1)
  )
    refuse("ReceiptMismatch");
  const status = resultCode === 0 ? 1n : 0n;
  const quantities = {
    seq: globalSequence,
    height,
    index: BigInt(index),
    timestamp,
    status,
    gasUsed,
    cumulativeGasUsed,
    effectiveGasPrice,
    feeAmount: feeCharged,
  };
  for (const [field, expected] of Object.entries(quantities))
    if (rpcQuantity(v[field]) !== expected) refuse("ReceiptMismatch");
  for (const [field, expected] of Object.entries({
    keyId: contextHash,
    from: trader,
    to: to.slice(12),
    contractAddress,
    sponsor,
  })) {
    if (!equal(rpcBytes(v[field], expected.length), expected))
      refuse("ReceiptMismatch");
  }
  if (
    !Array.isArray(v.logs) ||
    v.logs.length !== logCount ||
    effects.length !== logCount + 1
  )
    refuse("ReceiptMismatch");
  for (let i = 0; i < logCount; i += 1) {
    const effect = effects[i + 1];
    if (effect === undefined) refuse("ReceiptMismatch");
    const lr = new Reader(effect.body, () => refuse("ReceiptMismatch"));
    const address = lr.fixed(20),
      topicsCount = lr.u8();
    if (topicsCount > 4) refuse("ReceiptMismatch");
    const topics = Array.from({ length: topicsCount }, () => lr.fixed(32));
    const dataHash = lr.fixed(32);
    lr.end();
    const log = receiptObject(v.logs[i]);
    if (
      !equal(rpcBytes(log.address, 20), address) ||
      rpcQuantity(log.logIndex) !== BigInt(i) ||
      !Array.isArray(log.topics) ||
      log.topics.length !== topicsCount ||
      !topics.every((topic, n) =>
        equal(topic, rpcBytes((log.topics as unknown[])[n], 32)),
      ) ||
      !equal(sha256(rpcBytes(log.data)), dataHash)
    )
      refuse("ReceiptMismatch");
  }
  const call = decodeNativeProgramCallV1(transaction.activity.payload);
  const revert = status === 0n ? decodeRevertReason(returnData) : null;
  return Object.freeze({
    receipt,
    programResult:
      status === 1n
        ? decodeProgramsCallResult(returnData, call.responseCapacity)
        : null,
    refusal:
      revert === null
        ? null
        : `${revert.code}: ${revert.message}${revert.detail === undefined ? "" : `; ${revert.detail}`}`,
    revert,
  });
}

// ---------------------------------------------------------------------------------------------
// Bytes

function ascii(value: string): Uint8Array {
  return new TextEncoder().encode(value);
}
function concat(...parts: readonly Uint8Array[]): Uint8Array {
  const out = new Uint8Array(
    parts.reduce((total, part) => total + part.length, 0),
  );
  let offset = 0;
  for (const part of parts) {
    out.set(part, offset);
    offset += part.length;
  }
  return out;
}
function hex(bytes: Uint8Array): string {
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join(
    "",
  );
}
function fromHex(value: string): Uint8Array {
  if (value.length % 2 !== 0 || !/^[0-9a-f]*$/u.test(value)) invalid();
  return Uint8Array.from(value.match(/../gu) ?? [], (pair) =>
    Number.parseInt(pair, 16),
  );
}
function isId(value: unknown): value is Hex32 {
  return typeof value === "string" && HEX32.test(value) && value !== ZERO32;
}
function idBytes(value: Hex32): Uint8Array {
  return isId(value) ? fromHex(value) : invalid();
}
function equal(left: Uint8Array, right: Uint8Array): boolean {
  return (
    left.length === right.length &&
    left.every((value, index) => value === right[index])
  );
}
