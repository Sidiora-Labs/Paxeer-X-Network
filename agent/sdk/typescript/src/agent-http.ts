import { randomBytes } from "node:crypto";
import * as http from "node:http";
import * as https from "node:https";
import { bindSignedProgramLifecycle } from "./program-wire.js";

import type { Operation as AgentOperation } from "./generated/client.js";
import {
  PlatformSdkError,
  SecretBytes,
  type ProductionTransport,
  type SdkErrorCode,
  type TransportCall,
} from "./production.js";

const MAX_RESPONSE_BYTES = 8 * 1024 * 1024;
const MAX_REQUEST_BYTES = 4 * 1024 * 1024;
const DEFAULT_TIMEOUT_MS = 30_000;
const HEX32 = /^[0-9a-f]{64}$/u;
const KEY_ID = /^[A-Za-z0-9_-]{1,64}$/u;

interface AgentRoute {
  readonly method: "GET" | "POST";
  readonly path: string;
  readonly pathField?: "program_id" | "idempotency_key" | "activity_id";
}

const PROGRAM_ROUTES: Readonly<Partial<Record<AgentOperation, AgentRoute>>> = Object.freeze({
  "program.deploy": Object.freeze({ method: "POST", path: "/v1/programs/deploy" }),
  "program.upgrade": Object.freeze({ method: "POST", path: "/v1/programs/upgrade" }),
  "program.wind-down": Object.freeze({ method: "POST", path: "/v1/programs/wind-down" }),
  "program.discover": Object.freeze({ method: "GET", path: "/v1/programs/registry/{program_id}", pathField: "program_id" }),
  "program.interface": Object.freeze({ method: "GET", path: "/v1/programs/registry/{program_id}/interface", pathField: "program_id" }),
  "program.simulate": Object.freeze({ method: "POST", path: "/v1/programs/simulate" }),
  "program.call": Object.freeze({ method: "POST", path: "/v1/programs/call" }),
  "program.receipt": Object.freeze({ method: "GET", path: "/v1/programs/receipts/by-idempotency/{idempotency_key}", pathField: "idempotency_key" }),
  "program.activity": Object.freeze({ method: "GET", path: "/v1/programs/activities/{activity_id}", pathField: "activity_id" }),
});

const ERROR_CLASS: Readonly<Record<string, SdkErrorCode>> = Object.freeze({
  TransportFailure: "transport-failure",
  Deadline: "deadline",
  ProtocolIncompatibility: "protocol-incompatibility",
  UnavailableCapability: "unavailable-capability",
  CoreRejection: "core-rejection",
  VerificationFailure: "verification-failure",
  PolicyRefusal: "policy-refusal",
  CapabilityRefusal: "capability-refusal",
  BudgetRefusal: "budget-refusal",
  RateLimit: "rate-limit",
  IdempotencyConflict: "idempotency-conflict",
  InternalFault: "internal-fault",
});

export class LayerXKeyCredential {
  public constructor(
    private readonly keyId: string,
    private readonly secret: SecretBytes,
  ) {
    if (!KEY_ID.test(keyId)) throw invalidArgument();
  }

  public use<T>(consumer: (authorization: string) => T): T {
    return this.secret.withBytes((bytes) => {
      let value: string;
      try { value = new TextDecoder("utf-8", { fatal: true }).decode(bytes); }
      catch { throw invalidArgument(); }
      if (!/^lxp_live_[0-9a-f]{64}$/u.test(value)) throw invalidArgument();
      return consumer(`LayerX-Key ${this.keyId}:${value}`);
    });
  }

  public toString(): string { return "[REDACTED]"; }
  public toJSON(): string { return "[REDACTED]"; }
}

export interface AgentHttpTransportOptions {
  readonly endpoint: URL | string;
  readonly credential?: LayerXKeyCredential;
  readonly timeoutMs?: number;
  readonly maximumResponseBytes?: number;
}

/** Exact HTTP transport for the six schema-routed Programs operations. */
export class AgentHttpTransport implements ProductionTransport {
  readonly #endpoint: URL;
  readonly #credential: LayerXKeyCredential | undefined;
  readonly #timeoutMs: number;
  readonly #maximumResponseBytes: number;

