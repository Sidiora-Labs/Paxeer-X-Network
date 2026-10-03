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

export type AgentServiceProviderSource = AgentServiceProviders | DaemonReceiptServiceProviders;

export async function loadAgentServiceProviders(environment: Environment): Promise<AgentServiceProviders> {
  const path = required(environment, "LAYERX_AGENT_SERVICES_MODULE");
  if (!isAbsolute(path) || path.includes("\0")) throw new AgentIntegrationError("invalid-declared-key");
  const module = await import(pathToFileURL(path).href) as { createAgentServices?: (environment: Environment) => Promise<AgentServiceProviderSource> | AgentServiceProviderSource };
  if (typeof module.createAgentServices !== "function") throw new AgentIntegrationError("missing-declared-key");
  const providers = await module.createAgentServices(environment);
  if (providers === null || typeof providers !== "object"
    || typeof providers.signer?.sign !== "function"
    || providers.destroy !== undefined && typeof providers.destroy !== "function") throw new AgentIntegrationError("missing-declared-key");
  if (providers.budgets !== undefined && (typeof providers.budgets.reserve !== "function" || typeof providers.budgets.hold !== "function"
    || typeof providers.budgets.commit !== "function" || typeof providers.budgets.release !== "function")) throw new AgentIntegrationError("missing-declared-key");
  if ("receipts" in providers) {
    if ("daemonReceiptStorePath" in providers || typeof providers.receipts?.resolve !== "function") throw new AgentIntegrationError("invalid-declared-key");
    if (providers.preparationBudgets !== undefined && typeof providers.preparationBudgets.spendPrepared !== "function"
      || (providers.budgets === undefined) === (providers.preparationBudgets === undefined)) throw new AgentIntegrationError("missing-declared-key");
    return providers;
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
      signer: providers.signer,
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
