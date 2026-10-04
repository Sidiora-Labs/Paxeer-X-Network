import {
  MiddlewareError,
  VerifiedWebhookConsumer,
  type JsonValue,
  type WebhookClaimResult,
  type WebhookConsumeResult,
  type WebhookDeliveryClaim,
  type WebhookDeliveryStore,
  type WebhookRequestHeaders,
} from "@sidiora/layerx-seller-middleware";
import { constants } from "node:fs";
import { lstat, realpath, open, mkdir, rename, unlink } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { DatabaseSync } from "node:sqlite";
import { AgentIntegrationError, type AgentWebhookSettings } from "./config.js";

export const WEBHOOK_ID_HEADER = "layerx-webhook-id" as const;
export const WEBHOOK_TIMESTAMP_HEADER = "layerx-webhook-timestamp" as const;
export const WEBHOOK_KEY_HEADER = "layerx-webhook-key-id" as const;
export const WEBHOOK_SIGNATURE_HEADER = "layerx-webhook-signature" as const;

export const MAXIMUM_WEBHOOK_BYTES = 1_048_576;

export type WebhookHeaderSource =
  | Headers
  | Readonly<Record<string, string | readonly string[] | undefined>>;

export interface AgentWebhookEvent {
  readonly deliveryId: string;
  readonly event: Readonly<Record<string, JsonValue>>;
}

export interface AgentWebhookHandler {
  handle(event: Readonly<Record<string, JsonValue>>, deliveryId: string): Promise<void>;
}

export interface AgentWebhookResponse {
  readonly status: number;
  readonly headers: Readonly<Record<string, string>>;
  readonly body: string;
}

export class SingleProcessWebhookDeliveryStore implements WebhookDeliveryStore {
  readonly #entries = new Map<string, { payloadDigest: string; leaseUntilMs: number; completed: boolean }>();
  readonly #now: () => number;
  readonly #capacity: number;

  public constructor(now?: () => number, capacity = 16_384) {
    this.#now = now ?? Date.now;
    this.#capacity = Math.max(capacity, 1);
  }

  public claim(value: WebhookDeliveryClaim): Promise<WebhookClaimResult> {
    const existing = this.#entries.get(value.deliveryId);
    if (existing === undefined) {
      this.#evict();
      this.#entries.set(value.deliveryId, {
        payloadDigest: value.payloadDigest,
        leaseUntilMs: value.leaseUntilMs,
        completed: false,
      });
      return Promise.resolve("claimed");
    }
    if (existing.payloadDigest !== value.payloadDigest) {
      return Promise.resolve("conflict");
    }
    if (existing.completed) {
      return Promise.resolve("completed");
    }
    if (existing.leaseUntilMs > this.#now()) {
      return Promise.resolve("processing");
    }
    existing.leaseUntilMs = value.leaseUntilMs;
    return Promise.resolve("claimed");
  }

  public complete(deliveryId: string, payloadDigest: string): Promise<void> {
    const existing = this.#entries.get(deliveryId);
    if (existing === undefined || existing.payloadDigest !== payloadDigest) {
      throw new MiddlewareError("webhook-replay");
    }
    existing.completed = true;
    existing.leaseUntilMs = 0;
    return Promise.resolve();
  }

  public release(deliveryId: string, payloadDigest: string): Promise<void> {
    const existing = this.#entries.get(deliveryId);
    if (existing !== undefined && existing.payloadDigest === payloadDigest && !existing.completed) {
      this.#entries.delete(deliveryId);
    }
    return Promise.resolve();
  }

  #evict(): void {
    if (this.#entries.size < this.#capacity) {
      return;
    }
    const now = this.#now();
    for (const [deliveryId, entry] of this.#entries) {
      if (entry.completed || entry.leaseUntilMs <= now) {
        this.#entries.delete(deliveryId);
      }
    }
    for (const deliveryId of this.#entries.keys()) {
      if (this.#entries.size < this.#capacity) {
        break;
      }
      this.#entries.delete(deliveryId);
    }
  }
}

interface FileDeliveryEntry {
  readonly payloadDigest: string;
  readonly leaseUntilMs: number;
  readonly completed: boolean;
}