  public constructor(options: AgentHttpTransportOptions) {
    this.#endpoint = validateEndpoint(options.endpoint);
    this.#credential = options.credential;
    this.#timeoutMs = exactPositive(options.timeoutMs ?? DEFAULT_TIMEOUT_MS);
    this.#maximumResponseBytes = exactPositive(options.maximumResponseBytes ?? MAX_RESPONSE_BYTES);
    if (this.#maximumResponseBytes > MAX_RESPONSE_BYTES) throw invalidArgument();
  }

  public async call<TRequest, TResponse>(call: TransportCall<TRequest>): Promise<TResponse> {
    if (call.plane !== "agent") throw unavailableCapability();
    const route = PROGRAM_ROUTES[call.operation as AgentOperation];
    if (route === undefined) throw unavailableCapability();
    const request = record(call.request);
    const path = route.pathField === undefined
      ? route.path
      : route.path.replace(`{${route.pathField}}`, encodeURIComponent(hex32Field(request, route.pathField)));
    if (isMutation(call.operation)) {
      if (call.idempotencyKey === undefined || !HEX32.test(call.idempotencyKey)) throw invalidArgument();
    } else if (call.idempotencyKey !== undefined) {
      throw invalidArgument();
    }
    requireRequestedVerification(call.operation as AgentOperation, request);
    requireExactRequest(call.operation as AgentOperation, request);
    let body: Buffer;
    try {
      if (route.method === "POST") {
        body = encodeProgramMutationBody(request.signed_activity);
      } else body = Buffer.from(JSON.stringify(request), "utf8");
    }
    catch { throw invalidArgument(); }
    if (body.length > MAX_REQUEST_BYTES) throw invalidArgument();
    if (isLifecycle(call.operation)) {
      const ordinal = call.operation === "program.deploy" ? 1 : call.operation === "program.upgrade" ? 2 : 7;
      try { await bindSignedProgramLifecycle(body, undefined, ordinal, call.idempotencyKey); }
      catch { throw invalidArgument(); }
    }
    const endpoint = routeEndpoint(this.#endpoint, path);
    const headers: http.OutgoingHttpHeaders = {
      Accept: "application/json",
      "Content-Type": route.method === "POST" ? "application/octet-stream" : "application/json",
      "Content-Length": body.length,
      "User-Agent": "layerx-typescript/0.1.0",
    };
    if (call.idempotencyKey !== undefined) headers["Idempotency-Key"] = call.idempotencyKey;
    if (this.#credential !== undefined) {
      this.#credential.use((authorization) => { headers.Authorization = authorization; });
    }
    return await this.dispatch<TResponse>(endpoint, route.method, headers, body, call.operation as AgentOperation);
  }

  private dispatch<TResponse>(
    endpoint: URL,
    method: "GET" | "POST",
    headers: http.OutgoingHttpHeaders,
    body: Buffer,
    operation: AgentOperation,
  ): Promise<TResponse> {
    return new Promise<TResponse>((resolve, reject) => {
      const driver = endpoint.protocol === "https:" ? https : http;
      let settled = false;
      const request = driver.request(endpoint, { method, headers, timeout: this.#timeoutMs }, (response) => {
        const chunks: Buffer[] = [];
        let received = 0;
        response.on("data", (chunk: Buffer) => {
          received += chunk.length;
          if (received > this.#maximumResponseBytes) {
            response.destroy();
            finish(reject, decodeFailure());
            return;
          }
          chunks.push(Buffer.from(chunk));
        });
        response.on("end", () => {
          if (settled) return;
          try {
            if (response.headers["content-type"] !== "application/json") throw decodeFailure();
            const value = decodeEnvelope(response.statusCode ?? 0, Buffer.concat(chunks), operation);
            finish(resolve, value as TResponse);
          } catch (error) {
            finish(reject, error);
          }
        });
        response.on("error", () => finish(reject, transportFailure(operation)));
      });
      const finish = <T>(callback: (value: T) => void, value: T): void => {
        if (settled) return;
        settled = true;
        callback(value);
      };
      request.on("timeout", () => request.destroy());
      request.on("error", () => finish(reject, transportFailure(operation)));
      request.end(body);
    });
  }
}

export function encodeProgramMutationBody(signed: unknown): Buffer {
  if (typeof signed !== "string" || !/^(?:[0-9a-f]{2})+$/u.test(signed) || signed.length > 2_097_152) throw invalidArgument();
  return Buffer.from(signed, "hex");
}

function record(value: unknown): Readonly<Record<string, unknown>> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) throw invalidArgument();
  return value as Readonly<Record<string, unknown>>;
}

