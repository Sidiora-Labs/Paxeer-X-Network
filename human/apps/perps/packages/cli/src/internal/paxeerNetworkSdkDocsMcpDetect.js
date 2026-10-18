const fs = require("fs");
const path = require("path");
const { dim } = require("../shared");
const { DEFAULT_PAXEER_NETWORK_SDK_DOCS_MCP_NAME } = require("./constants");

// Config paths per agent — keep in sync with packages/sdk-docs/src/install/clients.ts.

const ALL_CLIENTS = /** @type {const} */ ([
  "claude",
  "codex",
  "cursor",
  "opencode",
]);

const CANONICAL_COMMAND = "npx";
const CANONICAL_ARGS = [
  "-y",
  "@paxeer-network/sdk-docs",
  "paxeer-network-sdk-docs-mcp",
];

/**
 * @returns {Record<string, { user: string; project: string }>}
 */
function getClientPaths() {
  const home = process.env.HOME || process.env.USERPROFILE || "";
  return {
    claude: {
      user: path.join(home, ".claude.json"),
      project: ".claude.json",
    },
    codex: {
      user: path.join(home, ".codex", "config.json"),
      project: ".codex/config.json",
    },
    cursor: {
      user: path.join(home, ".cursor", "mcp.json"),
      project: ".cursor/mcp.json",
    },
    opencode: {
      user: path.join(home, ".opencode", "config.json"),
      project: ".opencode/config.json",
    },
  };
}

/**
 * Same semantics as sdk-docs readJsonConfig: missing/empty → {}.
 * @param {string} configPath
 * @returns {{ ok: true; data: Record<string, unknown> } | { ok: false; error: string }}
 */
function readJsonConfigSafe(configPath) {
  if (!fs.existsSync(configPath)) {
    return { ok: true, data: {} };
  }
  const rawText = fs.readFileSync(configPath, "utf8").trim();
  if (!rawText) {
    return { ok: true, data: {} };
  }
  try {
    const parsed = JSON.parse(rawText);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return { ok: false, error: "Root value must be an object" };
    }
    return { ok: true, data: /** @type {Record<string, unknown>} */ (parsed) };
  } catch (e) {
    return {
      ok: false,
      error: e instanceof Error ? e.message : String(e),
    };
  }
}

/**
 * @param {unknown} args
 * @returns {boolean}
 */
function argsMatchCanonical(args) {
  if (!Array.isArray(args)) {
    return false;
  }
  return JSON.stringify(args) === JSON.stringify(CANONICAL_ARGS);
}

/**
 * Strict match for the default server key (same as sdk-docs merge noop check).
 * @param {unknown} entry
 * @returns {boolean}
 */
function entryMatchesCanonical(entry) {
  if (!entry || typeof entry !== "object" || Array.isArray(entry)) {
    return false;
  }
  const o = /** @type {Record<string, unknown>} */ (entry);
  return o.command === CANONICAL_COMMAND && argsMatchCanonical(o.args);
}

/**
 * Loose match for custom --name installs.
 * @param {unknown} entry
 * @returns {boolean}
 */
function entryMatchesFuzzyPaxeerNetwork(entry) {
  if (!entry || typeof entry !== "object" || Array.isArray(entry)) {
    return false;
  }
  const o = /** @type {Record<string, unknown>} */ (entry);
  if (o.command !== "npx") {
    return false;
  }
  const args = o.args;
  if (!Array.isArray(args)) {
    return false;
  }
  return (
    args.includes("paxeer-network-sdk-docs-mcp") &&
    args.includes("@paxeer-network/sdk-docs")
  );
}

/**
 * @param {Record<string, unknown>} data
 * @param {string} defaultName
 * @returns {{ configured: boolean; serverKey?: string }}
 */
