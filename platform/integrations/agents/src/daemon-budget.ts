import { closeSync, existsSync, lstatSync, mkdirSync, openSync, realpathSync } from "node:fs";
import { dirname, isAbsolute, resolve } from "node:path";
import { DatabaseSync } from "node:sqlite";
import {
  decodeDaemonPrepared, decodeDaemonSubmission, signedActivityId, verifyAgentPayment,
  type AgentPreparationBudget, type AgentSpendRequest, type OwnerBudgetSpendResult,
} from "@sidiora/layerx-agent-middleware";
import { AgentEnvelopeTransport, PlatformSdkError, idempotencyKey, decodeNativePrepareResult,
  type NativePrepareResultV1 } from "@sidiora/layerx-sdk";
import { DaemonReceiptResolver, type DaemonReceiptPrincipal } from "./daemon-providers.js";

type Services = Parameters<AgentPreparationBudget["spendPrepared"]>[2];
type Stage = "preparing" | "prepared" | "signing" | "signed";
interface Retained {
  requestDigest: string;
  preparationId: string;
  result: NativePrepareResultV1 | null;
  stage: Stage;
  activityId: string | null;
  submissionRef: string | null;
}
export interface DaemonPreparationBudgetOptions {
  readonly transport: AgentEnvelopeTransport;
  readonly principal: DaemonReceiptPrincipal;
  readonly storePath: string;
}
const SCHEMA = "CREATE TABLE admissions (preparation_id TEXT PRIMARY KEY, request_digest TEXT NOT NULL UNIQUE, record TEXT NOT NULL) STRICT";
const CONTEXT = "CREATE TABLE principal (singleton INTEGER PRIMARY KEY CHECK (singleton = 1), tenant TEXT NOT NULL, actor TEXT NOT NULL, session_id TEXT NOT NULL) STRICT";
const APP_ID = 0x4c584250;

export class DaemonPreparationBudget implements AgentPreparationBudget {
  readonly #transport: AgentEnvelopeTransport;
  readonly #principal: DaemonReceiptPrincipal;
  readonly #path: string;
  readonly #db: DatabaseSync;
  #closed = false;