function hex32Field(value: Readonly<Record<string, unknown>>, field: string): string {
  const candidate = value[field];
  if (typeof candidate !== "string" || !HEX32.test(candidate)) throw invalidArgument();
  return candidate;
}

function requireRequestedVerification(operation: AgentOperation, request: Readonly<Record<string, unknown>>): void {
  if (operation === "program.discover" || operation === "program.interface"
    || operation === "program.receipt" || operation === "program.activity") {
    if (request.requested_verification_level !== "sequencer-signed") throw invalidArgument();
  }
}

function requireExactRequest(operation: AgentOperation, request: Readonly<Record<string, unknown>>): void {
  const fields: Readonly<Partial<Record<AgentOperation, readonly string[]>>> = {
    "program.deploy": ["signed_activity"],
    "program.upgrade": ["signed_activity"],
    "program.wind-down": ["signed_activity"],
    "program.discover": ["program_id", "requested_verification_level"],
    "program.interface": ["program_id", "requested_verification_level"],
    "program.simulate": ["program_id", "calldata", "budget", "capabilities", "signed_activity"],
    "program.call": ["program_id", "calldata", "budget", "capabilities", "signed_activity"],
    "program.receipt": ["idempotency_key", "expected_activity_id", "requested_verification_level"],
    "program.activity": ["activity_id", "requested_verification_level"],
  };
  const expected = (operation === "program.call" || operation === "program.simulate") && request.payload_encoding === "native-v1"
    ? ["payload_encoding", "program_id", "calldata", "budget", "signed_activity", "native_call"] : fields[operation];
  if (expected === undefined || Object.keys(request).length !== expected.length || expected.some((field) => !(field in request))) throw invalidArgument();
}

function validateEndpoint(value: URL | string): URL {
  let endpoint: URL;
  try { endpoint = new URL(value); } catch { throw invalidArgument(); }
  if ((endpoint.protocol !== "https:" && endpoint.protocol !== "http:")
    || endpoint.username !== "" || endpoint.password !== "" || endpoint.search !== "" || endpoint.hash !== "") {
    throw invalidArgument();
  }
  if (endpoint.protocol === "http:" && !isLoopback(endpoint.hostname)) throw invalidArgument();
  return endpoint;
}

function isLoopback(hostname: string): boolean {
  const host = hostname.toLowerCase();
  return host === "localhost" || host === "::1" || host === "[::1]" || /^127(?:\.[0-9]{1,3}){3}$/u.test(host);
}

function routeEndpoint(base: URL, path: string): URL {
  const endpoint = new URL(base.toString());
  endpoint.pathname = `${endpoint.pathname.replace(/\/+$/u, "")}${path}`;
  return endpoint;
}

function exactPositive(value: number): number {
  if (!Number.isSafeInteger(value) || value <= 0) throw invalidArgument();
  return value;
}

export function decodeEnvelope(status: number, encoded: Buffer, operation: AgentOperation): unknown {
  let envelope: Readonly<Record<string, unknown>>;
  try { envelope = record(JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(encoded)) as unknown); }
  catch { throw decodeFailure(); }
  if ("class" in envelope) {
    exactKeys(envelope, ["class", "protocol_result_code", "retriability", "reason", "request_id"]);
    throw serviceError(status, envelope);
  }
  if (isLifecycle(operation) || operation === "program.receipt" && ("result" in envelope || "error" in envelope)) {
    if ("error" in envelope) {
      exactKeys(envelope, ["error"]);
      throw decodeProgramBoundaryError(status, envelope.error);
    }
    if (status === 202 && envelope.state === "unknown") return envelope;
    exactKeys(envelope, ["result"]);
    if (status < 200 || status >= 300) throw decodeFailure();
    return envelope.result;
  }
  exactKeys(envelope, ["request_id", "value", "verification_status"]);
  const requestId = envelope.request_id;
  if (status < 200 || status >= 300 || !validRequestId(requestId) || !("value" in envelope)) {
    throw decodeFailure(typeof requestId === "string" ? requestId : undefined);
  }
  if (!acceptedProgramVerification(operation, envelope.value, envelope.verification_status)) throw new PlatformSdkError({ code: "verification-failure", retry: "never", requestId });
  return envelope.value;
}

