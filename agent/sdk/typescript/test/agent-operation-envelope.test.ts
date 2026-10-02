import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import {
  AGENT_DAEMON_RPC_PATH,
  AGENT_ENVELOPE_OPERATION_NAMES,
  AGENT_ENVELOPE_PATH,
  AgentEnvelopeTransport,
  AgentSessionCredential,
  idempotencyKey,
  LayerXKeyCredential,
  PlatformSdkError,
  SecretBytes,
} from "../src/index.js";
import type { AgentEnvelopeSuccess } from "../src/index.js";
import type { Operation } from "../src/generated/client.js";

// Probe of the real unified gateway and full-mode daemon (harness phase "read").
// PAXEER_X_AGENT_ENVELOPE_CASE names the private harness JSON case file; nothing is defaulted or synthesised.
// Secret material is read from the named files and never printed.

function assert(condition: boolean, message: string): asserts condition {
  if (!condition) throw new Error(message);
}

function refuse(message: string): never {
  process.stderr.write(`agent-operation-envelope: refused: ${message}\n`);
  process.exit(2);
}

function field(source: Readonly<Record<string, unknown>>, name: string): unknown {
  if (!(name in source)) refuse(`probe input lacks ${name}`);
  return source[name];
}

function text(source: Readonly<Record<string, unknown>>, name: string): string {
  const value = field(source, name);
  if (typeof value !== "string" || value.length === 0) refuse(`probe input ${name} is not a non-empty string`);
  return value;
}

function object(source: Readonly<Record<string, unknown>>, name: string): Readonly<Record<string, unknown>> {
  const value = field(source, name);
  if (value === null || typeof value !== "object" || Array.isArray(value)) refuse(`probe input ${name} is not an object`);
  return value as Readonly<Record<string, unknown>>;
}

function strings(source: Readonly<Record<string, unknown>>, name: string): readonly string[] {
  const value = field(source, name);
  if (!Array.isArray(value) || value.some((item) => typeof item !== "string")) refuse(`probe input ${name} is not a string array`);
  return value as readonly string[];
}

function secretFile(path: string, name: string): Uint8Array {
  let bytes: Buffer;
  try { bytes = readFileSync(path); } catch { refuse(`${name} is unreadable`); }
  const raw = bytes.toString("utf8");
  bytes.fill(0);
  const line = raw.endsWith("\n") ? raw.slice(0, -1) : raw;
  if (line.length === 0 || line.includes("\n")) refuse(`${name} is not exactly one line`);
  return new Uint8Array(Buffer.from(line, "utf8"));
}

const probePath = process.env.PAXEER_X_AGENT_ENVELOPE_CASE;
if (probePath === undefined || probePath.length === 0) refuse("PAXEER_X_AGENT_ENVELOPE_CASE is not set");
let input: Readonly<Record<string, unknown>>;
try {
  const parsed = JSON.parse(readFileSync(probePath, "utf8")) as unknown;
  if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("not an object");
  input = parsed as Readonly<Record<string, unknown>>;
} catch { refuse("PAXEER_X_AGENT_ENVELOPE_CASE is not a readable JSON object"); }

if (text(input, "phase") !== "read") refuse("the TypeScript probe runs only phase \"read\"");

const operations = strings(input, "operations");
if (operations.length !== AGENT_ENVELOPE_OPERATION_NAMES.length
  || operations.some((name, index) => name !== AGENT_ENVELOPE_OPERATION_NAMES[index])) {
  refuse("operations does not equal the SDK catalogue");
}

const endpointText = text(input, "endpoint");
let endpointUrl: URL;
try { endpointUrl = new URL(endpointText); } catch { refuse("endpoint is not a URL"); }
if (endpointUrl.protocol !== "https:" || endpointUrl.pathname !== AGENT_ENVELOPE_PATH || endpointUrl.search !== "" || endpointUrl.hash !== "") {
  refuse(`endpoint is not an https ${AGENT_ENVELOPE_PATH} URL`);
}
if (endpointUrl.hostname !== text(input, "server_name")) refuse("endpoint host differs from server_name");
const gatewayBase = new URL(endpointUrl.toString());
gatewayBase.pathname = "/";

