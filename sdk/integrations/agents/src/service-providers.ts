import { createHash } from "node:crypto";
import { PlatformSdkError } from "@sidiora/layerx-sdk";
import { signedActivityId, type PreparedActivity } from "@sidiora/layerx-agent-middleware";
import { isAbsolute } from "node:path";
import { pathToFileURL } from "node:url";
import type { AgentBudgetLedger, AgentPreparationBudget, AgentSigner, AgentReceiptResolver } from "@sidiora/layerx-agent-middleware";
import {
  AgentIntegrationError, authenticatedAgentTransport, boundedInteger, declaredProtocolVersion,
  optional, required, type Environment,
} from "./config.js";
import { DaemonReceiptResolver } from "./daemon-providers.js";
import { DaemonPreparationBudget } from "./daemon-budget.js";

export interface AgentServiceProviders {
  readonly budgets?: AgentBudgetLedger;
  readonly preparationBudgets?: AgentPreparationBudget;
  readonly signer: AgentSigner;
  readonly receipts: AgentReceiptResolver;
  destroy?(): void | Promise<void>;
}

export interface DaemonReceiptServiceProviders {
  readonly budgets?: AgentBudgetLedger;
  readonly signer: AgentSigner;
  readonly daemonReceiptStorePath: string;
  readonly daemonPreparationStorePath?: string;
  destroy?(): void | Promise<void>;
}

export type WalletApprovedServiceProviders = (
  Omit<AgentServiceProviders, "signer"> | Omit<DaemonReceiptServiceProviders, "signer">
) & { readonly signer?: never; readonly walletSigner: { readonly client: WalletLxSigningClient } };

export type AgentServiceProviderSource = AgentServiceProviders | DaemonReceiptServiceProviders | WalletApprovedServiceProviders;

export async function loadAgentServiceProviders(environment: Environment): Promise<AgentServiceProviders> {
  const path = required(environment, "LAYERX_AGENT_SERVICES_MODULE");
  if (!isAbsolute(path) || path.includes("\0")) throw new AgentIntegrationError("invalid-declared-key");
  const module = await import(pathToFileURL(path).href) as { createAgentServices?: (environment: Environment) => Promise<AgentServiceProviderSource> | AgentServiceProviderSource };
  if (typeof module.createAgentServices !== "function") throw new AgentIntegrationError("missing-declared-key");
  const providers = await module.createAgentServices(environment);
  if (providers === null || typeof providers !== "object"
    || providers.destroy !== undefined && typeof providers.destroy !== "function") throw new AgentIntegrationError("missing-declared-key");
  let signer: AgentSigner;
  if ("walletSigner" in providers) {
    if (providers.signer !== undefined || providers.walletSigner === null || typeof providers.walletSigner !== "object") throw new AgentIntegrationError("invalid-declared-key");
    signer = new ApprovedWalletSigner(providers.walletSigner.client);
  } else {
    if (typeof providers.signer?.sign !== "function") throw new AgentIntegrationError("missing-declared-key");
    signer = providers.signer;
  }
  if (providers.budgets !== undefined && (typeof providers.budgets.reserve !== "function" || typeof providers.budgets.hold !== "function"
    || typeof providers.budgets.commit !== "function" || typeof providers.budgets.release !== "function")) throw new AgentIntegrationError("missing-declared-key");
  if ("receipts" in providers) {
    if ("daemonReceiptStorePath" in providers || typeof providers.receipts?.resolve !== "function") throw new AgentIntegrationError("invalid-declared-key");
    if (providers.preparationBudgets !== undefined && typeof providers.preparationBudgets.spendPrepared !== "function"
      || (providers.budgets === undefined) === (providers.preparationBudgets === undefined)) throw new AgentIntegrationError("missing-declared-key");
    return { ...providers, signer };
  }
  if (!("daemonReceiptStorePath" in providers) || typeof providers.daemonReceiptStorePath !== "string"
    || providers.daemonPreparationStorePath !== undefined && typeof providers.daemonPreparationStorePath !== "string"
    || (providers.budgets === undefined) === (providers.daemonPreparationStorePath === undefined)) throw new AgentIntegrationError("missing-declared-key");
  const protocolVersion = declaredProtocolVersion(required(environment, "LAYERX_PROTOCOL_VERSION"));
  const authenticated = authenticatedAgentTransport(environment, {
    agentRpcUrl: required(environment, "LAYERX_AGENT_RPC_URL"),
    tenant: required(environment, "LAYERX_TENANT"),
    requestTimeoutMs: boundedInteger(optional(environment, "LAYERX_REQUEST_TIMEOUT_MS") ?? "30000", 1000, 300000),
  });
  let receipts: DaemonReceiptResolver | undefined;
  let preparationBudgets: DaemonPreparationBudget | undefined;
  try {
    const principal = {
      tenant: required(environment, "LAYERX_TENANT"), actor: required(environment, "LAYERX_ACTOR"),
      sessionId: required(environment, "LAYERX_SESSION_ID"),
    };
    receipts = new DaemonReceiptResolver({
      transport: authenticated.transport, protocolVersion, storePath: providers.daemonReceiptStorePath, principal,
    });
    if (providers.daemonPreparationStorePath !== undefined) preparationBudgets = new DaemonPreparationBudget({
      transport: authenticated.transport, storePath: providers.daemonPreparationStorePath, principal,
    });
    return {
      ...(providers.budgets === undefined ? {} : { budgets: providers.budgets }),
      ...(preparationBudgets === undefined ? {} : { preparationBudgets }),
      signer,
      receipts,
      async destroy() {
        try { preparationBudgets?.close(); }
        finally { try { receipts?.close(); } finally { authenticated.destroy(); await providers.destroy?.(); } }
      },
    };
  } catch (error) {
    try { preparationBudgets?.close(); }
    finally { try { receipts?.close(); } finally { authenticated.destroy(); await providers.destroy?.(); } }
    throw error;
  }
}