function acceptedProgramVerification(operation: AgentOperation, result: unknown, value: unknown): boolean {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return false;
  const status = value as Readonly<Record<string, unknown>>;
  const resultState = result !== null && typeof result === "object" && !Array.isArray(result)
    ? (result as Readonly<Record<string, unknown>>).state : undefined;
  if (operation === "program.discover" || operation === "program.interface") {
    return exactUnverified(status, "server_side_receipt_verification_only");
  }
  if ((operation === "program.call" || operation === "program.receipt" || operation === "program.activity")
    && (resultState === "unknown" || resultState === "pending")) {
    return exactUnverified(status, "receipt_pending");
  }
  return Object.keys(status).length === 2 && status.state === "Achieved" && status.level === "SequencerSigned";
}

function exactUnverified(value: Readonly<Record<string, unknown>>, reason: string): boolean {
  return Object.keys(value).length === 4 && value.state === "Unverified" && value.requested === "SequencerSigned"
    && value.achieved === "Unverified" && value.reason === reason;
}

function serviceError(status: number, error: Readonly<Record<string, unknown>>): PlatformSdkError {
  const requestId = validRequestId(error.request_id) ? error.request_id : undefined;
  const code = typeof error.class === "string" ? ERROR_CLASS[error.class] : undefined;
  const retriability = error.retriability;
  const reason = error.reason;
  const protocol = error.protocol_result_code;
  if (status >= 200 && status < 300 || requestId === undefined || code === undefined
    || (retriability !== "Terminal" && retriability !== "Retriable")
    || typeof reason !== "string" || !/^[a-z0-9_.]+$/u.test(reason)
    || (protocol !== null && (!Number.isSafeInteger(protocol) || typeof protocol !== "number"))) {
    throw decodeFailure(requestId);
  }
  return new PlatformSdkError({
    code,
    retry: retriability === "Retriable" ? "safe" : "never",
    requestId,
    ...(protocol === null ? {} : { protocolResultCode: protocol as number }),
  });
}

function exactKeys(value: Readonly<Record<string, unknown>>, required: readonly string[]): void {
  if (Object.keys(value).length !== required.length || required.some((key) => !(key in value))) throw decodeFailure();
}

function validRequestId(value: unknown): value is string {
  return typeof value === "string" && value.length > 0 && value.length <= 128 && /^[\x21-\x7e]+$/u.test(value);
}

function transportFailure(operation: AgentOperation): PlatformSdkError {
  return new PlatformSdkError({
    code: isMutation(operation) ? "unknown-outcome" : "transport-failure",
    retry: isMutation(operation) ? "unknown-outcome" : "safe",
  });
}

function isLifecycle(operation: string): boolean { return operation === "program.deploy" || operation === "program.upgrade" || operation === "program.wind-down"; }
function isMutation(operation: string): boolean { return operation === "program.call" || isLifecycle(operation); }

export class ProgramBoundaryError extends PlatformSdkError {
  constructor(readonly status: number, readonly boundaryCode: string, retryAfterMs?: number) {
    super({ code: status === 409 ? "idempotency-conflict" : status === 429 ? "rate-limit" : "core-rejection",
      retry: retryAfterMs === undefined ? "never" : "safe", ...(retryAfterMs === undefined ? {} : { retryAfterMs }) });
  }
}

