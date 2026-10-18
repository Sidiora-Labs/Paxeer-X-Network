const {
  heading,
  info,
  warn,
  error,
  success,
  select,
  getApiErrorInfo,
} = require("../shared");
const { requireAuth, authenticatedFetch } = require("../internal/auth");
const {
  CLI_BIN_NAME,
  MARKETPLACE_API_MY_PLUGINS_URL,
  getMarketplaceApiPluginUrl,
} = require("../internal/constants");
const { normalizePlugins } = require("../internal/formatting");

/**
 * Format a compact prompt label so users can identify the target plugin quickly.
 * @param {Record<string, unknown>} plugin
 * @returns {string}
 */
function toPluginChoiceLabel(plugin) {
  const name = plugin?.name || plugin?.npmName || "Unknown";
  const id = plugin?.id || plugin?.pluginId || "unknown-id";
  const status = plugin?.status || "unknown";
  return `${name} (${id}) [${status}]`;
}

module.exports = {
  command: "disable",
  describe: "Delist one of your submitted plugins from Marketplace",
  builder: (yargs) => {
    return yargs
      .option("pluginId", {
        type: "string",
        describe:
          "string; plugin ID to delist directly. If omitted, an interactive selector is shown",
        demandOption: false,
      })
      .example(
        "paxeer-network-devkit disable",
        "Select one of your plugins and delist it",
      )
      .example(
        "paxeer-network-devkit disable --pluginId my-plugin-id",
        "Delist a specific plugin directly without selection",
      );
  },
  handler: async (argv) => {
    heading("Disable My Plugin");
    info("This command will delist one of your submitted plugins.\n");

    if (!requireAuth()) {
      return;
    }

    try {
      let selectedPluginId =
        typeof argv.pluginId === "string" ? argv.pluginId.trim() : "";
      if (!selectedPluginId) {
        info("Fetching your submitted plugins...");
        const listResponse = await authenticatedFetch(
          MARKETPLACE_API_MY_PLUGINS_URL,
          {
            method: "GET",
            headers: { Accept: "application/json" },
          },
        );

        const listData = await listResponse.json().catch(() => null);
        if (!listResponse.ok) {
          const { message } = getApiErrorInfo(listData, listResponse.status);
          error(`Failed to fetch plugins: ${message}`);
          process.exitCode = 1;
          return;
        }

        const plugins = normalizePlugins(listData).filter(
          (plugin) => plugin?.status === "available",
        );
        if (plugins.length === 0) {
          info("You have no available plugins to delist.");
          info(
            `If Marketplace Web shows records, run \`${CLI_BIN_NAME} whoami\` to confirm account consistency and \`${CLI_BIN_NAME} list --json\` to inspect the raw API response.`,
          );
          return;
        }

        const choices = plugins
          .map((plugin) => ({
            name: String(plugin?.id || plugin?.pluginId || ""),
            message: toPluginChoiceLabel(plugin),
            value: String(plugin?.id || plugin?.pluginId || ""),
          }))
          .filter((choice) => Boolean(choice.value));

        if (choices.length === 0) {
          error("No valid plugin IDs found in marketplace response.");
          process.exitCode = 1;
          return;
        }

        selectedPluginId = await select(
          "Select a plugin to delist (set status to deprecated):",
          choices,
          0,
        );
      } else {
        info(`Using pluginId from command argument: ${selectedPluginId}`);
      }

      if (!selectedPluginId) {
        warn("No plugin selected.");
        process.exitCode = 1;
        return;
      }

      const endpoint = getMarketplaceApiPluginUrl(selectedPluginId);
      info(`Submitting delist request to ${endpoint} ...`);
      const disableResponse = await authenticatedFetch(endpoint, {
        method: "PATCH",
        headers: {
          "Content-Type": "application/json",
          Accept: "application/json",
        },
        body: JSON.stringify({ status: "deprecated" }),
      });

      const disableData = await disableResponse.json().catch(() => null);
      if (!disableResponse.ok) {
        const { code, message } = getApiErrorInfo(
          disableData,
          disableResponse.status,
        );
        error(`Failed to delist plugin: ${message}`);
        if (code) {
          info(`Error code: ${code}`);
        }
        info("Please verify plugin ownership and API availability.");
        process.exitCode = 1;
        return;
      }

      const updatedName = disableData?.name || disableData?.npmName || "N/A";
      const updatedStatus = disableData?.status || "deprecated";
      success("\nPlugin delisted successfully.");
      info(`Plugin ID: ${selectedPluginId}`);
      info(`Plugin Name: ${updatedName}`);
      info(`Status: ${updatedStatus}`);
    } catch (e) {
      const cause = e?.message || String(e);
      error(`Request failed while calling marketplace APIs: ${cause}`);
      info("Please verify network connectivity and API availability.");
      process.exitCode = 1;
    }
  },
};
