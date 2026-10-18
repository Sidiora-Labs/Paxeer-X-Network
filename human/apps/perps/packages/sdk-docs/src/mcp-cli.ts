#!/usr/bin/env node
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { loadBundle } from "./bundle.js";
import { errResult } from "./envelope.js";
import { createAiDocsFacade, type AiDocsFacade } from "./facade.js";
import {
  parseInstallArgs,
  runInstallCommand,
} from "./install/install.command.js";
import { registerDocsTools } from "./mcp-tools/registerDocsTools.js";

let facade: AiDocsFacade | null = null;
let loadError: string | null = null;

try {
  facade = createAiDocsFacade(loadBundle());
} catch (e) {
  loadError = e instanceof Error ? e.message : String(e);
}

function guardFacade():
  | { ok: true; facade: AiDocsFacade }
  | { ok: false; payload: string } {
  if (!facade || loadError) {
    return {
      ok: false,
      payload: JSON.stringify(
        errResult(
          "INVALID_INDEX_SHAPE",
          loadError ?? "SDK docs bundle failed to load.",
          "Set PAXEER_NETWORK_AI_DOCS_REPO_ROOT or run pnpm --filter @paxeer-network/ai-docs generate, then pnpm --filter @paxeer-network/sdk-docs sync:bundle.",
          null,
        ),
        null,
        2,
      ),
    };
  }
  return { ok: true, facade };
}

const server = new McpServer({
  name: "paxeer-network-sdk-docs",
  version: "0.1.0",
});

registerDocsTools(server, { guardFacade });

const MCP_INSTALL_DEBUG =
  process.env.PAXEER_NETWORK_MCP_INSTALL_DEBUG === "1" ||
  process.env.PAXEER_NETWORK_MCP_INSTALL_DEBUG === "true";

/** stderr-only diagnostics when PAXEER_NETWORK_MCP_INSTALL_DEBUG is set */
function debugInstall(...args: unknown[]): void {
  if (!MCP_INSTALL_DEBUG) return;
  console.error("[paxeer-network-sdk-docs-mcp install:debug]", ...args);
}

/**
 * Resolve argv after `node …/mcp-cli.js …` so the install subcommand is argv[0].
 * npx/npm often passes the shim path as the first token (e.g. `.bin/paxeer-network-sdk-docs-mcp`),
 * so checking only argv[0] === "install" misses install and we fall through to MCP stdio —
 * the child never exits and paxeer-network-devkit appears hung after spawning npx.
 */
function argvStartingAtInstall(argv: string[]): string[] | null {
  const idx = argv.indexOf("install");
  if (idx === -1) return null;
  return argv.slice(idx);
}

/**
 * Run MCP install flow when user executes:
 * `paxeer-network-sdk-docs-mcp install ...`
 */
async function maybeRunInstallCommand(): Promise<boolean> {
  const sliced = process.argv.slice(2);
  debugInstall("process.argv.slice(2)", sliced);

  const argv = argvStartingAtInstall(sliced);
  debugInstall("argvStartingAtInstall", argv);

  if (!argv || argv[0] !== "install") {
    debugInstall(
      "not entering install branch (expected literal subcommand install)",
    );
    return false;
  }

  console.warn("Starting MCP config installation...");
  const options = parseInstallArgs(argv.slice(1));
  const report = runInstallCommand(options);
  if (!report.ok) {
    process.exitCode = 1;
  }
  return true;
}

async function main() {
  const handledInstall = await maybeRunInstallCommand();
  if (handledInstall) return;
  const transport = new StdioServerTransport();
  await server.connect(transport);
}

main().catch((err: unknown) => {
  console.error(err);
  process.exit(1);
});
