const path = require("path");
const fs = require("fs");
const crypto = require("crypto");
const {
  input,
  heading,
  info,
  success,
  warn,
  error,
  getApiErrorInfo,
} = require("../shared");
const {
  requireAuth,
  getEmail,
  authenticatedFetch,
} = require("../internal/auth");
const {
  CLI_BIN_NAME,
  MARKETPLACE_API_PLUGINS_SUBMIT_URL,
} = require("../internal/constants");
const { resolvePluginManifest, getRepoUrl } = require("../internal/manifest");
const {
  resolveCoverImageUrls,
  validateCoverSources,
  rememberCoverImageUrls,
  isRemoteCoverSource,
} = require("../internal/coverUpload");
const {
  VALID_TAGS,
  partitionTags,
  validateSubmission,
} = require("../internal/marketplaceSchema");
const {
  maybePrintPaxeerNetworkDevEnvironmentHints,
} = require("../internal/paxeerNetworkSdkDocsMcpDetect");
const { printMarketplaceFailure } = require("../internal/marketplaceErrors");

const SUBMIT_PROGRESS_TOTAL_STEPS = 8;

/**
 * Print a consistent progress message so users can track submit lifecycle.
 * @param {number} step
 * @param {string} message
 */
function printProgress(step, message) {
  info(`[${step}/${SUBMIT_PROGRESS_TOTAL_STEPS}] ${message}`);
}

/**
 * Warn-flavored variant of printProgress for recoverable hiccups.
 * @param {number} step
 * @param {string} message
 */
function printProgressWarn(step, message) {
  warn(`[${step}/${SUBMIT_PROGRESS_TOTAL_STEPS}] ${message}`);
}

