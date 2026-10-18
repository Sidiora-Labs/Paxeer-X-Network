const chalk = require("chalk");
const { prompt } = require("enquirer");

// Logger utilities
function log(message) {
  console.log(message);
}

function info(message) {
  console.log(chalk.blue("ℹ"), message);
}

function success(message) {
  console.log(chalk.green("✓"), message);
}

function warn(message) {
  console.log(chalk.yellow("⚠"), message);
}

function error(message) {
  console.error(chalk.red("✗"), message);
}

function heading(message) {
  console.log(chalk.bold.cyan(message));
}

function dim(message) {
  console.log(chalk.dim(message));
}

// Interactive prompts
async function input(message, initial) {
  const result = await prompt({
    type: "input",
    name: "value",
    message,
    initial: initial || "",
  });
  return result.value;
}

async function select(message, choices, initial) {
  const result = await prompt({
    type: "select",
    name: "value",
    message,
    choices,
    initial: initial || 0,
  });
  return result.value;
}

async function confirm(message) {
  const result = await prompt({
    type: "confirm",
    name: "confirm",
    message,
    initial: true,
  });
  return result.confirm;
}

/**
 * Normalize backend error payload into a human-readable message.
 * @param {unknown} responseData
 * @param {number} status
 * @returns {string}
 */
function getErrorMessage(responseData, status) {
  const candidates = [
    responseData?.message,
    responseData?.error,
    responseData?.details,
  ];

  for (const candidate of candidates) {
    if (typeof candidate === "string" && candidate.trim()) {
      return candidate;
    }

    if (Array.isArray(candidate) && candidate.length > 0) {
      const firstString = candidate.find((item) => typeof item === "string");
      if (firstString) {
        return firstString;
      }
      return JSON.stringify(candidate);
    }

    if (candidate && typeof candidate === "object") {
      if (typeof candidate.message === "string" && candidate.message.trim()) {
        return candidate.message;
      }
      return JSON.stringify(candidate);
    }
  }

  return `HTTP ${status}`;
}

/**
 * Extract a stable error code/message pair from backend payloads. Supports
 * structured `{ error: { code, message } }` and the flat `{ error, code }`
 * shape the marketplace rate limiter returns.
 *
 * @param {unknown} responseData
 * @param {number} status
 * @returns {{ code: string | null, message: string }}
 */
function getApiErrorInfo(responseData, status) {
  const nestedCode = responseData?.error?.code;
  const flatCode = responseData?.code;
  const codeCandidate =
    typeof nestedCode === "string" && nestedCode.trim()
      ? nestedCode
      : typeof flatCode === "string" && flatCode.trim()
        ? flatCode
        : null;

  return {
    code: codeCandidate ? codeCandidate.trim() : null,
    message: getErrorMessage(responseData, status),
  };
}

module.exports = {
  log,
  info,
  success,
  warn,
  error,
  heading,
  dim,
  input,
  select,
  confirm,
  getErrorMessage,
  getApiErrorInfo,
};
