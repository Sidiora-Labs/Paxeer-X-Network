import { AgentMiddleware, type AgentBudgetLedger, type AgentSigner, type AgentReceiptResolver } from "@sidiora/layerx-agent-middleware";
import { ProductionClient } from "@sidiora/layerx-sdk";
import type { WebhookDeliveryStore } from "@sidiora/layerx-seller-middleware";
import {
  AgentIntegrationError,
  readDeclaredConfig,
  authenticatedAgentTransport,
  type AgentDeclaredConfig,
  type Environment,
} from "./config.js";
import {
  LayerXAgentTransport,
} from "./services.js";
import { AgentToolExecutor } from "./tools.js";
import { AgentWebhookGateway } from "./webhooks.js";

export const AGENT_FRAMEWORKS = ["mcp", "a2a", "openai", "anthropic", "langchain", "vercel-ai"] as const;

export type AgentFramework = (typeof AGENT_FRAMEWORKS)[number];

export interface AgentIntegrationOptions {
  readonly environment: Environment;
  readonly budgets: AgentBudgetLedger;
  readonly signer: AgentSigner;
  readonly receipts: AgentReceiptResolver;
  readonly deliveries?: WebhookDeliveryStore;
  readonly now?: () => number;
  readonly fetch?: typeof globalThis.fetch;
  readonly wait?: (milliseconds: number) => Promise<void>;
}

export interface LayerXAgentIntegration {
  readonly config: AgentDeclaredConfig;
  readonly client: ProductionClient;
  readonly middleware: AgentMiddleware;
  readonly tools: AgentToolExecutor;
  readonly webhooks: AgentWebhookGateway;
  destroy(): void;
}

export function createAgentIntegration(options: AgentIntegrationOptions): LayerXAgentIntegration {
  const config = readDeclaredConfig(options.environment);
  if (options.budgets === undefined || options.signer === undefined || options.receipts === undefined) throw new AgentIntegrationError("missing-declared-key");
  const authenticated = authenticatedAgentTransport(options.environment, config);
  try {
  const client = new ProductionClient(new LayerXAgentTransport(authenticated.transport));
  const receipts = options.receipts;
  const middleware = new AgentMiddleware({
    client,
    protocolVersion: config.protocolVersion,
    budgets: options.budgets,
    signer: options.signer,
    receipts,
    maximumTrackPolls: config.maximumTrackPolls,
    ...(options.wait === undefined ? {} : { wait: options.wait }),
  });
  const webhooks = new AgentWebhookGateway({
    webhook: config.webhook,
    deliveryStorePath: config.webhookDeliveryStorePath,
    ...(options.deliveries === undefined ? {} : { deliveries: options.deliveries }),
    ...(options.now === undefined ? {} : { now: options.now }),
  });
  return {
    config,
    client,
    middleware,
    tools: new AgentToolExecutor({ middleware, client, receipts, config }),
    webhooks,
    destroy: () => {
      authenticated.destroy();
    },
  };
  } catch (error) { authenticated.destroy(); throw error; }
}

export function platform_int_agent_frameworks(): "receipt-verified-agent-framework-integrations" {
  return "receipt-verified-agent-framework-integrations";
}