let trustedCa: Buffer;
if ("ca_pem" in input) {
  try { trustedCa = readFileSync(text(input, "ca_pem")); } catch { refuse("ca_pem is unreadable"); }
} else if ("ca_der" in input) {
  let der: Buffer;
  try { der = readFileSync(text(input, "ca_der")); } catch { refuse("ca_der is unreadable"); }
  trustedCa = Buffer.from(`-----BEGIN CERTIFICATE-----\n${der.toString("base64").replace(/(.{64})/gu, "$1\n")}\n-----END CERTIFICATE-----\n`, "utf8");
} else refuse("probe input lacks ca_pem and ca_der");

let credentialInput: Readonly<Record<string, unknown>>;
try {
  let raw: Buffer;
  try { raw = readFileSync(text(input, "credential_file")); } catch { refuse("credential_file is unreadable"); }
  const parsed = JSON.parse(raw.toString("utf8")) as unknown;
  raw.fill(0);
  if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("not an object");
  credentialInput = parsed as Readonly<Record<string, unknown>>;
} catch { refuse("credential_file is not a JSON object"); }
if (Object.keys(credentialInput).length !== 4) refuse("credential_file has fields beyond tenant, session_id, token_id, generation");
const generation = text(credentialInput, "generation");
const tokenHex = text(credentialInput, "token_id");
if (!/^[0-9a-f]{64}$/u.test(tokenHex)) refuse("credential token_id is not 64 lowercase hex characters");
const tokenId = new SecretBytes(new Uint8Array(Buffer.from(tokenHex, "hex")));
const gatewayLine = Buffer.from(secretFile(text(input, "gateway_api_key_file"), "gateway_api_key_file")).toString("utf8");
const separator = gatewayLine.indexOf(":");
if (separator <= 0) refuse("gateway_api_key_file is not <key_id>:<secret>");
const gatewayKey = new LayerXKeyCredential(
  gatewayLine.slice(0, separator),
  new SecretBytes(new Uint8Array(Buffer.from(gatewayLine.slice(separator + 1), "utf8"))),
);
const responseDir = text(input, "response_dir");
const requests = object(input, "requests");
const cases = strings(input, "cases");
if (cases.length === 0) refuse("cases is empty");

const session = new AgentSessionCredential(text(credentialInput, "tenant"), text(credentialInput, "session_id"), tokenId, generation);
let lastResponse: { status: number; body: Buffer } | undefined;
let responseCount = 0;
const transportFor = (credential: AgentSessionCredential, endpoint: URL = gatewayBase): AgentEnvelopeTransport => new AgentEnvelopeTransport({
  endpoint,
  session: credential,
  gatewayCredential: gatewayKey,
  trustedCa,
  onResponse: (status, body) => { responseCount += 1; lastResponse = { status, body: Buffer.from(body) }; },
});
const transport = transportFor(session);
function observed(): { status: number; body: Buffer } | undefined { return lastResponse; }

const EXPECTED_OPERATION: Readonly<Record<string, Operation>> = Object.freeze({
  read: "read.account",
  program_read: "program.interface",
  approval_list: "approval.list",
});

const results: { case: string; outcome: "pass" | "fail"; detail: string }[] = [];

// Harness case ids served outside the plain read set. Mutation idempotency keys are the harness-issued per-probe keys
// in requests.<id>.idempotency_key (fresh per language run), used verbatim and never derived or defaulted here.
const SPECIAL_CASES: Readonly<Record<string, (name: string) => Promise<string>>> = Object.freeze({
  read_decode_failure: (name: string) => expectDecodeOutcome(name, false),
  mutation_decode_unknown: (name: string) => expectDecodeOutcome(name, true),
  read_daemon: (name: string) => readDaemon(name),
});

function recordResponse(name: string): void {
  if (lastResponse === undefined) throw new Error(`${name}: no HTTP response was received`);
  let body: unknown;
  try { body = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(lastResponse.body)) as unknown; }
  catch { throw new Error(`${name}: response body is not JSON`); }
  writeFileSync(join(responseDir, `${name}.json`), `${JSON.stringify({ status: lastResponse.status, body })}\n`, { mode: 0o600 });
}