export function decodeProgramBoundaryError(status: number, value: unknown): ProgramBoundaryError {
  if (!Number.isInteger(status) || status < 400 || status >= 600 || value === null || typeof value !== "object" || Array.isArray(value)) throw decodeFailure();
  const error = value as Readonly<Record<string, unknown>>;
  if (typeof error.code !== "string" || !/^[a-z0-9_]{1,128}$/u.test(error.code)) throw decodeFailure();
  if (error.retry === "never") { exactKeys(error, ["code", "retry"]); return new ProgramBoundaryError(status, error.code); }
  exactKeys(error, ["code", "retry", "retry_after_seconds"]);
  const seconds = error.retry_after_seconds;
  if (error.retry !== "after" || typeof seconds !== "number" || !Number.isSafeInteger(seconds) || seconds <= 0 || !Number.isSafeInteger(seconds * 1000)) throw decodeFailure();
  return new ProgramBoundaryError(status, error.code, seconds * 1000);
}

function invalidArgument(): PlatformSdkError {
  return new PlatformSdkError({ code: "invalid-argument", retry: "never" });
}

function unavailableCapability(): PlatformSdkError {
  return new PlatformSdkError({ code: "unavailable-capability", retry: "never" });
}

function decodeFailure(requestId?: string): PlatformSdkError {
  return new PlatformSdkError({ code: "decode-failure", retry: "never", ...(requestId === undefined ? {} : { requestId }) });
}

export const AGENT_ENVELOPE_VERSION = 1 as const;
export const AGENT_ENVELOPE_PATH = "/v1/agent/rpc" as const;
const MAX_TENANT_BYTES = 255;
const MAX_U64 = (1n << 64n) - 1n;
const CANONICAL_DECIMAL = /^(?:0|[1-9][0-9]{0,19})$/u;
const MAX_ENVELOPE_BYTES = 1_048_576;
const BOOTSTRAP_OPERATIONS: ReadonlySet<string> = new Set<AgentOperation>(["agent.register", "session.open"]);
const VERIFICATION_LEVELS: ReadonlySet<string> = new Set([
  "Unverified", "SequencerSigned", "BatchIncluded", "StateProven", "CheckpointFinalised", "SettlementAnchored",
]);
const AGENT_ENVELOPE_OPERATIONS: ReadonlySet<string> = new Set<AgentOperation>([
  "agent.register", "approval.approve", "approval.get", "approval.list", "approval.reject", "availability.fetch",
  "budget.create", "budget.fund", "budget.list", "budget.reconciliation", "budget.revoke",
  "capability.attenuate", "capability.create", "capability.list", "capability.revoke", "export.offline", "faucet.claim",
  "prepare", "program.activity", "program.call", "program.deploy", "program.discover", "program.interface",
  "program.receipt", "program.simulate", "program.upgrade", "program.wind-down", "project",
  "read.account", "read.balance", "read.batch", "read.checkpoint", "read.history", "read.module_state", "read.proof_bundle",
  "session.close", "session.list", "session.open", "session.refresh", "sign", "submit",
  "subscription.acknowledge", "subscription.create", "subscription.delete", "subscription.health", "subscription.list",
  "subscription.pause", "subscription.resume", "track", "wait",
]);
const ENVELOPE_MUTATIONS: ReadonlySet<string> = new Set<AgentOperation>([
  "agent.register", "approval.approve", "approval.reject", "budget.create", "budget.fund", "budget.revoke",
  "capability.attenuate", "capability.create", "capability.revoke", "prepare", "program.call", "program.deploy",
  "program.upgrade", "program.wind-down", "session.close", "session.open", "session.refresh", "sign", "submit",
  "subscription.acknowledge", "subscription.create", "subscription.delete", "subscription.pause", "subscription.resume",
]);

/** Full daemon session coordinates; the token never appears in string or JSON renderings of this object. */
export class AgentSessionCredential {
  readonly #tenant: string;
  readonly #sessionId: string;
  readonly #tokenId: SecretBytes;
  readonly #generation: bigint;

