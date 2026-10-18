import { defaultClientAdapterRegistry } from "./clients.js";
import { backupIfExists, readJsonConfig, writeJsonAtomic } from "./fs-write.js";
import { printInstallReport } from "./report.js";
import { buildServerEntry } from "./spec.js";
import type {
  InstallClient,
  InstallOptions,
  InstallReport,
  InstallResult,
  InstallScope,
} from "./types.js";

const ALL_CLIENTS: InstallClient[] = ["claude", "codex", "cursor", "opencode"];

/** Reject characters that break npx/npm package@version parsing or shell wrappers. */
function assertSafeSdkDocsVersionSpecifier(raw: string): void {
  if (!raw.trim()) {
    throw new Error("Empty --sdk-docs-version");
  }
  // No whitespace, quotes, or backslashes — keeps npx argv and merged JSON stable.
  if (/[\s'"\\]/.test(raw)) {
    throw new Error(
      `Invalid --sdk-docs-version: "${raw}" (no spaces, quotes, or backslashes)`,
    );
  }
}

const MCP_INSTALL_DEBUG =
  process.env.PAXEER_NETWORK_MCP_INSTALL_DEBUG === "1" ||
  process.env.PAXEER_NETWORK_MCP_INSTALL_DEBUG === "true";

/** stderr-only when PAXEER_NETWORK_MCP_INSTALL_DEBUG is set */
function debugInstallRun(...args: unknown[]): void {
  if (!MCP_INSTALL_DEBUG) return;
  console.error("[sdk-docs runInstallCommand:debug]", ...args);
}

/**
 * Parse install options from argv list (excluding node/script path).
 */
export function parseInstallArgs(argv: string[]): InstallOptions {
  const getValue = (flag: string): string | undefined => {
    const index = argv.indexOf(flag);
    if (index === -1) return undefined;
    return argv[index + 1];
  };

  const clientArg = getValue("--client") ?? "all";
  const scopeArg = (getValue("--scope") ?? "user") as InstallScope;
  const nameArg = getValue("--name") ?? "paxeer-network-sdk-docs";
  const sdkDocsVerRaw = getValue("--sdk-docs-version");

  if (scopeArg !== "user" && scopeArg !== "project") {
    throw new Error(`Invalid --scope: ${scopeArg}`);
  }

  const clients =
    clientArg === "all"
      ? ALL_CLIENTS
      : (clientArg
          .split(",")
          .map((x) => x.trim())
          .filter(Boolean) as InstallClient[]);

  for (const c of clients) {
    if (!ALL_CLIENTS.includes(c)) {
      throw new Error(`Invalid --client value: ${c}`);
    }
  }

  let sdkDocsVersion: string | undefined;
  if (sdkDocsVerRaw !== undefined) {
    const trimmed = sdkDocsVerRaw.trim();
    if (trimmed.length > 0) {
      assertSafeSdkDocsVersionSpecifier(trimmed);
      sdkDocsVersion = trimmed;
    }
  }

  return {
    clients,
    scope: scopeArg,
    dryRun: argv.includes("--dry-run"),
    force: argv.includes("--force"),
    name: nameArg,
    sdkDocsVersion,
  };
}

/**
 * Execute config installation for all requested clients.
 */
export function runInstallCommand(
  options: InstallOptions,
  cwd = process.cwd(),
): InstallReport {
  debugInstallRun(
    "cwd",
    cwd,
    "HOME",
    process.env.HOME,
    "USERPROFILE",
    process.env.USERPROFILE,
  );
  debugInstallRun("options", options);

  const entry = buildServerEntry({
    name: options.name,
    sdkDocsVersion: options.sdkDocsVersion,
  });
  const results: InstallResult[] = [];
  const errors: string[] = [];

  const phasePrefix = options.dryRun ? "[dry-run]" : "[install]";
  for (const client of options.clients) {
    const adapter = defaultClientAdapterRegistry.get(client);
    if (!adapter) {
      errors.push(`${client}: no adapter registered for this client`);
      continue;
    }
    const configPath = adapter.resolvePath(options.scope, cwd);

    console.warn(
      `${phasePrefix} merging ${client} (${options.scope} scope) -> ${configPath}`,
    );

    try {
      const rawConfig = readJsonConfig(configPath);
      const beforeExists = Object.keys(rawConfig).length > 0;
      const merged = adapter.mergeConfig(
        rawConfig,
        options.name,
        entry,
        options.force,
      );

      let backupPath: string | undefined;
      if (merged.action === "updated" && !options.dryRun) {
        backupPath = backupIfExists(configPath);
        writeJsonAtomic(configPath, merged.merged);
      }

      results.push({
        client,
        path: configPath,
        action:
          merged.action === "noop"
            ? "noop"
            : beforeExists
              ? "updated"
              : "created",
        backupPath,
        changed: merged.action === "updated",
      });
    } catch (error) {
      errors.push(
        `${client} (${configPath}): ${error instanceof Error ? error.message : String(error)}`,
      );
    }
  }

  const report: InstallReport = {
    ok: errors.length === 0,
    results,
    errors,
  };

  printInstallReport(report, options.dryRun);
  return report;
}
