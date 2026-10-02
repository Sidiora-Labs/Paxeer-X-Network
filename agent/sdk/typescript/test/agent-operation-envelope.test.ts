import { readFileSync } from "node:fs";

import {
  AgentEnvelopeTransport,
  AgentSessionCredential,
  LayerXKeyCredential,
  PlatformSdkError,
  SecretBytes,
} from "../src/index.js";
import type { AgentEnvelopeSuccess } from "../src/index.js";
import type { Operation } from "../src/generated/client.js";

// Probe of the real unified gateway and full-mode daemon. Every input is explicit; nothing is defaulted or synthesised.
// LAYERX_AGENT_ENVELOPE_PROBE names a JSON file:
// {"endpoint","trusted_ca_file"?,"tenant","session_id","token_id_file","generation","gateway_key_id","gateway_key_file",
//  "read_account":{request},"program_read":{"operation","request"},"approval_list":{request},"faucet_claim":{request}}
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

function secretFile(path: string, name: string): Uint8Array {
  let bytes: Buffer;
  try { bytes = readFileSync(path); } catch { refuse(`${name} is unreadable`); }
  const trimmed = bytes.toString("utf8").trim();
  bytes.fill(0);
  if (trimmed.length === 0) refuse(`${name} is empty`);
  return new Uint8Array(Buffer.from(trimmed, "utf8"));
}

const probePath = process.env.LAYERX_AGENT_ENVELOPE_PROBE;
if (probePath === undefined || probePath.length === 0) refuse("LAYERX_AGENT_ENVELOPE_PROBE is not set");
let input: Readonly<Record<string, unknown>>;
try {
  const parsed = JSON.parse(readFileSync(probePath, "utf8")) as unknown;
  if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("not an object");
  input = parsed as Readonly<Record<string, unknown>>;
} catch { refuse("LAYERX_AGENT_ENVELOPE_PROBE is not a readable JSON object"); }

const endpoint = text(input, "endpoint");
let trustedCa: Buffer | undefined;
if ("trusted_ca_file" in input) {
  try { trustedCa = readFileSync(text(input, "trusted_ca_file")); } catch { refuse("trusted_ca_file is unreadable"); }
}
const generation = text(input, "generation");
const tokenHex = Buffer.from(secretFile(text(input, "token_id_file"), "token_id_file")).toString("utf8");
if (!/^[0-9a-f]{64}$/u.test(tokenHex)) refuse("token_id_file does not hold 64 lowercase hex characters");
const tokenId = new SecretBytes(new Uint8Array(Buffer.from(tokenHex, "hex")));
const gatewayKey = new LayerXKeyCredential(
  text(input, "gateway_key_id"),
  new SecretBytes(secretFile(text(input, "gateway_key_file"), "gateway_key_file")),
);
const session = new AgentSessionCredential(text(input, "tenant"), text(input, "session_id"), tokenId, generation);
const transportFor = (credential: AgentSessionCredential): AgentEnvelopeTransport => new AgentEnvelopeTransport({
  endpoint,
  session: credential,
  gatewayCredential: gatewayKey,
  ...(trustedCa === undefined ? {} : { trustedCa }),
});
const transport = transportFor(session);

const results: { case: string; outcome: "pass" | "fail"; detail: string }[] = [];

async function expectSuccess(name: string, operation: Operation, request: unknown): Promise<void> {
  try {
    const success = await transport.call<unknown, AgentEnvelopeSuccess>({ plane: "agent", operation, request });
    assert(/^(?:0|[1-9][0-9]*)$/u.test(success.request_id), `${name}: request_id is not canonical decimal`);
    assert("value" in success, `${name}: value missing`);
    const status = success.verification_status as Readonly<Record<string, unknown>>;
    results.push({ case: name, outcome: "pass", detail: `verification ${String(status.state)}` });
  } catch (error) {
    results.push({ case: name, outcome: "fail", detail: describe(error) });
  }
}

async function expectRefusal(name: string, run: () => Promise<unknown>, code: string, retry: string): Promise<void> {
  try {
    await run();
    results.push({ case: name, outcome: "fail", detail: "succeeded where a refusal was required" });
  } catch (error) {
    const ok = error instanceof PlatformSdkError && error.code === code && error.retry === retry;
    results.push({ case: name, outcome: ok ? "pass" : "fail", detail: describe(error) });
  }
}

function describe(error: unknown): string {
  return error instanceof PlatformSdkError ? JSON.stringify(error.toJSON()) : error instanceof Error ? error.name : "non-error";
}

const programRead = object(input, "program_read");
const programOperation = text(programRead, "operation");
if (!["program.discover", "program.interface", "program.receipt", "program.activity"].includes(programOperation)) {
  refuse("program_read.operation is not a program read");
}

await expectSuccess("sdk_typescript_read", "read.account", object(input, "read_account"));
await expectSuccess("sdk_typescript_program_read", programOperation as Operation, object(programRead, "request"));
await expectSuccess("sdk_typescript_approval_list", "approval.list", object(input, "approval_list"));
await expectRefusal("sdk_typescript_faucet_retired",
  () => transport.call({ plane: "agent", operation: "faucet.claim", request: object(input, "faucet_claim") }),
  "unavailable-capability", "never");
const staleGeneration = (BigInt(generation) + 1n).toString(10);
await expectRefusal("sdk_typescript_wrong_generation",
  () => transportFor(new AgentSessionCredential(session.tenant, session.sessionId, tokenId, staleGeneration))
    .call({ plane: "agent", operation: "read.account", request: object(input, "read_account") }),
  "policy-refusal", "never");
await expectRefusal("sdk_typescript_unknown_operation_local",
  () => transport.call({ plane: "agent", operation: "not.catalogued" as Operation, request: {} }),
  "unavailable-capability", "never");
await expectRefusal("sdk_typescript_missing_idempotency_local",
  () => transport.call({ plane: "agent", operation: "budget.fund", request: {} }),
  "invalid-argument", "never");
await expectRefusal("sdk_typescript_noncanonical_generation_local",
  async () => new AgentSessionCredential(session.tenant, session.sessionId, tokenId, `0${generation}`),
  "invalid-argument", "never");
assert(JSON.stringify(session) === "\"[REDACTED]\"" && String(session) === "[REDACTED]", "session credential rendering leaked");

for (const result of results) process.stdout.write(`${JSON.stringify(result)}\n`);
const failed = results.filter((result) => result.outcome !== "pass");
if (failed.length > 0) {
  process.stderr.write(`agent-operation-envelope: ${failed.length} of ${results.length} cases failed\n`);
  process.exit(1);
}
