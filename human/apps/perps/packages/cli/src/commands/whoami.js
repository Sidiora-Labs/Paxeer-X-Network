const { heading, info, success, warn, error } = require("../shared");
const {
  getEmail,
  isLoggedIn,
  authenticatedFetch,
} = require("../internal/auth");
const {
  CLI_BIN_NAME,
  MARKETPLACE_API_BASE_URL,
} = require("../internal/constants");

module.exports = {
  command: "whoami",
  describe: "Display current logged in user",
  builder: (yargs) => {
    return yargs.example(
      `${CLI_BIN_NAME} whoami`,
      "Show the account and confirm the session is still valid",
    );
  },
  handler: async () => {
    heading("Current User");

    if (!isLoggedIn()) {
      warn("You are not logged in.");
      info(`Run '${CLI_BIN_NAME} login' to authenticate.`);
      process.exitCode = 1;
      return;
    }

    const account = getEmail();
    success(`Logged in as: ${account || "(unknown user)"}`);

    // The credentials file only proves a token was stored once. Probe an
    // authenticated endpoint (which also triggers a refresh when needed) so an
    // expired session is not reported as active.
    const probeUrl = `${MARKETPLACE_API_BASE_URL}/submissions?limit=1`;
    try {
      const response = await authenticatedFetch(probeUrl, {
        method: "GET",
        headers: { Accept: "application/json" },
      });

      if (response.status === 401) {
        error("Your session is no longer valid.");
        info(`Run '${CLI_BIN_NAME} login --force' to authenticate again.`);
        process.exitCode = 1;
        return;
      }

      if (!response.ok) {
        warn(`Could not verify the session (HTTP ${response.status}).`);
        return;
      }

      info("Session is valid.");
    } catch (e) {
      warn(
        `Could not reach the marketplace to verify the session: ${e?.message || String(e)}`,
      );
    }
  },
};
