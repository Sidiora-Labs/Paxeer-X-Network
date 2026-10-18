const { error, info } = require("../shared");
const { CLI_BIN_NAME } = require("./constants");

/**
 * Render actionable failure hints while preserving the server message.
 * Shared by submit and update so both commands report the same status
 * codes with the same guidance.
 * @param {object} params
 * @param {"submit" | "update"} params.command - command name used in hints
 * @param {number} params.status - HTTP status code
 * @param {string | null} params.code - structured error code from the payload
 * @param {string} params.message - normalized server message
 * @param {Record<string, unknown> | null} params.responseData - raw response payload
 */
function printMarketplaceFailure({
  command,
  status,
  code,
  message,
  responseData,
}) {
  const normalizedCode = (code || "").toUpperCase();
  const normalizedMessage = message || `HTTP ${status}`;
  const label = command === "submit" ? "Submission" : "Update";

  if (status === 409 || normalizedCode === "CONFLICT") {
    if (/github/i.test(normalizedMessage)) {
      error(`GitHub account required: ${normalizedMessage}`);
      info(
        `Connect your GitHub account in the web console first, then rerun '${CLI_BIN_NAME} ${command}'.`,
      );
    } else if (/active submission/i.test(normalizedMessage)) {
      error(`Plugin registration conflict: ${normalizedMessage}`);
      info(
        `A submission for this plugin is still being processed. Track it with '${CLI_BIN_NAME} list' and resubmit once it completes.`,
      );
      const submissionId = responseData?.submissionId;
      if (submissionId) {
        info(`Submission ID: ${submissionId}`);
      }
    } else if (
      command === "submit" &&
      /npm package is already registered/i.test(normalizedMessage)
    ) {
      error(`Plugin registration conflict: ${normalizedMessage}`);
      info(
        `This package already has a listing. Use '${CLI_BIN_NAME} update' to change it, or set a different pluginId in your manifest to register a new one.`,
      );
    } else {
      error(`Plugin registration conflict: ${normalizedMessage}`);
      info(
        `Try a different pluginId in your manifest, then rerun '${CLI_BIN_NAME} ${command}'.`,
      );
    }
  } else if (status === 429 || /RATE_LIMIT/i.test(normalizedCode)) {
    error(`Rate limit exceeded: ${normalizedMessage}`);
    const retryAfter = Number(responseData?.retryAfter);
    if (Number.isFinite(retryAfter) && retryAfter > 0) {
      info(`Wait about ${Math.ceil(retryAfter)} seconds before trying again.`);
    }
  } else if (status === 404 || normalizedCode === "NOT_FOUND") {
    error(`Resource not found: ${normalizedMessage}`);
    info(
      command === "update"
        ? "Please verify pluginId in your manifest matches an existing listing and try again."
        : "Please verify npmName/repoUrl/pluginId in your manifest and try again.",
    );
  } else if (status === 400 || normalizedCode === "BAD_REQUEST") {
    error(`Invalid ${label.toLowerCase()} data: ${normalizedMessage}`);
    info(`Please correct your manifest fields and run ${command} again.`);
  } else if (status === 401) {
    error(`Unauthorized. Please run '${CLI_BIN_NAME} login' again.`);
  } else if (status >= 500) {
    error(`Marketplace server error (HTTP ${status}): ${normalizedMessage}`);
  } else {
    error(`${label} failed (HTTP ${status}): ${normalizedMessage}`);
  }

  if (responseData?.issues) {
    info("Details:");
    console.log(JSON.stringify(responseData.issues, null, 2));
  }

  if (code) {
    info(`Error code: ${code}`);
  }
}

module.exports = { printMarketplaceFailure };
