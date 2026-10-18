const { heading, info, error, success, getErrorMessage } = require("../shared");
const { requireAuth, authenticatedFetch } = require("../internal/auth");
const {
  CLI_BIN_NAME,
  MARKETPLACE_API_MY_PLUGINS_URL,
} = require("../internal/constants");
const {
  normalizePlugins,
  truncate,
  renderTable,
} = require("../internal/formatting");

const LIST_COLUMNS = {
  headers: ["ID", "Name", "Version", "Status", "Audit", "Description"],
  keys: ["id", "name", "version", "status", "audit", "description"],
};

/**
 * Summarize the latest audit job so authors can follow an in-flight review
 * without opening the web console.
 */
function formatAudit(plugin) {
  const audit = plugin?.latestAudit;
  if (!audit) {
    return "-";
  }
  const status = audit.status || "unknown";
  return audit.verdict ? `${status}/${audit.verdict}` : status;
}

function mapRow(plugin) {
  return {
    id: plugin?.id || plugin?.pluginId || "-",
    name: plugin?.name || plugin?.npmName || "-",
    version: plugin?.version || plugin?.latestVersion || "-",
    status: plugin?.status || "-",
    audit: formatAudit(plugin),
    description: truncate(plugin?.description || "-", 56),
  };
}

module.exports = {
  command: "list",
  describe: "List my submitted plugins from Marketplace",
  builder: (yargs) => {
    return yargs
      .option("json", {
        type: "boolean",
        describe:
          "boolean; when true, output the raw marketplace response as JSON (otherwise prints a compact table)",
        default: false,
      })
      .example(
        "paxeer-network-devkit list",
        "List your submitted plugins as a compact table",
      )
      .example(
        "paxeer-network-devkit list --json",
        "Output the raw JSON payload (useful for debugging/agents)",
      );
  },
  handler: async (argv) => {
    if (!argv.json) {
      heading("My Plugins");
    }

    if (!requireAuth()) {
      return;
    }

    try {
      const response = await authenticatedFetch(
        MARKETPLACE_API_MY_PLUGINS_URL,
        {
          method: "GET",
          headers: { Accept: "application/json" },
        },
      );

      const responseData = await response.json().catch(() => null);

      if (!response.ok) {
        const serverMessage = getErrorMessage(responseData, response.status);
        error(`Failed to fetch plugins: ${serverMessage}`);
        process.exitCode = 1;
        return;
      }

      const plugins = normalizePlugins(responseData);

      if (argv.json) {
        console.log(JSON.stringify(responseData, null, 2));
        return;
      }

      if (plugins.length === 0) {
        info("You have not submitted any plugins yet.");
        info(
          `If Marketplace Web shows records, run \`${CLI_BIN_NAME} whoami\` to confirm account consistency and \`${CLI_BIN_NAME} list --json\` to inspect the raw API response.`,
        );
        return;
      }

      const rows = plugins.map(mapRow);
      console.log(renderTable(rows, LIST_COLUMNS));
      success(`\nTotal: ${plugins.length} plugin(s)`);
    } catch (e) {
      const cause = e?.message || String(e);
      error(
        `Request failed while calling ${MARKETPLACE_API_MY_PLUGINS_URL}: ${cause}`,
      );
      info("Please verify network connectivity and API availability.");
      process.exitCode = 1;
    }
  },
};
