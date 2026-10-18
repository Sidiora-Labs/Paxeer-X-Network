import path from "node:path";
import { createClientAdapterRegistry } from "./adapterRegistry.js";
import type { ClientAdapter, InstallClient, InstallScope } from "./types.js";
import type { McpServerConfigEntry } from "./types_internal.js";

/**
 * Merge a server entry into root-level `mcpServers`.
 * Keeps unknown fields and updates only paxeer-network-owned keys.
 */
function mergeByMcpServers(
  raw: Record<string, unknown>,
  entryName: string,
  entry: McpServerConfigEntry,
  force: boolean,
): { merged: Record<string, unknown>; action: "updated" | "noop" } {
  const next = { ...raw };
  const mcpServers =
    next.mcpServers && typeof next.mcpServers === "object"
      ? { ...(next.mcpServers as Record<string, unknown>) }
      : {};

  const current = mcpServers[entryName];
  if (!force && current && typeof current === "object") {
    const currentObject = current as Record<string, unknown>;
    const commandEqual = currentObject.command === entry.command;
    const argsEqual =
      JSON.stringify(currentObject.args ?? []) ===
      JSON.stringify(entry.args ?? []);
    const envEqual =
      JSON.stringify(currentObject.env ?? {}) ===
      JSON.stringify(entry.env ?? {});

    if (commandEqual && argsEqual && envEqual) {
      return { merged: raw, action: "noop" };
    }

    mcpServers[entryName] = {
      ...currentObject,
      command: entry.command,
      args: entry.args ?? [],
      ...(entry.env ? { env: entry.env } : {}),
    };
    next.mcpServers = mcpServers;
    return { merged: next, action: "updated" };
  }

  mcpServers[entryName] = entry;
  next.mcpServers = mcpServers;
  return { merged: next, action: "updated" };
}

/**
 * Resolve a path in user or project scope for a JSON config.
 */
function byScope(
  scope: InstallScope,
  cwd: string,
  userPath: string,
  projectPath: string,
): string {
  return scope === "project" ? path.join(cwd, projectPath) : userPath;
}

function makeAdapter(
  client: InstallClient,
  userPath: string,
  projectPath: string,
): ClientAdapter {
  return {
    client,
    resolvePath(scope: InstallScope, cwd: string): string {
      return byScope(scope, cwd, userPath, projectPath);
    },
    mergeConfig(
      raw: Record<string, unknown>,
      entryName: string,
      entry: McpServerConfigEntry,
      force: boolean,
    ) {
      return mergeByMcpServers(raw, entryName, entry, force);
    },
  };
}

const home = process.env.HOME || process.env.USERPROFILE || "";

export const builtinClientAdapters: ClientAdapter[] = [
  makeAdapter("claude", path.join(home, ".claude.json"), ".claude.json"),
  makeAdapter(
    "codex",
    path.join(home, ".codex", "config.json"),
    ".codex/config.json",
  ),
  makeAdapter(
    "cursor",
    path.join(home, ".cursor", "mcp.json"),
    ".cursor/mcp.json",
  ),
  makeAdapter(
    "opencode",
    path.join(home, ".opencode", "config.json"),
    ".opencode/config.json",
  ),
];

/** Default adapter registry used by install command callers. */
export const defaultClientAdapterRegistry = createClientAdapterRegistry(
  builtinClientAdapters,
);
