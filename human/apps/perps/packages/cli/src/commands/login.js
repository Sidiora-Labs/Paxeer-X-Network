const crypto = require("crypto");
const { exec } = require("child_process");
const { heading, info, success, warn, error } = require("../shared");
const { saveOAuthToken, isLoggedIn } = require("../internal/auth");
const { startCallbackServer } = require("../internal/login-server");
const {
  CLI_BIN_NAME,
  CLI_CALLBACK_PORT,
  CLI_LOGIN_TIMEOUT_MS,
  MARKETPLACE_WEB_LOGIN_URL,
} = require("../internal/constants");

function openBrowser(url) {
  const platform = process.platform;
  let command;
  if (platform === "darwin") {
    command = `open "${url}"`;
  } else if (platform === "win32") {
    command = `start "" "${url}"`;
  } else {
    command = `xdg-open "${url}"`;
  }
  return new Promise((resolve, reject) => {
    exec(command, (err) => {
      if (err) reject(err);
      else resolve();
    });
  });
}

module.exports = {
  command: "login",
  describe: "Login to paxeer-network Marketplace via wallet",
  builder: (yargs) => {
    return yargs
      .option("port", {
        alias: "p",
        type: "number",
        describe:
          "number; available local callback port, an integer from 9876 to 9899. Default: 9876",
        default: CLI_CALLBACK_PORT,
      })
      .option("force", {
        alias: "f",
        type: "boolean",
        describe:
          "boolean; if true, force re-login even if you're already logged in",
        default: false,
      })
      .example(
        `${CLI_BIN_NAME} login`,
        "Open the browser and connect your wallet",
      )
      .example(
        `${CLI_BIN_NAME} login --force --port 9877`,
        "Re-authenticate and use a custom callback port",
      );
  },
  handler: async (argv) => {
    heading("Login to paxeer-network Marketplace");
    info("This will open your browser to authenticate with your wallet.\n");

    const port = argv.port ?? CLI_CALLBACK_PORT;
    if (!Number.isInteger(port) || port < CLI_CALLBACK_PORT || port > 9899) {
      error("Login port must be an integer from 9876 to 9899.");
      process.exitCode = 1;
      return;
    }

    if (isLoggedIn() && !argv.force) {
      warn("You are already logged in.");
      info(`Use '${CLI_BIN_NAME} login --force' to re-authenticate.`);
      return;
    }

    // Generate CSRF state
    const state = crypto.randomBytes(16).toString("hex");
    const browserUrl = `${MARKETPLACE_WEB_LOGIN_URL}?port=${port}&state=${state}`;

    // Start local callback server (loopback only)
    const { server, waitForToken, cancel, listen } = startCallbackServer({
      port,
      state,
    });

    try {
      await listen();
    } catch (err) {
      error(
        err.code === "EADDRINUSE"
          ? `Port ${port} is already in use. Use --port to specify a different port.`
          : err.message,
      );
      server.close();
      process.exitCode = 1;
      return;
    }

    // Open browser
    info("Opening browser...");
    info(`  ${browserUrl}\n`);
    try {
      await openBrowser(browserUrl);
    } catch (e) {
      info("Could not open browser automatically.");
      info("Please open the URL above in your browser manually.\n");
    }

    info("Waiting for authentication...");

    let timedOut = false;
    const timeout = setTimeout(() => {
      timedOut = true;
      error("Login timed out after 3 minutes.");
      cancel(new Error("Login timed out"));
    }, CLI_LOGIN_TIMEOUT_MS);

    const onSigInt = () => {
      info("\nLogin cancelled.");
      clearTimeout(timeout);
      cancel(new Error("Login cancelled"));
      process.exit(130);
    };
    process.on("SIGINT", onSigInt);

    try {
      const tokenData = await waitForToken;

      saveOAuthToken(tokenData);

      success("Login successful!");
      const displayName = tokenData.email || tokenData.username;
      if (displayName) {
        info(`Logged in as: ${displayName}`);
      }
      info(`Use '${CLI_BIN_NAME} whoami' to verify your account.`);
    } catch (err) {
      if (!timedOut) {
        error(`Login failed: ${err.message}`);
      }
      process.exitCode = 1;
    } finally {
      clearTimeout(timeout);
      process.removeListener("SIGINT", onSigInt);
      server.close();
    }
  },
};