module.exports = {
  command: "submit",
  describe: "Submit a plugin to paxeer-network Marketplace",
  builder: (yargs) => {
    return yargs
      .option("path", {
        alias: "p",
        type: "string",
        describe:
          "string; path to the plugin directory. If omitted, you'll be prompted (default: ./)",
        demandOption: false,
      })
      .option("tags", {
        alias: "t",
        type: "string",
        describe:
          "string; comma-separated tag list (e.g., 'UI,Trading'). Values are trimmed; invalid tags are ignored with a warning",
        demandOption: false,
      })
      .option("dry-run", {
        alias: "d",
        type: "boolean",
        describe:
          "boolean; validate the submission payload and print it without calling the marketplace API",
        default: false,
      })
      .example(
        "paxeer-network-devkit submit --path ./my-plugin --dry-run",
        "Validate the plugin payload from a local folder",
      )
      .example(
        "paxeer-network-devkit submit --path ./my-plugin --tags UI,Trading",
        "Submit with tags",
      );
  },
  handler: async (argv) => {
    heading("Submit to paxeer-network Marketplace");
    info("This command will submit your plugin to the marketplace.\n");
    printProgress(1, "Checking authentication status...");

    if (!requireAuth()) {
      return;
    }

    const accountName = getEmail();
    info(`Authenticated as: ${accountName || "(unknown user)"}\n`);

    printProgress(2, "Resolving plugin path...");
    const targetPath = argv.path || (await input("Path to plugin:", "./"));
    const resolvedPath = path.resolve(targetPath);

    printProgress(3, `Reading plugin metadata from ${resolvedPath}...`);

    const manifest = resolvePluginManifest(resolvedPath);
    if (!manifest) {
      error("No plugin metadata found.");
      info(
        `Add a package.json with a valid "name" field, or create .paxeer-network-manifest.json (e.g. via '${CLI_BIN_NAME} create plugin').`,
      );
      process.exitCode = 1;
      return;
    }

    if (!manifest.repoUrl) {
      const repoUrl = getRepoUrl(resolvedPath);
      if (repoUrl) {
        info("Found repo URL from git remote, adding to manifest...");
        manifest.repoUrl = repoUrl;
      }
    }

    printProgress(4, "Preparing submission fields...");

    // The marketplace requires a display name and description up front; the
    // plugin ID is derived from the npm name when not set explicitly.
    let displayName = manifest.name || "";
    if (!displayName) {
      displayName = await input("Plugin display name (required):");
    }
    let description = manifest.description || "";
    if (!description) {
      const packageJsonPath = path.join(resolvedPath, "package.json");
      try {
        description =
          JSON.parse(fs.readFileSync(packageJsonPath, "utf-8")).description ||
          "";
      } catch {
        description = "";
      }
    }
    if (!description) {
      description = await input("Plugin description (required):");
    }

    info(`Package: ${manifest.npmName}`);
    if (manifest.pluginId) {
      info(`Plugin ID: ${manifest.pluginId}`);
    } else {
      info("Plugin ID: (will be derived from the npm name)");
    }
    if (manifest.repoUrl) {
      info(`Repository: ${manifest.repoUrl}`);
    }

    let tags = manifest.tags || [];
    if (argv.tags) {
      tags = argv.tags.split(",").map((t) => t.trim());
    }

    const { validTags, invalidTags } = partitionTags(tags);
    if (invalidTags.length > 0) {
      warn(`Invalid tags: ${invalidTags.join(", ")}`);
      info(`Valid tags: ${VALID_TAGS.join(", ")}`);
    }
    tags = validTags;

    const usagePrompt = manifest.usagePrompt || null;
    const coverSources = manifest.coverImages || [];

    printProgress(5, "Validating submission payload...");
    const submission = validateSubmission({
      npmName: manifest.npmName,
      repoUrl: manifest.repoUrl,
      pluginId: manifest.pluginId,
      name: displayName,
      description,
      tags,
      coverImages: coverSources,
      usagePrompt,
    });

    if (!submission.valid) {
      error("\nValidation failed. Please fix the following issues:");
      submission.errors.forEach((e) => info(`  - ${e}`));
      process.exitCode = 1;
      return;
    }

    try {
      validateCoverSources({
        pluginDir: resolvedPath,
        coverSources,
        // Check local files before upload: the server would reject them
        // anyway, and cover uploads are rate-limited per hour.
        validateImageContents: true,
      });
    } catch (coverError) {
      error(`Cover image preparation failed: ${coverError.message}`);
      process.exitCode = 1;
      return;
    }

    if (argv["dry-run"]) {
      success("\nDry-run completed! Plugin is valid for submission.");
      info(`\nSubmission payload:`);
      const dryRunPayload = {
        npmName: manifest.npmName,
        repoUrl: manifest.repoUrl,
        name: displayName,
        description,
        tags,
        // Local paths are shown here; a real run replaces them with the
        // Marketplace-hosted URLs returned by the upload endpoint.
        coverImages: coverSources,
      };
      if (manifest.pluginId) {
        dryRunPayload.pluginId = manifest.pluginId;
      }
      if (usagePrompt) {
        dryRunPayload.usagePrompt = usagePrompt;
      }
      console.log(JSON.stringify(dryRunPayload, null, 2));
      return;
    }

    // All-managed cover URLs mean the upload step is a no-op.
    const hasLocalCovers = coverSources.some(
      (source) => !isRemoteCoverSource(source),
    );
    printProgress(
      6,
      hasLocalCovers
        ? "Uploading cover images..."
        : "Cover images already uploaded, skipping upload.",
    );
    let coverImageUrls = [];
    try {
      coverImageUrls = await resolveCoverImageUrls({
        pluginDir: resolvedPath,
        npmName: manifest.npmName,
        coverSources,
      });
    } catch (e) {
      const cause = e?.message || String(e);
      error(`Cover image preparation failed: ${cause}`);
      process.exitCode = 1;
      return;
    }

    printProgress(
      7,
      `Submitting request to paxeer-network Marketplace (${MARKETPLACE_API_PLUGINS_SUBMIT_URL})...`,
    );

    const payload = {
      npmName: manifest.npmName,
      repoUrl: manifest.repoUrl,
      name: displayName,
      description,
      tags,
      coverImages: coverImageUrls,
    };

    if (manifest.pluginId) {
      payload.pluginId = manifest.pluginId;
    }
    if (usagePrompt) {
      payload.usagePrompt = usagePrompt;
    }

    try {
      const response = await authenticatedFetch(
        MARKETPLACE_API_PLUGINS_SUBMIT_URL,
        {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            Accept: "application/json",
            // Required by the marketplace submit endpoint for safe retries.
            "Idempotency-Key": crypto.randomUUID(),
          },
          body: JSON.stringify(payload),
        },
        {
          onAuthEvent: (event, details = {}) => {
            if (event === "request_unauthorized") {
              printProgress(
                7,
                "Access token expired (HTTP 401), attempting token refresh...",
              );
              return;
            }

            if (event === "refresh_started") {
              printProgress(
                7,
                `Refreshing CLI token via ${details.url || "refresh endpoint"}...`,
              );
              return;
            }

            if (event === "refresh_succeeded") {
              printProgress(
                7,
                "Token refresh succeeded, retrying submit request...",
              );
              return;
            }

            if (event === "refresh_missing") {
              printProgressWarn(
                7,
                `No refresh token found. Please run '${CLI_BIN_NAME} login' again if submit fails.`,
              );
              return;
            }

            if (event === "refresh_failed") {
              printProgressWarn(
                7,
                `Token refresh failed (HTTP ${details.status || "unknown"}).`,
              );
              return;
            }

            if (event === "refresh_error") {
              printProgressWarn(
                7,
                `Token refresh request error: ${details.message || "unknown error"}`,
              );
              return;
            }

            if (event === "request_retry_started") {
              printProgress(
                7,
                "Sending retried submit request with refreshed token...",
              );
            }
          },
        },
      );

      printProgress(
        8,
        `Received server response (HTTP ${response.status}), processing...`,
      );
      const responseData = await response.json().catch(() => ({}));

      // The submit endpoint always responds 202: the listing is created later
      // by the audit worker, so there is no "created" status to distinguish.
      if (response.ok) {
        const submissionResult = responseData?.submission || {};
        success("\nSubmission received, awaiting processing.");
        info(`Plugin ID: ${submissionResult.pluginIdCandidate || "N/A"}`);
        info(`NPM Name: ${manifest.npmName}`);
        info(`Status: ${submissionResult.status || "queued"}`);
        if (submissionResult.id) {
          info(`Submission ID: ${submissionResult.id}`);
        }
        if (
          rememberCoverImageUrls({
            pluginDir: resolvedPath,
            coverSources,
            coverImageUrls,
          })
        ) {
          info(
            "Manifest coverImages now point at the uploaded URLs, so later runs skip re-uploading.",
          );
        }
        maybePrintPaxeerNetworkDevEnvironmentHints(resolvedPath);
      } else {
        const { code: errorCode, message: errorMessage } = getApiErrorInfo(
          responseData,
          response.status,
        );
        printMarketplaceFailure({
          command: "submit",
          status: response.status,
          code: errorCode,
          message: errorMessage,
          responseData,
        });

        process.exitCode = 1;
      }
    } catch (e) {
      const cause = e?.message || String(e);
      error(
        `Submission failed while calling ${MARKETPLACE_API_PLUGINS_SUBMIT_URL}: ${cause}`,
      );
      info("Please verify network connectivity and API availability.");
      process.exitCode = 1;
    }
  },
};
