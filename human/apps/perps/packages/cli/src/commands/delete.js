const { heading, info, warn } = require("../shared");
const {
  CLI_BIN_NAME,
  MARKETPLACE_WEB_LISTINGS_URL,
} = require("../internal/constants");

module.exports = {
  command: "delete",
  describe:
    "(Removed) Use 'disable' to delist or manage listings in the web console",
  handler: async () => {
    heading("Delete My Plugin");
    warn(
      "Plugin deletion is no longer available through the CLI API. " +
        "The new marketplace does not expose a delete endpoint.",
    );
    info(`To delist a plugin instead, run '${CLI_BIN_NAME} disable'.`);
    info(
      `Manage your plugins in the web console: ${MARKETPLACE_WEB_LISTINGS_URL}`,
    );
    process.exitCode = 1;
  },
};
