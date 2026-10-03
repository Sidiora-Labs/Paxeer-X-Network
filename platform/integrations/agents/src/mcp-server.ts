#!/usr/bin/env node
import { AgentIntegrationError } from "./config.js";
import { createMcpIntegration } from "./mcp.js";
import { loadAgentServiceProviders } from "./service-providers.js";

interface NodeProcess {
  readonly env: Readonly<Record<string, string | undefined>>;
  readonly stderr: { write(chunk: string): boolean };
  exitCode: number;
  once(signal: "SIGINT" | "SIGTERM", listener: () => void): void;
}

function runtime(): NodeProcess {
  const scope = globalThis as { readonly process?: NodeProcess };
  if (scope.process === undefined) throw new AgentIntegrationError("client-runtime-refused");
  return scope.process;
}

function describeFailure(error: unknown): string {
  if (error instanceof AgentIntegrationError) return error.code;
  return error instanceof Error ? error.name : "unknown-failure";
}

async function main(): Promise<void> {
  const host = runtime();
  const providers = await loadAgentServiceProviders(host.env);
  let integration: ReturnType<typeof createMcpIntegration> | undefined;
  let closing: Promise<void> | undefined;
  const close = (): Promise<void> => {
    closing ??= Promise.resolve().then(async () => {
      try { await integration?.closeMcp(); }
      finally {
        try { integration?.destroy(); }
        finally { await providers.destroy?.(); }
      }
    });
    return closing;
  };
  const shutdown = (): void => {
    void close().catch((error: unknown) => {
      host.stderr.write(`layerx-mcp-server: ${describeFailure(error)}\n`);
      host.exitCode = 1;
    });
  };
  try {
    integration = createMcpIntegration({ environment: host.env, ...providers });
    integration.officialServer.protocolServer.onclose = shutdown;
    host.once("SIGINT", shutdown);
    host.once("SIGTERM", shutdown);
    await integration.connectStdio();
  } catch (error) {
    await close();
    throw error;
  }
}

main().catch((error: unknown) => {
  const host = runtime();
  host.stderr.write(`layerx-mcp-server: ${describeFailure(error)}\n`);
  host.exitCode = 1;
});
