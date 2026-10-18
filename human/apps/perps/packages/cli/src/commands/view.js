const { heading, info, error, getErrorMessage } = require("../shared");
const { fetchWithTimeout, getHttpTimeoutMs } = require("../internal/auth");
const { getMarketplaceApiPluginUrl } = require("../internal/constants");

module.exports = {
  command: "view <id>",
  describe: "View plugin details by ID from Marketplace (JSON output)",
  builder: (yargs) => {
    return yargs
      .positional("id", {
        type: "string",
        describe:
          "string; plugin ID used to fetch details from Marketplace (required)",
        demandOption: true,
      })
      .example(
        "paxeer-network-devkit view trading-plugin-id",
        "Fetch and print plugin details as JSON",
      );
  },
  handler: async (argv) => {
    if (process.stdout.isTTY) {
      heading("Marketplace Plugin Details");
    }

    const pluginId = String(argv.id || "").trim();
    if (!pluginId) {
      error("Plugin ID is required.");
      process.exitCode = 1;
      return;
    }

    const url = getMarketplaceApiPluginUrl(pluginId);

    try {
      const headers = new Headers({ Accept: "application/json" });
      const response = await fetchWithTimeout(
        url,
        { method: "GET", headers },
        getHttpTimeoutMs(),
      );

      const responseData = await response.json().catch(() => null);

      if (response.status === 404) {
        error(`Plugin not found: ${pluginId}`);
        process.exitCode = 1;
        return;
      }

      if (!response.ok) {
        const serverMessage = getErrorMessage(responseData, response.status);
        error(`Failed to fetch plugin: ${serverMessage}`);
        process.exitCode = 1;
        return;
      }

      console.log(JSON.stringify(responseData ?? {}, null, 2));
    } catch (e) {
      const cause = e?.message || String(e);
      error(`Request failed while calling ${url}: ${cause}`);
      info("Please verify network connectivity and API availability.");
      process.exitCode = 1;
    }
  },
};