interface FileDeliveryLedger {
  readonly version: 1;
  readonly entries: Readonly<Record<string, FileDeliveryEntry>>;
}

const COORDINATION_APPLICATION_ID = 0x4c585748;
const COORDINATION_SCHEMA = "CREATE TABLE delivery_context (singleton INTEGER PRIMARY KEY CHECK (singleton = 1), ledger_path TEXT NOT NULL) STRICT";

export class FileWebhookDeliveryStore implements WebhookDeliveryStore {
  readonly #ledgerPath: string;
  readonly #lockPath: string;
  readonly #now: () => number;
  readonly #capacity: number;

  public constructor(path = ".layerx/webhook-deliveries-v1.json", now?: () => number, capacity = 65_536) {
    if (path.length === 0 || path.length > 4_096 || path.includes("\0")
        || !Number.isSafeInteger(capacity) || capacity < 1) {
      throw new AgentIntegrationError("invalid-declared-key");
    }
    this.#ledgerPath = resolve(path);
    this.#lockPath = `${this.#ledgerPath}.coordination.sqlite3`;
    this.#now = now ?? Date.now;
    this.#capacity = capacity;
  }

  public claim(value: WebhookDeliveryClaim): Promise<WebhookClaimResult> {
    this.#requireClaim(value);
    return this.#mutate((entries) => {
      const existing = entries[value.deliveryId];
      if (existing !== undefined) {
        if (existing.payloadDigest !== value.payloadDigest) return "conflict";
        if (existing.completed) return "completed";
        if (existing.leaseUntilMs > this.#now()) return "processing";
      } else if (Object.keys(entries).length >= this.#capacity) {
        this.#evict(entries);
      }
      if (Object.keys(entries).length >= this.#capacity && existing === undefined) {
        throw new AgentIntegrationError("service-refused");
      }
      entries[value.deliveryId] = {
        payloadDigest: value.payloadDigest,
        leaseUntilMs: value.leaseUntilMs,
        completed: false,
      };
      return "claimed";
    });
  }

  public complete(deliveryId: string, payloadDigest: string): Promise<void> {
    this.#requireIdentifier(deliveryId);
    this.#requireDigest(payloadDigest);
    return this.#mutate((entries) => {
      const existing = entries[deliveryId];
      if (existing === undefined || existing.payloadDigest !== payloadDigest) {
        throw new MiddlewareError("webhook-replay");
      }
      entries[deliveryId] = { payloadDigest, leaseUntilMs: 0, completed: true };
    });
  }

  public release(deliveryId: string, payloadDigest: string): Promise<void> {
    this.#requireIdentifier(deliveryId);
    this.#requireDigest(payloadDigest);
    return this.#mutate((entries) => {
      const existing = entries[deliveryId];
      if (existing !== undefined && existing.payloadDigest === payloadDigest && !existing.completed) {
        delete entries[deliveryId];
      }
    });
  }

  async #mutate<Result>(body: (entries: Record<string, FileDeliveryEntry>) => Result): Promise<Result> {
    await mkdir(dirname(this.#ledgerPath), { recursive: true, mode: 0o700 });
    await secureOwnedPath(dirname(this.#ledgerPath), true);
    if (await realpath(dirname(this.#ledgerPath)) !== dirname(this.#ledgerPath)) {
      throw new AgentIntegrationError("service-refused");
    }
    const lock = await this.#acquireLock();
    try {
      const entries = await this.#read();
      const result = body(entries);
      await this.#write(entries);
      return result;
    } catch (error) {
      if (error instanceof MiddlewareError || error instanceof AgentIntegrationError) throw error;
      throw new AgentIntegrationError("service-refused");
    } finally {
      try { if (lock.isTransaction) lock.exec("ROLLBACK"); }
      finally { lock.close(); }
    }
  }

  async #acquireLock(): Promise<DatabaseSync> {
    try {
      const file = await open(this.#lockPath, "wx", 0o600);
      await file.close();
    } catch (error) {
      if (!isNodeError(error, "EEXIST")) throw new AgentIntegrationError("service-refused");
    }
    await secureOwnedPath(this.#lockPath, false);
    let lock: DatabaseSync | undefined;
    try {
      lock = new DatabaseSync(this.#lockPath, {
        timeout: 0, enableDoubleQuotedStringLiterals: false, allowExtension: false,
      });
      lock.exec("PRAGMA trusted_schema = OFF; PRAGMA synchronous = FULL");
      await beginCoordination(lock);
      const applicationId = coordinationPragma(lock, "application_id");
      const version = coordinationPragma(lock, "user_version");
      const objects = lock.prepare("SELECT type, name FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").all();
      if (applicationId === 0 && version === 0 && objects.length === 0) {
        lock.exec(`${COORDINATION_SCHEMA}; PRAGMA application_id = ${COORDINATION_APPLICATION_ID}; PRAGMA user_version = 1`);
        lock.prepare("INSERT INTO delivery_context (singleton, ledger_path) VALUES (1, ?)").run(this.#ledgerPath);
      } else if (applicationId !== COORDINATION_APPLICATION_ID || version !== 1 || objects.length !== 1
          || objects[0]?.["type"] !== "table" || objects[0]?.["name"] !== "delivery_context") {
        throw new AgentIntegrationError("service-refused");
      }
      const schema = lock.prepare("SELECT sql FROM sqlite_schema WHERE type = 'table' AND name = 'delivery_context'").get();
      const context = lock.prepare("SELECT singleton, ledger_path FROM delivery_context").all();
      if (schema?.["sql"] !== COORDINATION_SCHEMA || context.length !== 1 || context[0]?.["singleton"] !== 1
          || context[0]?.["ledger_path"] !== this.#ledgerPath) throw new AgentIntegrationError("service-refused");
      lock.exec("PRAGMA max_page_count = 64");
      if (coordinationPragma(lock, "max_page_count") !== 64 || coordinationPragma(lock, "synchronous") !== 2) {
        throw new AgentIntegrationError("service-refused");
      }
      lock.exec("COMMIT");
      await beginCoordination(lock);
      for (const path of [this.#lockPath, `${this.#lockPath}-journal`]) {
        try { await secureOwnedPath(path, false); }
        catch (error) { if (!isNodeError(error, "ENOENT")) throw error; }
      }
      return lock;
    } catch (error) {
      try { if (lock?.isTransaction) lock.exec("ROLLBACK"); }
      finally { lock?.close(); }
      if (error instanceof AgentIntegrationError) throw error;
      throw new AgentIntegrationError("service-refused");
    }
  }

  async #read(): Promise<Record<string, FileDeliveryEntry>> {
    let encoded: Uint8Array;
    try {
      encoded = await readBoundedRegularFile(this.#ledgerPath, 32 * 1024 * 1024);
    } catch (error) {
      if (isNodeError(error, "ENOENT")) return {};
      throw error;
    }
    const parsed: unknown = JSON.parse(new TextDecoder().decode(encoded));
    if (!isLedger(parsed) || Object.keys(parsed.entries).length > this.#capacity) {
      throw new AgentIntegrationError("service-refused");
    }
    return { ...parsed.entries };
  }

  async #write(entries: Record<string, FileDeliveryEntry>): Promise<void> {
    const encoded = JSON.stringify({ version: 1, entries } satisfies FileDeliveryLedger);
    if (Buffer.byteLength(encoded, "utf8") > 32 * 1024 * 1024) throw new AgentIntegrationError("service-refused");
    const temporary = `${this.#ledgerPath}.${globalThis.crypto.randomUUID()}.tmp`;
    const handle = await open(temporary, "wx", 0o600);
    try {
      await handle.writeFile(encoded, "utf8");
      await handle.sync();
    } finally {
      await handle.close();
    }
    try {
      await rename(temporary, this.#ledgerPath);
    } catch (error) {
      await unlink(temporary).catch(() => undefined);
      throw error;
    }
    const directory = await open(dirname(this.#ledgerPath), "r");
    try {
      await directory.sync();
    } finally {
      await directory.close();
    }
  }

  #evict(entries: Record<string, FileDeliveryEntry>): void {
    const now = this.#now();
    for (const [deliveryId, entry] of Object.entries(entries)) {
      if (!entry.completed && entry.leaseUntilMs <= now) delete entries[deliveryId];
    }
  }

  #requireClaim(value: WebhookDeliveryClaim): void {
    this.#requireIdentifier(value.deliveryId);
    this.#requireDigest(value.payloadDigest);
    if (!Number.isSafeInteger(value.leaseUntilMs) || value.leaseUntilMs <= 0) {
      throw new AgentIntegrationError("service-refused");
    }
  }

  #requireIdentifier(value: string): void {
    if (value.length === 0 || value.length > 255 || value.includes("\0")) {
      throw new AgentIntegrationError("service-refused");
    }
  }

  #requireDigest(value: string): void {
    if (!/^[0-9a-f]{64}$/u.test(value)) throw new AgentIntegrationError("service-refused");
  }
}

async function beginCoordination(lock: DatabaseSync): Promise<void> {
  for (let attempt = 0; attempt < 200; attempt += 1) {
      try {
        lock.exec("BEGIN IMMEDIATE");
        return;
      } catch (error) {
        if (!(error instanceof Error) || !("errcode" in error) || error.errcode !== 5 && error.errcode !== 6) {
          throw new AgentIntegrationError("service-refused");
        }
        await new Promise<void>((resolveWait) => setTimeout(resolveWait, Math.min(10 + attempt * 5, 250)));
      }
  }
  throw new AgentIntegrationError("service-refused");
}

function coordinationPragma(lock: DatabaseSync, name: "application_id" | "user_version" | "max_page_count" | "synchronous"): number {
  const value = lock.prepare(`PRAGMA ${name}`).get()?.[name];
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) throw new AgentIntegrationError("service-refused");
  return value;
}

async function secureOwnedPath(path: string, directory: boolean): Promise<void> {
  const metadata = await lstat(path);
  if (metadata.isSymbolicLink() || (directory ? !metadata.isDirectory() : !metadata.isFile() || metadata.nlink !== 1)
      || (metadata.mode & 0o077) !== 0 || process.getuid !== undefined && metadata.uid !== process.getuid()) {
    throw new AgentIntegrationError("service-refused");
  }
}

export interface AgentWebhookGatewayConfig {
  readonly webhook: AgentWebhookSettings;
  readonly deliveries?: WebhookDeliveryStore;
  readonly deliveryStorePath?: string;
  readonly now?: () => number;
}

export class AgentWebhookGateway {
  readonly #consumer: VerifiedWebhookConsumer;
  readonly #deliveries: WebhookDeliveryStore;

  public constructor(config: AgentWebhookGatewayConfig) {
    this.#deliveries = config.deliveries
      ?? new FileWebhookDeliveryStore(config.deliveryStorePath, config.now);
    this.#consumer = new VerifiedWebhookConsumer({
      publicKeys: config.webhook.publicKeys,
      deliveries: this.#deliveries,
      maximumAgeMs: config.webhook.maximumAgeMs,
      leaseMs: config.webhook.leaseMs,
      ...(config.now === undefined ? {} : { now: config.now }),
    });
  }

  public get consumer(): VerifiedWebhookConsumer {
    return this.#consumer;
  }

  public get deliveries(): WebhookDeliveryStore {
    return this.#deliveries;
  }

  public consume(
    rawBody: Uint8Array,
    headers: WebhookHeaderSource,
    handler: AgentWebhookHandler,
  ): Promise<WebhookConsumeResult> {
    if (rawBody.length > MAXIMUM_WEBHOOK_BYTES) {
      throw new MiddlewareError("invalid-webhook");
    }
    return this.#consumer.consume(
      rawBody,
      webhookHeaders(headers),
      (event, deliveryId) => handler.handle(event, deliveryId),
    );
  }

  public async respond(
    rawBody: Uint8Array,
    headers: WebhookHeaderSource,
    handler: AgentWebhookHandler,
  ): Promise<AgentWebhookResponse> {
    try {
      const outcome = await this.consume(rawBody, headers, handler);
      if (outcome === "processed") {
        return json(200, { outcome });
      }
      if (outcome === "duplicate") {
        return json(200, { outcome });
      }
      return { status: 409, headers: { "content-type": "application/json", "retry-after": "1" }, body: JSON.stringify({ outcome }) };
    } catch (error) {
      if (error instanceof MiddlewareError && error.code === "invalid-webhook") {
        return json(401, { error: error.code });
      }
      if (error instanceof MiddlewareError && error.code === "webhook-replay") {
        return json(409, { error: error.code });
      }
      if (error instanceof AgentIntegrationError) {
        return json(400, { error: error.code });
      }
      throw error;
    }
  }
}

export function webhookHeaders(source: WebhookHeaderSource): WebhookRequestHeaders {
  const id = singleHeader(source, WEBHOOK_ID_HEADER);
  const timestamp = singleHeader(source, WEBHOOK_TIMESTAMP_HEADER);
  const keyId = singleHeader(source, WEBHOOK_KEY_HEADER);
  const signature = singleHeader(source, WEBHOOK_SIGNATURE_HEADER);
  if (id === undefined || timestamp === undefined || keyId === undefined || signature === undefined) {
    throw new MiddlewareError("invalid-webhook");
  }
  return { id, timestamp, keyId, signature };
}

function singleHeader(source: WebhookHeaderSource, name: string): string | undefined {
  if (source instanceof Headers) {
    const value = source.get(name);
    return value === null ? undefined : value;
  }
  let found: string | undefined;
  for (const [key, value] of Object.entries(source)) {
    if (key.toLowerCase() !== name) continue;
    if (typeof value !== "string" || found !== undefined) {
      throw new AgentIntegrationError("duplicate-header");
    }
    found = value;
  }
  return found;
}

function json(status: number, body: Readonly<Record<string, string>>): AgentWebhookResponse {
  return { status, headers: { "content-type": "application/json" }, body: JSON.stringify(body) };
}

function isNodeError(error: unknown, code: string): boolean {
  return error instanceof Error && "code" in error && (error as Error & { readonly code: unknown }).code === code;
}

async function readBoundedRegularFile(path: string, maximum: number): Promise<Uint8Array> {
  const handle = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const metadata = await handle.stat();
    if (!metadata.isFile() || metadata.nlink !== 1 || (metadata.mode & 0o077) !== 0
        || process.getuid !== undefined && metadata.uid !== process.getuid()
        || metadata.size > maximum) throw new AgentIntegrationError("service-refused");
    const chunks: Uint8Array[] = [];
    let total = 0;
    for (;;) {
      const chunk = new Uint8Array(Math.min(64 * 1024, maximum + 1 - total));
      const { bytesRead } = await handle.read(chunk, 0, chunk.length, null);
      if (bytesRead === 0) break;
      total += bytesRead;
      if (total > maximum) throw new AgentIntegrationError("service-refused");
      chunks.push(chunk.subarray(0, bytesRead));
    }
    const encoded = new Uint8Array(total);
    let offset = 0;
    for (const chunk of chunks) {
      encoded.set(chunk, offset);
      offset += chunk.length;
    }
    return encoded;
  } finally {
    await handle.close();
  }
}

function isLedger(value: unknown): value is FileDeliveryLedger {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return false;
  const ledger = value as Readonly<Record<string, unknown>>;
  if (Object.keys(ledger).length !== 2 || ledger["version"] !== 1 || ledger["entries"] === null
      || typeof ledger["entries"] !== "object" || Array.isArray(ledger["entries"])) return false;
  for (const [deliveryId, untrusted] of Object.entries(ledger["entries"] as Record<string, unknown>)) {
    if (deliveryId.length === 0 || deliveryId.length > 255 || deliveryId.includes("\0") || untrusted === null
        || typeof untrusted !== "object" || Array.isArray(untrusted)) return false;
    const entry = untrusted as Readonly<Record<string, unknown>>;
    if (Object.keys(entry).length !== 3
        || typeof entry["payloadDigest"] !== "string" || !/^[0-9a-f]{64}$/u.test(entry["payloadDigest"])
        || typeof entry["leaseUntilMs"] !== "number" || !Number.isSafeInteger(entry["leaseUntilMs"])
        || entry["leaseUntilMs"] < 0 || typeof entry["completed"] !== "boolean"
        || entry["completed"] && entry["leaseUntilMs"] !== 0) return false;
  }
  return true;
}
