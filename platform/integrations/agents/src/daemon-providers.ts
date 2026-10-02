import { closeSync, existsSync, lstatSync, mkdirSync, openSync, realpathSync } from "node:fs";
import { dirname, isAbsolute, resolve } from "node:path";
import { DatabaseSync } from "node:sqlite";
import type {
  AgentReceiptContext, AgentReceiptEvidence, AgentReceiptRecording, AgentReceiptResolver,
} from "@sidiora/layerx-agent-middleware";
import {
  AgentEnvelopeTransport, PlatformSdkError, isSelectableProtocolVersion, verifyReceipt,
  type AgentEnvelopeSuccess, type SelectableProtocolVersion,
} from "@sidiora/layerx-sdk";

export interface DaemonReceiptPrincipal {
  readonly tenant: string;
  readonly actor: string;
  readonly sessionId: string;
}

export interface DaemonReceiptResolverOptions {
  readonly transport: AgentEnvelopeTransport;
  readonly protocolVersion: SelectableProtocolVersion;
  readonly principal: DaemonReceiptPrincipal;
  readonly storePath: string;
}

interface Binding extends AgentReceiptContext { readonly receiptDigest: string }
const APPLICATION_ID = 0x4c585250;
const MAX_BINDINGS = 65536;
const MAX_RECEIPT_BYTES = 1048576;
const VERIFIED_LEVELS = new Set([
  "SequencerSigned", "BatchIncluded", "StateProven", "CheckpointFinalised", "SettlementAnchored",
]);
const CONTEXT_SCHEMA = "CREATE TABLE receipt_context (singleton INTEGER PRIMARY KEY CHECK (singleton = 1), tenant TEXT NOT NULL, actor TEXT NOT NULL, session_id TEXT NOT NULL, protocol_version INTEGER NOT NULL) STRICT";
const BINDING_SCHEMA = "CREATE TABLE receipt_bindings (receipt_digest TEXT PRIMARY KEY, idempotency_key TEXT NOT NULL UNIQUE, activity_id TEXT NOT NULL UNIQUE) STRICT";
const ALIAS_SCHEMA = "CREATE TABLE receipt_aliases (reference TEXT PRIMARY KEY, receipt_digest TEXT NOT NULL REFERENCES receipt_bindings(receipt_digest)) STRICT";

export class DaemonReceiptResolver implements AgentReceiptResolver {
  readonly #transport: AgentEnvelopeTransport;
  readonly #protocolVersion: SelectableProtocolVersion;
  readonly #principal: Readonly<DaemonReceiptPrincipal>;
  readonly #path: string;
  readonly #database: DatabaseSync;
  #closed = false;