async function runCase(name: string): Promise<void> {
  lastResponse = undefined;
  responseCount = 0;
  const special = SPECIAL_CASES[name];
  if (special !== undefined) {
    try {
      results.push({ case: name, outcome: "pass", detail: await special(name) });
    } catch (error) {
      results.push({ case: name, outcome: "fail", detail: describe(error) });
    }
    return;
  }
  try {
    const expected = EXPECTED_OPERATION[name];
    if (expected === undefined) throw new Error(`${name}: case is not implemented by the TypeScript probe`);
    const entry = object(requests, name);
    const operation = text(entry, "operation");
    if (operation !== expected) throw new Error(`${name}: requests.${name}.operation is not ${expected}`);
    const request = object(entry, "request");
    const key = "idempotency_key" in entry ? text(entry, "idempotency_key") : undefined;
    let success: AgentEnvelopeSuccess;
    try {
      success = await transport.call<unknown, AgentEnvelopeSuccess>({
        plane: "agent",
        operation: expected,
        request,
        ...(key === undefined ? {} : { idempotencyKey: idempotencyKey(key) }),
      });
    } finally {
      if (observed() !== undefined) recordResponse(name);
    }
    assert(/^(?:0|[1-9][0-9]*)$/u.test(success.request_id), `${name}: request_id is not canonical decimal`);
    assert("value" in success, `${name}: value missing`);
    assert(observed()?.status === 200, `${name}: status was not 200`);
    const status = success.verification_status as Readonly<Record<string, unknown>>;
    results.push({ case: name, outcome: "pass", detail: `verification ${String(status.state)}` });
  } catch (error) {
    results.push({ case: name, outcome: "fail", detail: describe(error) });
  }
}

const checks: { check: string; ok: boolean; detail: string }[] = [];

async function expectRefusal(name: string, run: () => Promise<unknown>, code: string, retry: string): Promise<void> {
  try {
    await run();
    checks.push({ check: name, ok: false, detail: "succeeded where a refusal was required" });
  } catch (error) {
    const ok = error instanceof PlatformSdkError && error.code === code && error.retry === retry;
    checks.push({ check: name, ok, detail: describe(error) });
  }
}

function describe(error: unknown): string {
  return error instanceof PlatformSdkError ? JSON.stringify(error.toJSON())
    : error instanceof Error ? error.message : "non-error";
}

// A case entry may carry its own gateway "endpoint" (same https host and route as the case file endpoint) when the
// harness serves the schema-violating reply from a distinct listener; otherwise the case file endpoint is used.
function caseEndpoint(name: string, entry: Readonly<Record<string, unknown>>): URL {
  if (!("endpoint" in entry)) return gatewayBase;
  let url: URL;
  try { url = new URL(text(entry, "endpoint")); } catch { throw new Error(`${name}: endpoint is not a URL`); }
  if (url.protocol !== "https:" || url.pathname !== AGENT_ENVELOPE_PATH || url.search !== "" || url.hash !== ""
    || url.hostname !== endpointUrl.hostname) throw new Error(`${name}: endpoint is not an https ${AGENT_ENVELOPE_PATH} URL on server_name`);
  url.pathname = "/";
  return url;
}

// Writes {status, body}; body is the parsed JSON, or the raw UTF-8 text when the reply is deliberately undecodable.
function recordRawResponse(name: string): void {
  if (lastResponse === undefined) return;
  const raw = lastResponse.body.toString("utf8");
  let body: unknown;
  try { body = JSON.parse(raw) as unknown; } catch { body = raw; }
  writeFileSync(join(responseDir, `${name}.json`), `${JSON.stringify({ status: lastResponse.status, body })}\n`, { mode: 0o600 });
}

async function expectDecodeOutcome(name: string, mutation: boolean): Promise<string> {
  const entry = object(requests, name);
  const operation = text(entry, "operation") as Operation;
  const request = object(entry, "request");
  const key = "idempotency_key" in entry ? text(entry, "idempotency_key") : undefined;
  if (mutation && key === undefined) throw new Error(`${name}: requests.${name} carries no idempotency_key`);
  if (!mutation && key !== undefined) throw new Error(`${name}: a read case carries an idempotency_key`);
  const caseTransport = transportFor(session, caseEndpoint(name, entry));
  let refusal: unknown;
  try {
    await caseTransport.call({ plane: "agent", operation, request, ...(key === undefined ? {} : { idempotencyKey: idempotencyKey(key) }) });
  } catch (error) {
    refusal = error;
  } finally {
    recordRawResponse(name);
  }
  if (refusal === undefined) throw new Error(`${name}: succeeded where ${mutation ? "unknown-outcome" : "decode-failure"} was required`);
  if (!(refusal instanceof PlatformSdkError)) throw refusal;
  const [code, retry] = mutation ? ["unknown-outcome", "unknown-outcome"] : ["decode-failure", "never"];
  assert(refusal.code === code && refusal.retry === retry, `${name}: expected ${code}/${retry}, got ${describe(refusal)}`);
  assert(responseCount <= 1, `${name}: the SDK sent the envelope more than once`);
  if (!mutation) assert(lastResponse !== undefined, `${name}: no HTTP response was received`);
  return `${code} ${retry}, ${responseCount} response`;
}

