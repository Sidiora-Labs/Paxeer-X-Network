#!/usr/bin/env node

const fs = require("fs");
const path = require("path");
const yargs = require("yargs/yargs");
const { hideBin } = require("yargs/helpers");
const chalk = require("chalk");

// Resolve commands directory relative to this file's location
const commandsDir = path.resolve(__dirname, "../src/commands");

/**
 * Detect known enquirer cancel crash on newer Node runtimes.
 * This keeps Ctrl+C behavior user-friendly while preserving real errors.
 * @param {unknown} err
 * @returns {boolean}
 */
function isEnquirerReadlineCancelError(err) {
  if (!err || typeof err !== "object") {
    return false;
  }

  const error =
    /** @type {{ code?: string; stack?: string; message?: string }} */ (err);
  const stack = typeof error.stack === "string" ? error.stack : "";
  const message = typeof error.message === "string" ? error.message : "";

  return (
    error.code === "ERR_USE_AFTER_CLOSE" &&
    message.includes("readline") &&
    stack.includes("enquirer/lib/")
  );
}

/**
 * Gracefully handle Ctrl+C prompt cancellation emitted by enquirer.
 * Only swallows the known cancellation crash and rethrows everything else.
 */
function setupGracefulPromptInterruptHandling() {
  process.on("uncaughtException", (err) => {
    if (!isEnquirerReadlineCancelError(err)) {
      throw err;
    }

    console.log();
    console.log(chalk.yellow("Operation cancelled."));
    process.exit(130);
  });
}

/**
 * Ensure process cwd is valid before yargs initialization.
 * Yargs reads cwd eagerly; when users run CLI from a deleted directory,
 * Node throws ENOENT before command parsing starts.
 */
function ensureValidWorkingDirectory() {
  const cwd = process.cwd();
  try {
    fs.accessSync(cwd);
    return;
  } catch {
    const fallbackDir =
      process.env.HOME ||
      process.env.USERPROFILE ||
      path.resolve(__dirname, "..");

    try {
      fs.accessSync(fallbackDir);
      process.chdir(fallbackDir);
      console.warn(
        chalk.yellow(
          `Current working directory is unavailable. Switched to: ${fallbackDir}`,
        ),
      );
    } catch {
      // Fallback also unavailable — let the error surface naturally.
      throw new Error(
        `Current working directory "${cwd}" is inaccessible. ` +
          `Please change to a valid directory and try again.`,
      );
    }
  }
}

setupGracefulPromptInterruptHandling();
ensureValidWorkingDirectory();

yargs(hideBin(process.argv))
  .scriptName("paxeer-network-devkit")
  .usage(chalk.cyan("Usage:") + " $0 <command> [options]")
  .alias("v", "version")
  .alias("h", "help")
  .showHelpOnFail(true)
  .strictCommands()
  .demandCommand(1, "Please specify a command.")
  .command("create", "Create a new plugin or module", (yargs) => {
    return yargs.commandDir(path.join(commandsDir, "create"), {
      exclude: /\.test\.js$/,
    });
  })
  // co-located test files are not commands; requiring them would run their
  // node:test suites inside the CLI process
  .commandDir(commandsDir, { exclude: /\.test\.js$/ })
  .recommendCommands()
  .fail((message, error, parser) => {
    // Always show usage/help for missing or invalid command input.
    parser.showHelp();
    if (message) {
      console.error(chalk.red(`\n${message}`));
    }
    if (error && !message) {
      console.error(chalk.red(`\n${error.message}`));
    }
    process.exit(1);
  })
  .parse();