export interface WalletLxDisclosure {
  readonly account: string;
  readonly module: string;
  readonly operation: number;
  readonly amounts: readonly { readonly asset: string; readonly amount: string }[];
  readonly destinations: readonly string[];
  readonly sequence: string;
  readonly not_before: string;
  readonly not_after: string;
}
export interface WalletLxApproval {
  readonly version: 1;
  readonly principal: string;
  readonly key_id: string;
  readonly network_id: number;
  readonly protocol_version: number;
  readonly session_id: string;
  readonly activity_digest: string;
  readonly expires_at: string;
}
export interface WalletLxArtifact {
  readonly kind: "lx_activity" | "lx_send_authorization";
  readonly id: string;
  readonly state: "reviewed" | "approved" | "signing_unknown" | "signed";
  readonly activity: string;
  readonly signing_preimage: string;
  readonly public_key: string;
  readonly key_id: string;
  readonly principal: string;
  readonly did: string;
  readonly account_id: string;
  readonly epoch: number;
  readonly participants: readonly string[];
  readonly network_id: number;
  readonly protocol_version: number;
  readonly session_id: string;
  readonly disclosure: WalletLxDisclosure;
  readonly expires_at: string;
  readonly approval?: WalletLxApproval;
  readonly signature?: string;
  readonly attestor_audit?: readonly { readonly node_id: string; readonly audit_sequence: number }[];
}
export interface WalletLxSigningClient {
  lxApprovalStatus(approvalId: string): Promise<WalletLxArtifact>;
  signApprovedLxActivity(approvalId: string): Promise<WalletLxArtifact>;
}