  public constructor(options: DaemonReceiptResolverOptions) {
    if (!(options.transport instanceof AgentEnvelopeTransport) || !isSelectableProtocolVersion(options.protocolVersion)
      || typeof options.storePath !== "string" || !isAbsolute(options.storePath)
      || options.storePath.length > 4096 || options.storePath.includes("\0")) throw invalidArgument();
    this.#transport = options.transport;
    this.#protocolVersion = options.protocolVersion;
    this.#principal = Object.freeze({
      tenant: boundedText(options.principal.tenant, 255, invalidArgument),
      actor: boundedText(options.principal.actor, 255, invalidArgument),
      sessionId: identifier(options.principal.sessionId, invalidArgument),
    });
    this.#path = resolve(options.storePath);
    let database: DatabaseSync | undefined;
    try {
      const parent = dirname(this.#path);
      mkdirSync(parent, { recursive: true, mode: 0o700 });
      secureOwnerPath(parent, true);
      if (realpathSync(parent) !== parent) throw storeFailure();
      if (!existsSync(this.#path)) {
        try { closeSync(openSync(this.#path, "wx", 0o600)); }
        catch (error) { if (!(error instanceof Error) || (error as NodeJS.ErrnoException).code !== "EEXIST") throw error; }
      }
      secureOwnerPath(this.#path, false);
      database = new DatabaseSync(this.#path, {
        timeout: 5000, enableForeignKeyConstraints: true,
        enableDoubleQuotedStringLiterals: false, allowExtension: false,
      });
      initialize(database, this.#principal, this.#protocolVersion);
      secureFiles(this.#path);
      this.#database = database;
    } catch (error) {
      database?.close();
      if (error instanceof PlatformSdkError) throw error;
      throw storeFailure();
    }
  }

  public close(): void {
    if (this.#closed) return;
    this.#database.close();
    this.#closed = true;
  }

  public async resolve(receiptRef: string): Promise<AgentReceiptEvidence> {
    const reference = boundedText(receiptRef, 255, invalidArgument);
    this.#assertPrincipal();
    let binding: Binding;
    try {
      const row = this.#database.prepare(`SELECT b.receipt_digest, b.idempotency_key, b.activity_id
        FROM receipt_aliases a JOIN receipt_bindings b ON a.receipt_digest = b.receipt_digest
        WHERE a.reference = ?`).get(reference);
      if (row === undefined) throw unavailable();
      binding = decodeBinding(row);
    } catch (error) {
      if (error instanceof PlatformSdkError) throw error;
      throw storeFailure();
    }
    return (await this.#lookup(binding, binding.receiptDigest)).evidence;
  }

  public async resolveFor(receiptRef: string, input: AgentReceiptContext): Promise<AgentReceiptEvidence> {
    const reference = boundedText(receiptRef, 255, invalidArgument);
    const context = copyContext(input);
    const fetched = await this.#lookup(context);
    this.#persist({ ...context, receiptDigest: fetched.receiptDigest }, reference);
    return fetched.evidence;
  }

  public async retainVerified(input: AgentReceiptRecording): Promise<void> {
    const context = copyContext(input);
    const receiptDigest = identifier(input.receiptDigest, invalidArgument);
    const reference = input.receiptRef === undefined ? receiptDigest : boundedText(input.receiptRef, 255, invalidArgument);
    await this.#lookup(context, receiptDigest);
    this.#persist({ ...context, receiptDigest }, reference);
  }

  async #lookup(context: AgentReceiptContext, expectedDigest?: string): Promise<{ evidence: AgentReceiptEvidence; receiptDigest: string }> {
    this.#assertPrincipal();
    const response = await this.#transport.call<
      { readonly idempotency_key: string; readonly expected_activity_id: string }, AgentEnvelopeSuccess
    >({
      plane: "agent", operation: "program.receipt",
      request: { idempotency_key: context.idempotencyKey, expected_activity_id: context.activityId },
    });
    const value = record(response.value, decodeFailure);
    if (value["found"] === false) { exact(value, ["found"], decodeFailure); throw unavailable(); }
    exact(value, ["found", "receipt"], decodeFailure);
    if (value["found"] !== true) throw decodeFailure();
    const receipt = record(value["receipt"], decodeFailure);
    exact(receipt, ["canonical_bytes", "authorised_batch", "verification_level"], decodeFailure);
    const level = receipt["verification_level"];
    if (typeof level !== "string" || !VERIFIED_LEVELS.has(level)) throw verificationFailure();
    const status = record(response.verification_status, decodeFailure);
    exact(status, ["state", "level"], decodeFailure);
    if (status["state"] !== "achieved" || status["level"] !== level) throw verificationFailure();
    const batch = record(receipt["authorised_batch"], decodeFailure);
    exact(batch, ["batch_id", "asset", "previous_state_root", "resulting_state_root", "sequencer_public_key"], decodeFailure);
    const fixed = (field: string): Uint8Array => Buffer.from(hex(batch[field], 32, 32), "hex");
    const evidence: AgentReceiptEvidence = {
      canonicalReceipt: Buffer.from(hex(receipt["canonical_bytes"], MAX_RECEIPT_BYTES), "hex"),
      authorizedBatch: { batchId: fixed("batch_id"), asset: fixed("asset"),
        previousStateRoot: fixed("previous_state_root"), resultingStateRoot: fixed("resulting_state_root"),
        sequencerPublicKey: fixed("sequencer_public_key") },
    };
    try {
      const verified = await verifyReceipt(evidence.canonicalReceipt, evidence.authorizedBatch, { protocolVersion: this.#protocolVersion });
      const receiptDigest = Buffer.from(verified.receiptDigest).toString("hex");
      if (expectedDigest !== undefined && receiptDigest !== expectedDigest
        || Buffer.from(verified.receipt.activityId).toString("hex") !== context.activityId) throw verificationFailure();
      return { evidence, receiptDigest };
    } catch { throw verificationFailure(); }
  }

  #persist(binding: Binding, reference: string): void {
    this.#assertPrincipal();
    try {
      secureFiles(this.#path);
      this.#database.exec("BEGIN IMMEDIATE");
      this.#assertPrincipal();
      const found = this.#database.prepare(`SELECT receipt_digest, idempotency_key, activity_id FROM receipt_bindings
        WHERE receipt_digest = ? OR idempotency_key = ? OR activity_id = ?`).all(binding.receiptDigest, binding.idempotencyKey, binding.activityId);
      if (found.length > 1) throw conflict();
      if (found.length === 1) {
        const previous = decodeBinding(found[0]);
        if (previous.receiptDigest !== binding.receiptDigest || previous.idempotencyKey !== binding.idempotencyKey
          || previous.activityId !== binding.activityId) throw conflict();
      } else {
        const count = countRows(this.#database, "receipt_bindings");
        if (count >= MAX_BINDINGS) throw storeFailure();
        const changed = this.#database.prepare("INSERT INTO receipt_bindings (receipt_digest, idempotency_key, activity_id) VALUES (?, ?, ?)")
          .run(binding.receiptDigest, binding.idempotencyKey, binding.activityId);
        if (changed.changes !== 1) throw storeFailure();
      }
      for (const alias of new Set([binding.receiptDigest, reference])) {
        const foundAlias = this.#database.prepare("SELECT receipt_digest FROM receipt_aliases WHERE reference = ?").get(alias);
        if (foundAlias !== undefined) {
          if (foundAlias["receipt_digest"] !== binding.receiptDigest) throw conflict();
        } else {
          if (countRows(this.#database, "receipt_aliases") >= MAX_BINDINGS * 2) throw storeFailure();
          const changed = this.#database.prepare("INSERT INTO receipt_aliases (reference, receipt_digest) VALUES (?, ?)").run(alias, binding.receiptDigest);
          if (changed.changes !== 1) throw storeFailure();
        }
      }
      secureFiles(this.#path);
      this.#database.exec("COMMIT");
    } catch (error) {
      try { if (this.#database.isTransaction) this.#database.exec("ROLLBACK"); }
      catch { throw storeFailure(); }
      if (error instanceof PlatformSdkError) throw error;
      throw storeFailure();
    }
  }

  #assertPrincipal(): void {
    if (this.#closed) throw unavailable();
    try { assertPrincipal(this.#database, this.#principal, this.#protocolVersion); }
    catch { throw storeFailure(); }
  }
}

function initialize(database: DatabaseSync, principal: DaemonReceiptPrincipal, protocolVersion: number): void {
  database.exec("PRAGMA trusted_schema = OFF; PRAGMA foreign_keys = ON; BEGIN IMMEDIATE");
  try {
    const app = pragma(database, "application_id"), version = pragma(database, "user_version");
    const objects = database.prepare("SELECT type, name FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").all();
    if (app === 0 && version === 0 && objects.length === 0) {
      database.exec(`${CONTEXT_SCHEMA}; ${BINDING_SCHEMA}; ${ALIAS_SCHEMA}; PRAGMA application_id = ${APPLICATION_ID}; PRAGMA user_version = 1`);
      database.prepare("INSERT INTO receipt_context (singleton, tenant, actor, session_id, protocol_version) VALUES (1, ?, ?, ?, ?)")
        .run(principal.tenant, principal.actor, principal.sessionId, protocolVersion);
    } else if (app !== APPLICATION_ID || version !== 1 || objects.length !== 3) throw storeFailure();
    for (const [name, sql] of [["receipt_context", CONTEXT_SCHEMA], ["receipt_bindings", BINDING_SCHEMA], ["receipt_aliases", ALIAS_SCHEMA]]) {
      if (name === undefined || sql === undefined) throw storeFailure();
      const row = database.prepare("SELECT sql FROM sqlite_schema WHERE type = 'table' AND name = ?").get(name);
      if (row?.["sql"] !== sql) throw storeFailure();
    }
    assertPrincipal(database, principal, protocolVersion);
    if (countRows(database, "receipt_bindings") > MAX_BINDINGS || countRows(database, "receipt_aliases") > MAX_BINDINGS * 2) throw storeFailure();
    database.exec("COMMIT");
  } catch (error) { if (database.isTransaction) database.exec("ROLLBACK"); throw error; }
  database.exec("PRAGMA journal_mode = WAL; PRAGMA synchronous = FULL; PRAGMA temp_store = MEMORY; PRAGMA wal_autocheckpoint = 100; PRAGMA journal_size_limit = 8388608");
  const pageSize = pragma(database, "page_size");
  if (pageSize < 512 || pageSize > 65536 || (pageSize & (pageSize - 1)) !== 0) throw storeFailure();
  const pages = Math.floor(67108864 / pageSize);
  database.exec(`PRAGMA max_page_count = ${pages}`);
  if (pragma(database, "max_page_count") !== pages || pragma(database, "synchronous") !== 2) throw storeFailure();
  const checked = database.prepare("PRAGMA quick_check").all();
  if (checked.length !== 1 || checked[0]?.["quick_check"] !== "ok" || database.prepare("PRAGMA foreign_key_check").all().length !== 0) throw storeFailure();
}
function assertPrincipal(database: DatabaseSync, principal: DaemonReceiptPrincipal, protocolVersion: number): void {
  const rows = database.prepare("SELECT singleton, tenant, actor, session_id, protocol_version FROM receipt_context").all();
  const row = rows[0];
  if (rows.length !== 1 || row === undefined || row["singleton"] !== 1 || row["tenant"] !== principal.tenant || row["actor"] !== principal.actor
    || row["session_id"] !== principal.sessionId || row["protocol_version"] !== protocolVersion) throw storeFailure();
}
function countRows(database: DatabaseSync, table: "receipt_bindings" | "receipt_aliases"): number {
  const value = database.prepare(`SELECT COUNT(*) AS count FROM ${table}`).get()?.["count"];
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) throw storeFailure();
  return value;
}
function pragma(database: DatabaseSync, name: string): number {
  const value = database.prepare(`PRAGMA ${name}`).get()?.[name];
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) throw storeFailure();
  return value;
}
function secureOwnerPath(path: string, directory: boolean): void {
  const metadata = lstatSync(path);
  if (metadata.isSymbolicLink() || (directory ? !metadata.isDirectory() : !metadata.isFile() || metadata.nlink !== 1)
    || (metadata.mode & 0o077) !== 0 || process.getuid !== undefined && metadata.uid !== process.getuid()) throw storeFailure();
}
function secureFiles(path: string): void {
  for (const candidate of [path, `${path}-wal`, `${path}-shm`]) if (existsSync(candidate)) secureOwnerPath(candidate, false);
}
function decodeBinding(value: unknown): Binding {
  const row = record(value, storeFailure);
  return { receiptDigest: identifier(row["receipt_digest"], storeFailure),
    idempotencyKey: identifier(row["idempotency_key"], storeFailure), activityId: identifier(row["activity_id"], storeFailure) };
}
function copyContext(input: AgentReceiptContext): AgentReceiptContext {
  const idempotencyKey = identifier(input.idempotencyKey, invalidArgument), activityId = identifier(input.activityId, invalidArgument);
  if (activityId === "00".repeat(32)) throw invalidArgument();
  return Object.freeze({ idempotencyKey, activityId });
}
function invalidArgument(): PlatformSdkError { return new PlatformSdkError({ code: "invalid-argument", retry: "never" }); }
function decodeFailure(): PlatformSdkError { return new PlatformSdkError({ code: "decode-failure", retry: "never" }); }
function verificationFailure(): PlatformSdkError { return new PlatformSdkError({ code: "verification-failure", retry: "never" }); }
function unavailable(): PlatformSdkError { return new PlatformSdkError({ code: "unavailable-capability", retry: "safe" }); }
function storeFailure(): PlatformSdkError { return new PlatformSdkError({ code: "internal-fault", retry: "unknown-outcome" }); }
function conflict(): PlatformSdkError { return new PlatformSdkError({ code: "idempotency-conflict", retry: "never" }); }
function record(value: unknown, failure: () => PlatformSdkError): Readonly<Record<string, unknown>> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) throw failure();
  return value as Readonly<Record<string, unknown>>;
}
function exact(value: Readonly<Record<string, unknown>>, fields: readonly string[], failure: () => PlatformSdkError): void {
  if (fields.some((field) => !Object.prototype.hasOwnProperty.call(value, field)) || Object.keys(value).some((field) => !fields.includes(field))) throw failure();
}
function identifier(value: unknown, failure: () => PlatformSdkError): string {
  if (typeof value !== "string" || !/^[0-9a-f]{64}$/u.test(value)) throw failure();
  return value;
}
function boundedText(value: unknown, maximumBytes: number, failure: () => PlatformSdkError): string {
  if (typeof value !== "string" || value.length === 0 || Buffer.byteLength(value) > maximumBytes || value.includes("\0")
    || Buffer.from(value).toString("utf8") !== value) throw failure();
  return value;
}
function hex(value: unknown, maximumBytes: number, exactBytes?: number): string {
  if (typeof value !== "string" || value.length > maximumBytes * 2 || !/^(?:[0-9a-f]{2})+$/u.test(value)
    || exactBytes !== undefined && value.length !== exactBytes * 2) throw decodeFailure();
  return value;
}