  public constructor(tenant: string, sessionId: string, tokenId: SecretBytes, generation: bigint | string) {
    const tenantBytes = Buffer.byteLength(tenant, "utf8");
    if (typeof tenant !== "string" || tenantBytes === 0 || tenantBytes > MAX_TENANT_BYTES || tenant.includes("\0")
      || !wellFormedUtf16(tenant)) throw invalidArgument();
    if (!HEX32.test(sessionId)) throw invalidArgument();
    this.#tenant = tenant;
    this.#sessionId = sessionId;
    this.#tokenId = tokenId;
    this.#generation = exactU64(generation);
    this.#tokenId.withBytes((bytes) => { if (bytes.length !== 32) throw invalidArgument(); });
  }

  public get tenant(): string { return this.#tenant; }
  public get sessionId(): string { return this.#sessionId; }
  public get generation(): bigint { return this.#generation; }

  /** @internal Serialises the four coordinates for one envelope body. */
  public encode(): Readonly<Record<string, string>> {
    return this.#tokenId.withBytes((bytes) => Object.freeze({
      tenant: this.#tenant,
      session_id: this.#sessionId,
      token_id: Buffer.from(bytes).toString("hex"),
      generation: this.#generation.toString(10),
    }));
  }

  public toString(): string { return "[REDACTED]"; }
  public toJSON(): string { return "[REDACTED]"; }
}

export interface AgentEnvelopeTransportOptions {
  readonly endpoint: URL | string;
  /** Required for every operation except the bootstrap set agent.register and session.open, which carry a null credential. */
  readonly session?: AgentSessionCredential;
  /** Gateway API-key admission; a separate authority from the daemon session credential. */
  readonly gatewayCredential?: LayerXKeyCredential;
  /** Explicit trusted server CA (PEM) replacing the platform store; server identity is always verified. */
  readonly trustedCa?: string | Buffer;
  readonly timeoutMs?: number;
  readonly maximumResponseBytes?: number;
}

export interface AgentEnvelopeSuccess<TValue = unknown> {
  readonly request_id: string;
  readonly value: TValue;
  readonly verification_status: unknown;
}

/** Version 1 authenticated operation envelope over the unified gateway route POST /v1/agent/rpc. */
export class AgentEnvelopeTransport implements ProductionTransport {
  readonly #endpoint: URL;
  readonly #session: AgentSessionCredential | undefined;
  readonly #gatewayCredential: LayerXKeyCredential | undefined;
  readonly #trustedCa: string | Buffer | undefined;
  readonly #timeoutMs: number;
  readonly #maximumResponseBytes: number;