class ApprovedWalletSigner implements AgentSigner {
  public readonly walletApprovalRequired = true as const;
  public constructor(private readonly wallet: WalletLxSigningClient) {
    if (!wallet || typeof wallet.lxApprovalStatus !== "function" || typeof wallet.signApprovedLxActivity !== "function") throw new AgentIntegrationError("missing-declared-key");
  }
  public async verifyWalletApproval(prepared: PreparedActivity): Promise<void> {
    await this.#retained(prepared);
  }
  public async sign(prepared: PreparedActivity): Promise<string> {
    const original = await this.#retained(prepared);
    const id = original.id;
    try {
      const signed = approvedArtifact(await this.wallet.signApprovedLxActivity(id), prepared, original.protocol_version);
      if (artifactBinding(signed) !== artifactBinding(original) || signed.state !== "signed" || signed.signature === undefined) throw walletUnknown();
      signedActivityId(prepared, signed.signature, signed.public_key);
      return signed.signature;
    } catch { throw walletUnknown(); }
  }
  async #retained(prepared: PreparedActivity): Promise<WalletLxArtifact> {
    const id = prepared.wallet_approval_id;
    if (typeof id !== "string" || !UUID.test(id) || prepared.signer_public_key === undefined) throw walletRefusal("policy-refusal", "never");
    const canonical = canonicalBytes(prepared.unsigned_canonical_bytes, 1048576);
    const protocol = canonical.readUInt16BE(0);
    if (protocol !== 2 && protocol !== 3) throw walletRefusal("unavailable-capability", "never");
    if (canonical.length < 5 || canonical.readUInt16BE(2) !== 0x1001 || canonical[4] !== 11
      || createHash("sha256").update(canonical).digest("hex") !== prepared.preparation_ref
      || createHash("sha256").update("LXP/v1/signature-preimage\0").update(canonical).digest("hex") !== prepared.signing_preimage) throw walletRefusal("verification-failure", "never");
    let original: WalletLxArtifact;
    try { original = approvedArtifact(await this.wallet.lxApprovalStatus(id), prepared, protocol); }
    catch { throw walletUnknown(); }
    if (original.state === "reviewed") throw walletRefusal("policy-refusal", "never");
    if (original.state === "signing_unknown") throw walletUnknown();
    if (original.state === "signed") {
      try {
        if (original.signature === undefined) throw walletUnknown();
        signedActivityId(prepared, original.signature, original.public_key);
      } catch { throw walletUnknown(); }
    }
    return original;
  }
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/u;
function approvedArtifact(value: unknown, prepared: PreparedActivity, protocol: number): WalletLxArtifact {
  const row = walletObject(value);
  walletExact(row, ["kind", "id", "state", "activity", "signing_preimage", "public_key", "key_id", "principal", "did", "account_id", "epoch", "participants", "network_id", "protocol_version", "session_id", "disclosure", "expires_at"], ["approval", "signature", "attestor_audit"]);
  if (row["kind"] !== "lx_activity" || row["id"] !== prepared.wallet_approval_id || row["activity"] !== prepared.unsigned_canonical_bytes
    || row["signing_preimage"] !== prepared.signing_preimage || row["public_key"] !== prepared.signer_public_key
    || row["protocol_version"] !== protocol || String(row["network_id"]) !== prepared.disclosure["network_id"]
    || row["did"] !== prepared.disclosure["actor"]) throw walletUnknown();
  if (!UUID.test(walletText(row["id"])) || !UUID.test(walletText(row["principal"])) || !UUID.test(walletText(row["session_id"]))) throw walletUnknown();
  walletText(row["key_id"]); walletText(row["did"]); fixedHex(row["account_id"], 32); fixedHex(row["public_key"], 32);
  integer(row["epoch"], 4294967295); integer(row["network_id"], 4294967295);
  if (!Array.isArray(row["participants"]) || row["participants"].length !== 3) throw walletUnknown();
  const participants = row["participants"].map(walletText);
  if (new Set(participants).size !== 3) throw walletUnknown();
  const d = walletObject(row["disclosure"]);
  walletExact(d, ["account", "module", "operation", "amounts", "destinations", "sequence", "not_before", "not_after"]);
  if (d["account"] !== row["account_id"] || d["sequence"] !== prepared.disclosure["account_sequence"]
    || d["not_before"] !== prepared.disclosure["not_before"] || d["not_after"] !== prepared.expiry) throw walletUnknown();
  walletText(d["module"]); integer(d["operation"], 65535);
  decimal(d["sequence"], 64); decimal(d["not_before"], 64); decimal(d["not_after"], 64);
  if (!Array.isArray(d["amounts"]) || d["amounts"].length > 1024 || !Array.isArray(d["destinations"]) || d["destinations"].length > 1024) throw walletUnknown();
  for (const amount of d["amounts"]) { const a = walletObject(amount); walletExact(a, ["asset", "amount"]); fixedHex(a["asset"], 32); decimal(a["amount"], 128); }
  for (const destination of d["destinations"]) fixedHex(destination, 32);
  const expiry = decimal(row["expires_at"], 64);
  if (expiry > decimal(d["not_after"], 64)) throw walletUnknown();
  if (typeof row["state"] !== "string" || !["reviewed", "approved", "signing_unknown", "signed"].includes(row["state"])) throw walletUnknown();
  if (row["state"] === "reviewed") {
    if (row["approval"] !== undefined || row["signature"] !== undefined || row["attestor_audit"] !== undefined) throw walletUnknown();
  } else {
    const a = walletObject(row["approval"]);
    walletExact(a, ["version", "principal", "key_id", "network_id", "protocol_version", "session_id", "activity_digest", "expires_at"]);
    if (a["version"] !== 1 || a["activity_digest"] !== prepared.signing_preimage || a["expires_at"] !== row["expires_at"]) throw walletUnknown();
    for (const field of ["principal", "key_id", "network_id", "protocol_version", "session_id"]) if (a[field] !== row[field]) throw walletUnknown();
  }
  if (row["state"] === "signed") {
    fixedHex(row["signature"], 64);
    if (!Array.isArray(row["attestor_audit"]) || row["attestor_audit"].length !== 3) throw walletUnknown();
    for (let index = 0; index < 3; index += 1) {
      const audit = walletObject(row["attestor_audit"][index]); walletExact(audit, ["node_id", "audit_sequence"]);
      if (audit["node_id"] !== participants[index]) throw walletUnknown(); integer(audit["audit_sequence"], Number.MAX_SAFE_INTEGER);
    }
  } else if (row["signature"] !== undefined || row["attestor_audit"] !== undefined) throw walletUnknown();
  return JSON.parse(JSON.stringify(row)) as WalletLxArtifact;
}
function artifactBinding(value: WalletLxArtifact): string {
  const { state: _state, signature: _signature, attestor_audit: _audit, ...binding } = value;
  return stableWallet(binding);
}
function stableWallet(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(stableWallet).join(",")}]`;
  if (value !== null && typeof value === "object") return `{${Object.entries(value).sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0).map(([key, item]) => `${JSON.stringify(key)}:${stableWallet(item)}`).join(",")}}`;
  const encoded = JSON.stringify(value);
  if (encoded === undefined) throw walletUnknown();
  return encoded;
}
function walletObject(value: unknown): Readonly<Record<string, unknown>> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw walletUnknown(); return value as Readonly<Record<string, unknown>>;
}
function walletExact(row: Readonly<Record<string, unknown>>, required: readonly string[], optional: readonly string[] = []): void {
  if (required.some((key) => !Object.prototype.hasOwnProperty.call(row, key)) || Object.keys(row).some((key) => !required.includes(key) && !optional.includes(key))) throw walletUnknown();
}
function walletText(value: unknown): string {
  if (typeof value !== "string" || !value || Buffer.byteLength(value) > 255 || value.includes("\0") || Buffer.from(value).toString("utf8") !== value) throw walletUnknown(); return value;
}
function integer(value: unknown, maximum: number): void {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0 || value > maximum) throw walletUnknown();
}
function decimal(value: unknown, bits: number): bigint {
  if (typeof value !== "string" || !/^(0|[1-9][0-9]*)$/u.test(value) || value.length > 39 || BigInt(value) >= 1n << BigInt(bits)) throw walletUnknown(); return BigInt(value);
}
function canonicalBytes(value: string, maximum: number): Buffer {
  if (typeof value !== "string" || !/^(?:[0-9a-f]{2})+$/u.test(value) || value.length > maximum * 2 || value.length < 10) throw walletUnknown(); return Buffer.from(value, "hex");
}
function fixedHex(value: unknown, bytes: number): void { if (typeof value !== "string" || !new RegExp(`^[0-9a-f]{${bytes * 2}}$`, "u").test(value)) throw walletUnknown(); }
function walletRefusal(code: "policy-refusal" | "verification-failure" | "unavailable-capability", retry: "never" | "unknown-outcome"): PlatformSdkError { return new PlatformSdkError({ code, retry }); }
function walletUnknown(): PlatformSdkError { return new PlatformSdkError({ code: "unknown-outcome", retry: "unknown-outcome" }); }
