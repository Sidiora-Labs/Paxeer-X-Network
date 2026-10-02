import { isAbsolute } from "node:path";
import { pathToFileURL } from "node:url";
import type { AgentBudgetLedger, AgentSigner, AgentReceiptResolver } from "@sidiora/layerx-agent-middleware";
import { AgentIntegrationError, required, type Environment } from "./config.js";

export interface AgentServiceProviders {
  readonly budgets: AgentBudgetLedger;
  readonly signer: AgentSigner;
  readonly receipts: AgentReceiptResolver;
  destroy?(): void | Promise<void>;
}

export async function loadAgentServiceProviders(environment: Environment): Promise<AgentServiceProviders> {
  const path = required(environment, "LAYERX_AGENT_SERVICES_MODULE");
  if (!isAbsolute(path) || path.includes("\0")) throw new AgentIntegrationError("invalid-declared-key");
  const module = await import(pathToFileURL(path).href) as { createAgentServices?: (environment: Environment) => Promise<AgentServiceProviders> | AgentServiceProviders };
  if (typeof module.createAgentServices !== "function") throw new AgentIntegrationError("missing-declared-key");
  const providers = await module.createAgentServices(environment);
  if (providers === null || typeof providers !== "object" || providers.budgets === undefined
    || typeof providers.budgets.reserve !== "function" || typeof providers.budgets.hold !== "function"
    || typeof providers.budgets.commit !== "function" || typeof providers.budgets.release !== "function"
    || typeof providers.signer?.sign !== "function" || typeof providers.receipts?.resolve !== "function"
    || providers.destroy !== undefined && typeof providers.destroy !== "function") throw new AgentIntegrationError("missing-declared-key");
  return providers;
}
