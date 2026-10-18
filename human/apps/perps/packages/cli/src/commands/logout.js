const { heading, info, success, warn } = require("../shared");
const {
  isLoggedIn,
  logout,
  getRefreshToken,
  fetchWithTimeout,
} = require("../internal/auth");
const { AUTH_API_BASE_URL } = require("../internal/constants");

/** Short timeout for the best-effort server-side revoke; logout must not hang. */
const REVOKE_TIMEOUT_MS = 5000;

/**
 * Ask the marketplace to revoke the server-side CLI session. Best-effort:
 * any failure keeps the local logout successful, the session simply expires
 * on its own later.
 * @param {string} refreshToken
 * @returns {Promise<boolean>} true when the server confirmed revocation
 */
async function revokeServerSession(refreshToken) {
  try {
    const response = await fetchWithTimeout(
      `${AUTH_API_BASE_URL}/revoke-cli`,
      {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Accept: "application/json",
        },
        body: JSON.stringify({ refreshToken }),
      },
      REVOKE_TIMEOUT_MS,
    );
    const data = await response.json().catch(() => null);
    return response.ok && data?.revoked === true;
  } catch {
    return false;
  }
}

module.exports = {
  command: "logout",
  describe: "Logout from paxeer-network Marketplace",
  handler: async () => {
    heading("Logout from paxeer-network Marketplace");

    if (!isLoggedIn()) {
      warn("You are not logged in.");
      return;
    }

    info("This command will clear your stored authentication.\n");

    const refreshToken = getRefreshToken();

    // Always clear local credentials first so logout succeeds offline.
    logout();

    if (refreshToken) {
      info("Revoking the server-side session...");
      const revoked = await revokeServerSession(refreshToken);
      if (revoked) {
        success("Logout successful!");
        console.log("Your session has been cleared and revoked on the server.");
      } else {
        success("Logout successful!");
        warn(
          "Could not revoke the server-side session. It will expire on its own.",
        );
      }
    } else {
      success("Logout successful!");
      console.log("Your session has been cleared.");
    }
  },
};