// Daemon-surface read: only the four case-file paths daemon_endpoint, client_cert_file, client_key_file, server_ca_file are read.
async function readDaemon(name: string): Promise<string> {
  let daemonUrl: URL;
  try { daemonUrl = new URL(text(input, "daemon_endpoint")); } catch { throw new Error(`${name}: daemon_endpoint is not a URL`); }
  if (daemonUrl.protocol !== "https:" || daemonUrl.pathname !== AGENT_DAEMON_RPC_PATH || daemonUrl.search !== "" || daemonUrl.hash !== "") {
    throw new Error(`${name}: daemon_endpoint is not an https ${AGENT_DAEMON_RPC_PATH} URL`);
  }
  daemonUrl.pathname = "/";
  const readFile = (field: string): Buffer => {
    try { return readFileSync(text(input, field)); } catch { throw new Error(`${name}: ${field} is unreadable`); }
  };
  const certificate = readFile("client_cert_file");
  const privateKey = readFile("client_key_file");
  const daemon = new AgentEnvelopeTransport({
    endpoint: daemonUrl,
    surface: "daemon",
    session,
    clientCertificate: new SecretBytes(new Uint8Array(certificate)),
    clientKey: new SecretBytes(new Uint8Array(privateKey)),
    trustedCa: readFile("server_ca_file"),
    onResponse: (status, body) => { responseCount += 1; lastResponse = { status, body: Buffer.from(body) }; },
  });
  certificate.fill(0);
  privateKey.fill(0);
  const entry = object(requests, "read");
  if (text(entry, "operation") !== "read.account") throw new Error(`${name}: requests.read.operation is not read.account`);
  let success: AgentEnvelopeSuccess;
  try {
    success = await daemon.call<unknown, AgentEnvelopeSuccess>({ plane: "agent", operation: "read.account", request: object(entry, "request") });
  } finally {
    if (lastResponse !== undefined) recordResponse(name);
  }
  assert(/^(?:0|[1-9][0-9]*)$/u.test(success.request_id), `${name}: request_id is not canonical decimal`);
  assert(lastResponse?.status === 200, `${name}: status was not 200`);
  return `verification ${String((success.verification_status as Readonly<Record<string, unknown>>).state)}`;
}

for (const name of cases) await runCase(name);

const readAccount = object(object(requests, "read"), "request");
await expectRefusal("faucet_retired",
  () => transport.call({ plane: "agent", operation: "faucet.claim", request: {} }),
  "unavailable-capability", "never");
const staleGeneration = (BigInt(generation) + 1n).toString(10);
await expectRefusal("wrong_generation",
  () => transportFor(new AgentSessionCredential(session.tenant, session.sessionId, tokenId, staleGeneration))
    .call({ plane: "agent", operation: "read.account", request: readAccount }),
  "policy-refusal", "never");
await expectRefusal("unknown_operation_local",
  () => transport.call({ plane: "agent", operation: "not.catalogued" as Operation, request: {} }),
  "unavailable-capability", "never");
await expectRefusal("missing_idempotency_local",
  () => transport.call({ plane: "agent", operation: "budget.fund", request: {} }),
  "invalid-argument", "never");
await expectRefusal("noncanonical_generation_local",
  async () => new AgentSessionCredential(session.tenant, session.sessionId, tokenId, `0${generation}`),
  "invalid-argument", "never");
checks.push({
  check: "credential_redaction",
  ok: JSON.stringify(session) === "\"[REDACTED]\"" && String(session) === "[REDACTED]",
  detail: "session credential rendering",
});

for (const result of results) {
  if (result.outcome === "pass") process.stdout.write(`PAXEER_X_AGENT_ENVELOPE_CASE ${result.case} passed\n`);
  else process.stderr.write(`${JSON.stringify(result)}\n`);
}
for (const check of checks) if (!check.ok) process.stderr.write(`${JSON.stringify(check)}\n`);
const passed = results.filter((result) => result.outcome === "pass").length;
process.stdout.write(`PAXEER_X_AGENT_ENVELOPE_CASES=${passed}\n`);
const failedChecks = checks.filter((check) => !check.ok).length;
if (passed !== cases.length || failedChecks > 0) {
  process.stderr.write(`agent-operation-envelope: ${cases.length - passed} of ${cases.length} cases and ${failedChecks} checks failed\n`);
  process.exit(1);
}