  public constructor(options: AgentEnvelopeTransportOptions) {
    if (options.session !== undefined && !(options.session instanceof AgentSessionCredential)) throw invalidArgument();
    this.#endpoint = routeEndpoint(validateEndpoint(options.endpoint), AGENT_ENVELOPE_PATH);
    this.#session = options.session;
    this.#gatewayCredential = options.gatewayCredential;
    if (options.trustedCa !== undefined && (this.#endpoint.protocol !== "https:" || options.trustedCa.length === 0)) throw invalidArgument();
    this.#trustedCa = options.trustedCa;
    this.#timeoutMs = exactPositive(options.timeoutMs ?? DEFAULT_TIMEOUT_MS);
    this.#maximumResponseBytes = exactPositive(options.maximumResponseBytes ?? MAX_RESPONSE_BYTES);
    if (this.#maximumResponseBytes > MAX_RESPONSE_BYTES) throw invalidArgument();
  }

  public async call<TRequest, TResponse>(call: TransportCall<TRequest>): Promise<TResponse> {
    if (call.plane !== "agent" || !AGENT_ENVELOPE_OPERATIONS.has(call.operation)) throw unavailableCapability();
    const operation = call.operation as AgentOperation;
    const mutation = ENVELOPE_MUTATIONS.has(operation);
    if (mutation) {
      if (call.idempotencyKey === undefined || !HEX32.test(call.idempotencyKey)) throw invalidArgument();
    } else if (call.idempotencyKey !== undefined) {
      throw invalidArgument();
    }
    const requestId = freshRequestId();
    const body = encodeAgentEnvelope(operation, requestId, call.request, this.#session, call.idempotencyKey);
    const headers: http.OutgoingHttpHeaders = {
      Accept: "application/json",
      "Content-Type": "application/json",
      "Content-Length": body.length,
      "User-Agent": "layerx-typescript/0.1.0",
    };
    if (this.#gatewayCredential !== undefined) {
      this.#gatewayCredential.use((authorization) => { headers.Authorization = authorization; });
    }
    return await this.dispatch<TResponse>(headers, body, mutation, requestId);
  }

  private dispatch<TResponse>(headers: http.OutgoingHttpHeaders, body: Buffer, mutation: boolean, requestId: string): Promise<TResponse> {
    const ambiguous = (): PlatformSdkError => mutation
      ? new PlatformSdkError({ code: "unknown-outcome", retry: "unknown-outcome" })
      : new PlatformSdkError({ code: "transport-failure", retry: "safe" });
    return new Promise<TResponse>((resolve, reject) => {
      const driver = this.#endpoint.protocol === "https:" ? https : http;
      let settled = false;
      const finish = <T>(callback: (value: T) => void, value: T): void => {
        if (settled) return;
        settled = true;
        callback(value);
      };
      const options: https.RequestOptions = { method: "POST", headers, timeout: this.#timeoutMs, rejectUnauthorized: true };
      if (this.#trustedCa !== undefined) options.ca = this.#trustedCa;
      const request = driver.request(this.#endpoint, options, (response) => {
        const chunks: Buffer[] = [];
        let received = 0;
        response.on("data", (chunk: Buffer) => {
          received += chunk.length;
          if (received > this.#maximumResponseBytes) {
            response.destroy();
            finish(reject, mutation ? ambiguous() : decodeFailure());
            return;
          }
          chunks.push(Buffer.from(chunk));
        });
        response.on("end", () => {
          if (settled) return;
          try {
            const status = response.statusCode ?? 0;
            if (status >= 300 && status < 400) throw mutation ? ambiguous() : decodeFailure();
            if (response.headers["content-type"] !== "application/json") throw mutation ? ambiguous() : decodeFailure();
            finish(resolve, decodeAgentEnvelopeResponse(status, Buffer.concat(chunks), mutation, requestId) as TResponse);
          } catch (error) {
            finish(reject, error);
          }
        });
        response.on("error", () => finish(reject, ambiguous()));
      });
      request.on("timeout", () => request.destroy());
      request.on("error", () => finish(reject, ambiguous()));
      request.end(body);
    });
  }
}

/** Encodes the exact version 1 envelope; integers are emitted as canonical decimal strings. */
export function encodeAgentEnvelope(
  operation: AgentOperation,
  requestId: string,
  request: unknown,
  session: AgentSessionCredential | undefined,
  idempotency: string | undefined,
): Buffer {
  if (!AGENT_ENVELOPE_OPERATIONS.has(operation)) throw unavailableCapability();
  if (!CANONICAL_DECIMAL.test(requestId) || BigInt(requestId) > MAX_U64) throw invalidArgument();
  const mutation = ENVELOPE_MUTATIONS.has(operation);
  if (mutation ? idempotency === undefined || !HEX32.test(idempotency) : idempotency !== undefined) throw invalidArgument();
  let credential: Readonly<Record<string, string>> | null;
  if (BOOTSTRAP_OPERATIONS.has(operation)) credential = null;
  else if (session === undefined) throw invalidArgument();
  else credential = session.encode();
  let text: string;
  try {
    text = JSON.stringify({
      version: AGENT_ENVELOPE_VERSION,
      request_id: requestId,
      operation,
      request: canonicalRequest(record(request), 0),
      credential,
      idempotency_key: mutation ? idempotency : null,
    });
  } catch (error) {
    if (error instanceof PlatformSdkError) throw error;
    throw invalidArgument();
  }
  const body = Buffer.from(text, "utf8");
  if (body.length > MAX_ENVELOPE_BYTES) throw invalidArgument();
  return body;
}

function freshRequestId(): string {
  return (randomBytes(8).readBigUInt64BE(0) & ((1n << 63n) - 1n)).toString(10);
}

function canonicalRequest(value: unknown, depth: number): unknown {
  if (depth > 64) throw invalidArgument();
  if (value === null || typeof value === "boolean") return value;
  if (typeof value === "string") {
    if (!wellFormedUtf16(value)) throw invalidArgument();
    return value;
  }
  if (typeof value === "bigint") {
    if (value < 0n || value > MAX_U64) throw invalidArgument();
    return value.toString(10);
  }
  if (typeof value === "number") {
    if (!Number.isSafeInteger(value)) throw invalidArgument();
    return value;
  }
  if (Array.isArray(value)) return value.map((item) => canonicalRequest(item, depth + 1));
  if (typeof value === "object" && Object.getPrototypeOf(value) === Object.prototype) {
    const out: Record<string, unknown> = {};
    for (const [key, item] of Object.entries(value as Record<string, unknown>)) {
      if (!wellFormedUtf16(key) || item === undefined) throw invalidArgument();
      out[key] = canonicalRequest(item, depth + 1);
    }
    return out;
  }
  throw invalidArgument();
}

/** Decodes ApiSuccess or the established ApiError; an ambiguous mutation reply is unknown-outcome, never safe. */
export function decodeAgentEnvelopeResponse(status: number, encoded: Buffer, mutation: boolean, sentRequestId: string): AgentEnvelopeSuccess {
  const ambiguous = (requestId?: string): PlatformSdkError => mutation
    ? new PlatformSdkError({ code: "unknown-outcome", retry: "unknown-outcome", ...(requestId === undefined ? {} : { requestId }) })
    : decodeFailure(requestId);
  let envelope: Readonly<Record<string, unknown>>;
  try {
    const parsed = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(encoded), (_key, value: unknown) => {
      if (typeof value === "number" && !Number.isSafeInteger(value)) throw invalidArgument();
      return value;
    }) as unknown;
    envelope = record(parsed);
  } catch { throw ambiguous(); }
  if ("class" in envelope) {
    try { exactKeys(envelope, ["class", "protocol_result_code", "retriability", "reason", "request_id"]); }
    catch { throw ambiguous(); }
    if (envelope.request_id !== sentRequestId && envelope.request_id !== "0") throw ambiguous();
    let refusal: PlatformSdkError;
    try { refusal = serviceError(status, envelope); }
    catch (error) {
      throw mutation && error instanceof PlatformSdkError ? ambiguous(error.requestId) : error;
    }
    if (mutation && (envelope.class === "TransportFailure" || envelope.class === "Deadline")) {
      throw ambiguous(refusal.requestId);
    }
    throw refusal;
  }
  const requestId = typeof envelope.request_id === "string" ? envelope.request_id : undefined;
  try { exactKeys(envelope, ["request_id", "value", "verification_status"]); }
  catch { throw ambiguous(requestId); }
  if (status !== 200 || envelope.request_id !== sentRequestId) throw ambiguous(requestId);
  const verification = envelope.verification_status;
  if (!validVerificationStatus(verification)) {
    throw new PlatformSdkError({ code: "verification-failure", retry: mutation ? "unknown-outcome" : "never", requestId: sentRequestId });
  }
  return Object.freeze({ request_id: sentRequestId, value: envelope.value, verification_status: verification });
}

function validVerificationStatus(value: unknown): boolean {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return false;
  const status = value as Readonly<Record<string, unknown>>;
  if (status.state === "achieved") {
    return Object.keys(status).length === 2 && typeof status.level === "string" && VERIFICATION_LEVELS.has(status.level);
  }
  return status.state === "unverified" && Object.keys(status).length === 4
    && typeof status.requested === "string" && VERIFICATION_LEVELS.has(status.requested)
    && typeof status.achieved === "string" && VERIFICATION_LEVELS.has(status.achieved)
    && typeof status.reason === "string" && /^[a-z0-9_.]{1,128}$/u.test(status.reason);
}

function exactU64(value: bigint | string): bigint {
  if (typeof value === "string") {
    if (!CANONICAL_DECIMAL.test(value)) throw invalidArgument();
    value = BigInt(value);
  }
  if (typeof value !== "bigint" || value < 0n || value > MAX_U64) throw invalidArgument();
  return value;
}

function wellFormedUtf16(value: string): boolean {
  return !/[\uD800-\uDFFF]/u.test(value);
}