  public constructor(options: DaemonPreparationBudgetOptions) {
    if (!(options.transport instanceof AgentEnvelopeTransport) || !isAbsolute(options.storePath)
      || options.storePath.includes("\0") || options.storePath.length > 4096) throw refusal();
    for (const value of [options.principal.tenant, options.principal.actor]) {
      if (typeof value !== "string" || !value || Buffer.byteLength(value) > 255 || value.includes("\0")) throw refusal();
    }
    identifier(options.principal.sessionId);
    this.#transport = options.transport;
    this.#principal = Object.freeze({ ...options.principal });
    this.#path = resolve(options.storePath);
    let db: DatabaseSync | undefined;
    try {
      const parent = dirname(this.#path);
      mkdirSync(parent, { recursive: true, mode: 0o700 });
      secure(parent, true);
      if (realpathSync(parent) !== parent) throw refusal();
      if (!existsSync(this.#path)) {
        try { closeSync(openSync(this.#path, "wx", 0o600)); }
        catch (error) { if ((error as NodeJS.ErrnoException).code !== "EEXIST") throw error; }
      }
      secure(this.#path, false);
      db = new DatabaseSync(this.#path, { timeout: 5000, enableForeignKeyConstraints: true,
        enableDoubleQuotedStringLiterals: false, allowExtension: false });
      db.exec("PRAGMA trusted_schema = OFF; BEGIN IMMEDIATE");
      const objects = db.prepare("SELECT name FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").all();
      if (pragma(db, "application_id") === 0 && pragma(db, "user_version") === 0 && objects.length === 0) {
        db.exec(`${SCHEMA}; ${CONTEXT}; PRAGMA application_id = ${APP_ID}; PRAGMA user_version = 1`);
        db.prepare("INSERT INTO principal VALUES (1, ?, ?, ?)").run(this.#principal.tenant, this.#principal.actor, this.#principal.sessionId);
      } else if (pragma(db, "application_id") !== APP_ID || pragma(db, "user_version") !== 1 || objects.length !== 2) throw refusal();
      for (const [name, sql] of [["admissions", SCHEMA], ["principal", CONTEXT]] as const) {
        if (db.prepare("SELECT sql FROM sqlite_schema WHERE name = ? AND type = 'table'").get(name)?.["sql"] !== sql) throw refusal();
      }
      principal(db, this.#principal);
      db.exec("COMMIT; PRAGMA journal_mode = WAL; PRAGMA synchronous = FULL; PRAGMA temp_store = MEMORY; PRAGMA wal_autocheckpoint = 100; PRAGMA journal_size_limit = 8388608");
      const pageSize = pragma(db, "page_size");
      if (pageSize < 512 || pageSize > 65536 || (pageSize & (pageSize - 1)) !== 0) throw refusal();
      const pages = Math.floor(67108864 / pageSize);
      db.exec(`PRAGMA max_page_count = ${pages}`);
      if (pragma(db, "max_page_count") !== pages || pragma(db, "synchronous") !== 2) throw refusal();
      const checked = db.prepare("PRAGMA quick_check").all();
      if (checked.length !== 1 || checked[0]?.["quick_check"] !== "ok") throw refusal();
      secureFiles(this.#path);
      this.#db = db;
    } catch {
      db?.close();
      throw refusal();
    }
  }

  public close(): void { if (!this.#closed) { this.#db.close(); this.#closed = true; } }

  public async spendPrepared(request: AgentSpendRequest, requestDigest: string, services: Services): Promise<OwnerBudgetSpendResult> {
    const p = request.preparation;
    if (!("variant" in p) || request.tenant !== this.#principal.tenant || p.actor !== this.#principal.actor
      || p.purpose.purpose.session_id !== this.#principal.sessionId || !(services.receipts instanceof DaemonReceiptResolver)) throw refusal();
    identifier(requestDigest);
    const preparationId = identifier(p.purpose.purpose.preparation_id);
    let admissionObserved = false;
    const outcome = (state: OwnerBudgetSpendResult["state"], extra: Omit<Partial<OwnerBudgetSpendResult>, "kind" | "state" | "preparationId" | "admissionObserved"> = {}): OwnerBudgetSpendResult =>
      ({ kind: "owner-budget", preparationId, admissionObserved, state, ...extra });
    let retained: Retained;
    let created: boolean;
    try { ({ retained, created } = this.#admit(preparationId, requestDigest)); }
    catch (error) { if (error instanceof PlatformSdkError && error.code === "idempotency-conflict") throw error; return outcome("unknown"); }
    if (created) {
      try {
        const response = await this.#transport.prepareNative(p, idempotencyKey(p.idempotency_key));
        decodeDaemonPrepared(response.value, request, services.protocolVersion);
        const next: Retained = { ...retained, result: response.value, stage: "prepared" };
        this.#replace(retained, next); retained = next;
      } catch { return outcome("admission-unknown"); }
    }
    if (retained.result === null) return outcome("admission-unknown");
    let prepared;
    try { prepared = decodeDaemonPrepared(retained.result, request, services.protocolVersion); admissionObserved = true; }
    catch { return outcome("unknown"); }
    let approval;
    try {
      approval = (await this.#transport.approvalGetNative(preparationId)).value;
      if (approval.approval_id !== preparationId || approval.activity.module !== p.activity.module || approval.activity.ordinal !== p.activity.ordinal) throw refusal();
      if (approval.state === "Rejected" || approval.state === "Expired") {
        if (retained.stage === "signing" || retained.stage === "signed") return outcome("unknown");
        return outcome(approval.state === "Rejected" ? "owner-rejected" : "owner-expired", {
          approval: { approvalId: preparationId, heldDigest: approval.held_digest, state: approval.state },
        });
      }
      if (approval.state === "Defective") return outcome("unknown");
      if (approval.state === "Awaiting") return outcome("approval", {
        approval: { approvalId: preparationId, heldDigest: approval.held_digest, state: approval.state },
      });
      if (approval.state === "Granted" && approval.submission_ref !== request.approvalReleaseRef) return outcome("unknown");
      if (approval.state === "NotRequired" && approval.submission_ref !== null) throw refusal();
    } catch { return outcome("unknown"); }
    if (retained.stage === "signing") return outcome("unknown");
    if (retained.stage === "prepared") {
      try {
        const signing: Retained = { ...retained, stage: "signing" };
        this.#replace(retained, signing); retained = signing;
        const signature = await services.signer.sign(prepared);
        const activityId = signedActivityId(prepared, signature, request.signerPublicKey);
        const signed: Retained = { ...retained, stage: "signed", activityId };
        this.#replace(retained, signed); retained = signed;
        const response = await this.#transport.call<unknown, { value: unknown }>({ plane: "agent", operation: "submit", request: {
          preparation_ref: prepared.preparation_ref, signature, signer_public_key: request.signerPublicKey,
          approval_release_ref: request.approvalReleaseRef ?? null,
        }, idempotencyKey: idempotencyKey(request.submitIdempotencyKey) });
        const submission = decodeDaemonSubmission(response.value, activityId);
        const submitted = { ...retained, submissionRef: submission.submission_ref };
        this.#replace(retained, submitted); retained = submitted;
      } catch { return outcome("unknown"); }
    }
    if (retained.activityId === null) return outcome("unknown");
    try {
      let submission;
      if (retained.submissionRef !== null) {
        const response = await this.#transport.call<unknown, { value: unknown }>({ plane: "agent", operation: "track", request: { submission_ref: retained.submissionRef } });
        submission = decodeDaemonSubmission(response.value, retained.activityId, retained.submissionRef);
        if (submission.state === "Unknown" || submission.state === "Failed" || submission.state === "Expired") return outcome("unknown", { submission });
        if (submission.state !== "Executed") return outcome("pending", { submission });
      }
      const evidence = await services.receipts.resolveActivity({ idempotencyKey: p.idempotency_key, activityId: retained.activityId });
      const verification = await verifyAgentPayment(evidence, request, services.commitments, { protocolVersion: services.protocolVersion });
      if (Buffer.from(verification.receipt.activityId).toString("hex") !== retained.activityId) throw refusal();
      await services.receipts.retainVerified({ idempotencyKey: p.idempotency_key, activityId: retained.activityId,
        receiptDigest: Buffer.from(verification.receiptDigest).toString("hex"),
        ...(submission?.receipt_ref === undefined ? {} : { receiptRef: submission.receipt_ref }),
      });
      return outcome("settled", { verification, ...(submission === undefined ? {} : { submission }) });
    } catch { return outcome("unknown"); }
  }

  #admit(preparationId: string, requestDigest: string): { retained: Retained; created: boolean } {
    return this.#transaction(() => {
      const rows = this.#db.prepare("SELECT record FROM admissions WHERE preparation_id = ? OR request_digest = ?").all(preparationId, requestDigest);
      if (rows.length > 1) throw conflict();
      if (rows.length === 1) {
        const retained = decode(rows[0]?.["record"]);
        if (retained.preparationId !== preparationId || retained.requestDigest !== requestDigest) throw conflict();
        return { retained, created: false };
      }
      const count = this.#db.prepare("SELECT COUNT(*) AS n FROM admissions").get()?.["n"];
      if (typeof count !== "number" || count >= 4096) throw refusal();
      const retained: Retained = { preparationId, requestDigest, stage: "preparing", result: null, activityId: null, submissionRef: null };
      this.#db.prepare("INSERT INTO admissions VALUES (?, ?, ?)").run(preparationId, requestDigest, JSON.stringify(retained));
      return { retained, created: true };
    });
  }
  #replace(previous: Retained, next: Retained): void {
    this.#transaction(() => {
      const changed = this.#db.prepare("UPDATE admissions SET record = ? WHERE preparation_id = ? AND request_digest = ? AND record = ?")
        .run(JSON.stringify(next), previous.preparationId, previous.requestDigest, JSON.stringify(previous));
      if (changed.changes !== 1) throw conflict();
    });
  }
  #transaction<T>(body: () => T): T {
    if (this.#closed) throw refusal();
    try {
      secureFiles(this.#path); this.#db.exec("BEGIN IMMEDIATE"); principal(this.#db, this.#principal);
      const answer = body(); secureFiles(this.#path); this.#db.exec("COMMIT"); return answer;
    } catch (error) {
      try { if (this.#db.isTransaction) this.#db.exec("ROLLBACK"); } catch { throw refusal(); }
      if (error instanceof PlatformSdkError) throw error;
      throw refusal();
    }
  }
}
function decode(value: unknown): Retained {
  if (typeof value !== "string" || Buffer.byteLength(value) > 4194304) throw refusal();
  const row = JSON.parse(value) as Retained;
  if (!row || typeof row !== "object" || Object.keys(row).sort().join() !== ["requestDigest", "preparationId", "result", "stage", "activityId", "submissionRef"].sort().join()) throw refusal();
  identifier(row.requestDigest); identifier(row.preparationId);
  if (!["preparing", "prepared", "signing", "signed"].includes(row.stage)) throw refusal();
  if ((row.stage === "preparing") !== (row.result === null)) throw refusal();
  if (row.result !== null) { row.result = decodeNativePrepareResult(row.result); if (row.result.preparation_id !== row.preparationId) throw refusal(); }
  if (row.activityId !== null) identifier(row.activityId);
  if ((row.stage === "signed") !== (row.activityId !== null)) throw refusal();
  if (row.submissionRef !== null && (row.stage !== "signed" || typeof row.submissionRef !== "string" || !row.submissionRef || Buffer.byteLength(row.submissionRef) > 255)) throw refusal();
  return row;
}
function principal(db: DatabaseSync, expected: DaemonReceiptPrincipal): void {
  const rows = db.prepare("SELECT * FROM principal").all(), row = rows[0];
  if (rows.length !== 1 || row === undefined || row["singleton"] !== 1 || row["tenant"] !== expected.tenant || row["actor"] !== expected.actor || row["session_id"] !== expected.sessionId) throw refusal();
}
function pragma(db: DatabaseSync, name: string): number {
  const n = db.prepare(`PRAGMA ${name}`).get()?.[name];
  if (typeof n !== "number" || !Number.isSafeInteger(n) || n < 0) throw refusal(); return n;
}
function secure(path: string, directory: boolean): void {
  const s = lstatSync(path);
  if (s.isSymbolicLink() || (directory ? !s.isDirectory() : !s.isFile() || s.nlink !== 1)
    || (s.mode & 0o077) !== 0 || process.getuid !== undefined && s.uid !== process.getuid()) throw refusal();
}
function secureFiles(path: string): void { for (const p of [path, `${path}-wal`, `${path}-shm`]) if (existsSync(p)) secure(p, false); }
function identifier(value: string): string { if (typeof value !== "string" || !/^[0-9a-f]{64}$/u.test(value)) throw refusal(); return value; }
function refusal(): PlatformSdkError { return new PlatformSdkError({ code: "unavailable-capability", retry: "unknown-outcome" }); }
function conflict(): PlatformSdkError { return new PlatformSdkError({ code: "idempotency-conflict", retry: "never" }); }