function analyzeMcpServers(data, defaultName) {
  const mcpServersRaw = data.mcpServers;
  const mcpServers =
    mcpServersRaw &&
    typeof mcpServersRaw === "object" &&
    !Array.isArray(mcpServersRaw)
      ? /** @type {Record<string, unknown>} */ (mcpServersRaw)
      : {};

  const named = mcpServers[defaultName];
  if (named && entryMatchesCanonical(named)) {
    return { configured: true, serverKey: defaultName };
  }

  for (const [key, val] of Object.entries(mcpServers)) {
    if (entryMatchesFuzzyPaxeerNetwork(val)) {
      return { configured: true, serverKey: key };
    }
  }
  return { configured: false };
}

/**
 * @param {string} configPath
 * @param {string} defaultName
 * @returns {{
 *   configPath: string;
 *   exists: boolean;
 *   configured: boolean;
 *   serverKey?: string;
 *   error?: string;
 * }}
 */
function inspectConfigFile(configPath, defaultName) {
  const exists = fs.existsSync(configPath);
  if (!exists) {
    return { configPath, exists: false, configured: false };
  }
  const read = readJsonConfigSafe(configPath);
  if (!read.ok) {
    return {
      configPath,
      exists: true,
      configured: false,
      error: read.error,
    };
  }
  const { configured, serverKey } = analyzeMcpServers(read.data, defaultName);
  return {
    configPath,
    exists: true,
    configured,
    ...(serverKey ? { serverKey } : {}),
  };
}

/**
 * @param {string} cwd
 * @param {{ serverName?: string; clients?: string[] }} [options]
 */
function getPaxeerNetworkSdkDocsMcpReport(cwd, options = {}) {
  const resolvedCwd = path.resolve(cwd);
  const defaultName = options.serverName || DEFAULT_PAXEER_NETWORK_SDK_DOCS_MCP_NAME;
  const paths = getClientPaths();
  const want =
    options.clients && options.clients.length > 0
      ? ALL_CLIENTS.filter((c) => options.clients.includes(c))
      : ALL_CLIENTS;

  /** @type {Record<string, { user: ReturnType<typeof inspectConfigFile>; project: ReturnType<typeof inspectConfigFile> }>} */
  const clients = {};

  for (const client of want) {
    const p = paths[client];
    clients[client] = {
      user: inspectConfigFile(p.user, defaultName),
      project: inspectConfigFile(
        path.join(resolvedCwd, p.project),
        defaultName,
      ),
    };
  }

  return {
    cwd: resolvedCwd,
    defaultServerName: defaultName,
    clients,
  };
}

/**
 * True if any checked user or project config for this cwd shows paxeer-network SDK Docs MCP.
 * @param {string} cwd
 * @param {{ serverName?: string; clients?: string[] }} [options]
 * @returns {boolean}
 */
function isPaxeerNetworkSdkDocsMcpConfiguredAnywhere(cwd, options = {}) {
  const report = getPaxeerNetworkSdkDocsMcpReport(cwd, options);
  for (const row of Object.values(report.clients)) {
    if (row.user.configured || row.project.configured) {
      return true;
    }
  }
  return false;
}

/**
 * One-time style hint after scaffold/submit when MCP is missing (opt-out via env).
 * @param {string} cwd
 */
function maybePrintPaxeerNetworkDevEnvironmentHints(cwd) {
  if (process.env.PAXEER_NETWORK_DEVKIT_NO_ENV_HINTS) {
    return;
  }
  if (isPaxeerNetworkSdkDocsMcpConfiguredAnywhere(cwd)) {
    return;
  }
  console.log();
  dim("Tip: paxeer-network SDK Docs MCP was not detected in your agent config files.");
  dim("  Install: paxeer-network-devkit mcp install");
  dim("  Verify:  paxeer-network-devkit mcp detect");
  dim("  Skills:  paxeer-network-devkit skills install");
}

module.exports = {
  ALL_CLIENTS,
  getPaxeerNetworkSdkDocsMcpReport,
  isPaxeerNetworkSdkDocsMcpConfiguredAnywhere,
  maybePrintPaxeerNetworkDevEnvironmentHints,
};
