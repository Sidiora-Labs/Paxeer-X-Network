import type { McpServerConfigEntry } from "./types_internal.js";

export type InstallClient = "claude" | "codex" | "cursor" | "opencode";
export type InstallScope = "user" | "project";

export type InstallOptions = {
  clients: InstallClient[];
  scope: InstallScope;
  dryRun: boolean;
  force: boolean;
  name: string;
  /** npm version / dist-tag for @paxeer-network/sdk-docs (npx and merged MCP args). */
  sdkDocsVersion?: string;
};

export type InstallClientTarget = {
  client: InstallClient;
  path: string;
};

export type InstallResult = {
  client: InstallClient;
  path: string;
  action: "created" | "updated" | "noop";
  backupPath?: string;
  changed: boolean;
};

export type InstallReport = {
  ok: boolean;
  results: InstallResult[];
  errors: string[];
};

export type BuildEntryOptions = {
  name: string;
  /** When set, MCP entry uses `@paxeer-network/sdk-docs@<version>` for reproducible installs. */
  sdkDocsVersion?: string;
};

export type ClientAdapter = {
  client: InstallClient;
  resolvePath(scope: InstallScope, cwd: string): string;
  mergeConfig(
    raw: Record<string, unknown>,
    entryName: string,
    entry: McpServerConfigEntry,
    force: boolean,
  ): { merged: Record<string, unknown>; action: "updated" | "noop" };
};

export type ClientAdapterRegistry = {
  /** Returns a registered adapter by client id, or undefined when missing. */
  get(client: InstallClient): ClientAdapter | undefined;
  /** Registers or replaces an adapter by its client id. */
  register(adapter: ClientAdapter): void;
  /** Returns all currently registered adapters in insertion order. */
  list(): ClientAdapter[];
};
