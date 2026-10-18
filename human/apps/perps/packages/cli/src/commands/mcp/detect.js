const chalk = require("chalk");
const { heading, info, dim } = require("../../shared");
const {
  ALL_CLIENTS,
  getPaxeerNetworkSdkDocsMcpReport,
} = require("../../internal/paxeerNetworkSdkDocsMcpDetect");

/**
 * @param {string} clientArg
 * @returns {string[]}
 */
function parseClients(clientArg) {
  const raw = String(clientArg || "all").trim();
  if (raw === "all") {
    return [...ALL_CLIENTS];
  }
  const parts = raw
    .split(",")
    .map((x) => x.trim())
    .filter(Boolean);
  for (const p of parts) {
    if (!ALL_CLIENTS.includes(p)) {
      throw new Error(
        `Invalid --client value: ${p}. Use: ${ALL_CLIENTS.join(", ")}, or all (comma-separated).`,
      );
    }
  }
  return parts;
}

/**
 * @param {{ cwd: string; defaultServerName: string; clients: Record<string, { user: { configured: boolean }; project: { configured: boolean } }> }} report
 */
function printHumanReport(report) {
  heading("paxeer-network SDK Docs MCP — detect");
  info(`Working directory: ${report.cwd}`);
  info(`Default server name: ${report.defaultServerName}\n`);

  for (const client of Object.keys(report.clients)) {
    const row = report.clients[client];
    console.log(chalk.bold.cyan(client));
    for (const scope of /** @type {const} */ (["user", "project"])) {
      const cell = row[scope];
      const scopeLabel = scope === "user" ? "user" : "proj";
      const pathLine = chalk.dim(cell.configPath);
      if (cell.error) {
        console.log(
          `  [${scopeLabel}] ${chalk.red("invalid JSON")} — ${cell.error}`,
        );
        console.log(` ${pathLine}`);
        continue;
      }
      if (!cell.exists) {
        console.log(`  [${scopeLabel}] ${chalk.yellow("no file")}`);
        console.log(`         ${pathLine}`);
        continue;
      }
      if (cell.configured) {
        const key = cell.serverKey ? ` (${cell.serverKey})` : "";
        console.log(`  [${scopeLabel}] ${chalk.green("configured")}${key}`);
      } else {
        console.log(`  [${scopeLabel}] ${chalk.yellow("not detected")}`);
      }
      console.log(`         ${pathLine}`);
    }
    console.log();
  }

  const anyConfigured = Object.values(report.clients).some(
    (row) => row.user.configured || row.project.configured,
  );
  if (!anyConfigured) {
    dim("paxeer-network SDK Docs MCP was not found in any checked config.");
    info("Install MCP for your agent(s):");
    info("  paxeer-network-devkit mcp install");
    info("  paxeer-network-devkit mcp install --client cursor --scope project");
    info("Install plugin workflow skills for your coding agent:");
    info("  paxeer-network-devkit skills install");
  }
}

module.exports = {
  command: "detect",
  describe:
    "Detect whether paxeer-network SDK Docs MCP is present in agent config files (user + project scope)",
  builder: (yargs) => {
    return yargs
      .option("client", {
        type: "string",
        describe:
          "claude|codex|cursor|opencode|all, or comma-separated list (default: all)",
        default: "all",
      })
      .option("json", {
        type: "boolean",
        default: false,
        describe: "Print machine-readable JSON to stdout",
      })
      .example(
        "paxeer-network-devkit mcp detect",
        "Scan all supported agents (user + project configs for cwd)",
      )
      .example(
        "paxeer-network-devkit mcp detect --client cursor",
        "Only inspect Cursor mcp.json paths",
      )
      .example("paxeer-network-devkit mcp detect --json", "JSON report for scripting");
  },
  handler: async (argv) => {
    let clients;
    try {
      clients = parseClients(argv.client);
    } catch (e) {
      console.error(chalk.red(e instanceof Error ? e.message : String(e)));
      process.exitCode = 1;
      return;
    }

    const report = getPaxeerNetworkSdkDocsMcpReport(process.cwd(), { clients });

    if (argv.json) {
      console.log(JSON.stringify(report, null, 2));
      return;
    }

    printHumanReport(report);
  },
};
