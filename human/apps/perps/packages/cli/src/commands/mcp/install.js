const { spawnSync } = require("child_process");
const chalk = require("chalk");

/** stderr-only; set PAXEER_NETWORK_MCP_INSTALL_DEBUG=1 to trace spawn failures / hangs */
function debugMcpInstall(...parts) {
  if (
    process.env.PAXEER_NETWORK_MCP_INSTALL_DEBUG !== "1" &&
    process.env.PAXEER_NETWORK_MCP_INSTALL_DEBUG !== "true"
  ) {
    return;
  }
  console.error(chalk.dim("[paxeer-network-devkit mcp install:debug]"), ...parts);
}

/**
 * Forward MCP install to sdk-docs so install ownership stays centralized.
 * Uses an explicit bin invocation to avoid npx argument parsing ambiguity
 * across npm versions and caches.
 */
function forwardToSdkDocs(argv) {
  console.log(
    chalk.cyan("paxeer-network-devkit:") +
      " Installing paxeer-network SDK Docs MCP config via npx…",
  );
  console.log(
    chalk.dim("First run may download @paxeer-network/sdk-docs; please wait."),
  );

  /** Pinned package spec for npx -y <spec> … (must match sdk-docs install merge). */
  const rawSdkVer = argv.sdkDocsVersion ?? argv["sdk-docs-version"];
  const ver =
    rawSdkVer && String(rawSdkVer).trim() ? String(rawSdkVer).trim() : "";
  const pkgSpec = ver
    ? `@paxeer-network/sdk-docs@${ver}`
    : "@paxeer-network/sdk-docs";
  if (ver) {
    console.log(chalk.dim(`Using package: ${pkgSpec}`));
  }

  const command = "npx";
  const args = ["-y", pkgSpec, "paxeer-network-sdk-docs-mcp", "install"];

  if (argv.client) args.push("--client", String(argv.client));
  if (argv.scope) args.push("--scope", String(argv.scope));
  if (argv.name) args.push("--name", String(argv.name));
  if (ver) args.push("--sdk-docs-version", ver);
  if (argv["dry-run"]) args.push("--dry-run");
  if (argv.force) args.push("--force");

  debugMcpInstall("cwd", process.cwd());
  debugMcpInstall("spawn", command, args);

  const result = spawnSync(command, args, {
    stdio: "inherit",
    shell: process.platform === "win32",
  });

  debugMcpInstall(
    "spawnSync done",
    "status",
    result.status,
    "signal",
    result.signal,
    "error",
    result.error,
  );

  if (result.error) {
    throw result.error;
  }
  return result.status ?? 1;
}

module.exports = {
  command: "install",
  describe:
    "Install paxeer-network SDK Docs MCP config for Claude/Codex/Cursor/OpenCode",
  builder: (yargs) => {
    return yargs
      .option("client", {
        type: "string",
        describe:
          "Target client(s): claude|codex|cursor|opencode|all (comma-separated supported)",
        default: "all",
      })
      .option("scope", {
        type: "string",
        choices: ["user", "project"],
        default: "user",
      })
      .option("name", {
        type: "string",
        describe: "MCP server name in config mcpServers map",
        default: "paxeer-network-sdk-docs",
      })
      .option("dry-run", {
        type: "boolean",
        default: false,
      })
      .option("force", {
        type: "boolean",
        default: false,
      })
      .option("sdk-docs-version", {
        type: "string",
        describe:
          "Pin @paxeer-network/sdk-docs (semver or dist-tag, e.g. 0.1.0 or beta); applies to this run and merged MCP config",
      })
      .example(
        "paxeer-network-devkit mcp install --client cursor --scope project",
        "Install MCP entry for Cursor in project scope",
      )
      .example(
        "paxeer-network-devkit mcp install --sdk-docs-version 0.1.0 --scope user",
        "Pin a specific sdk-docs release for npx and editor MCP configs",
      );
  },
  handler: async (argv) => {
    const status = forwardToSdkDocs(argv);
    process.exitCode = status;
  },
};
