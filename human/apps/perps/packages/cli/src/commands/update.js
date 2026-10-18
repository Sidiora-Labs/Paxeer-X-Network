const path = require("path");
const {
  heading,
  info,
  success,
  warn,
  error,
  input,
  getApiErrorInfo,
} = require("../shared");
const { requireAuth, authenticatedFetch } = require("../internal/auth");
const { MARKETPLACE_API_PLUGINS_URL } = require("../internal/constants");
const { resolvePluginManifest } = require("../internal/manifest");
const {
  resolveCoverImageUrls,
  validateCoverSources,
  rememberCoverImageUrls,
  isRemoteCoverSource,
} = require("../internal/coverUpload");
const {
  VALID_TAGS,
  partitionTags,
  validateUpdatePayload,
} = require("../internal/marketplaceSchema");
const { printMarketplaceFailure } = require("../internal/marketplaceErrors");

/**
 * Include only user-editable fields supported by PATCH /plugins/{id}
 * (@dex-creator/schemas MarketplaceUpdateSchema). Undefined values are
 * omitted; an explicit null/empty usagePrompt clears the server-side value.
 * @param {object} manifest
 * @returns {object}
 */
function buildUpdatePayload(manifest) {
  const payload = {};

  const directFields = ["name", "description", "usagePrompt"];

  directFields.forEach((field) => {
    const value = manifest[field];
    if (field === "usagePrompt" && (value === null || value === "")) {
      payload[field] = null;
    } else if (value !== undefined && value !== null && value !== "") {
      payload[field] = value;
    }
  });

  const tags = Array.isArray(manifest.tags) ? manifest.tags : [];
  if (tags.length > 0) {
    const { validTags, invalidTags } = partitionTags(tags);

    if (invalidTags.length > 0) {
      warn(`Ignored invalid tags from manifest: ${invalidTags.join(", ")}`);
      info(`Valid tags: ${VALID_TAGS.join(", ")}`);
    }

    if (validTags.length > 0) {
      payload.tags = validTags;
    }
  }

  if (Array.isArray(manifest.coverImages) && manifest.coverImages.length > 0) {
    payload.coverImages = manifest.coverImages;
  }
  return payload;
}

module.exports = {
  command: "update",
  describe: "Update plugin metadata in paxeer-network Marketplace",
  builder: (yargs) => {
    return yargs
      .option("path", {
        alias: "p",
        type: "string",
        describe:
          "string; path to the plugin directory (must contain .paxeer-network-manifest.json with a pluginId, or package.json-derived metadata). If omitted, you will be prompted",
        demandOption: false,
      })
      .option("dry-run", {
        alias: "d",
        type: "boolean",
        describe:
          "boolean; validate and print the PATCH payload without calling the marketplace API",
        default: false,
      })
      .example(
        "paxeer-network-devkit update --path ./my-plugin --dry-run",
        "Validate the update payload from a local plugin folder",
      );
  },
  handler: async (argv) => {
    heading("Update plugin on paxeer-network Marketplace");

    if (!requireAuth()) {
      return;
    }

    const targetPath = argv.path || (await input("Path to plugin:", "./"));
    const resolvedPath = path.resolve(targetPath);
    info(`Reading plugin metadata from ${resolvedPath}...`);

    const manifest = resolvePluginManifest(resolvedPath);
    if (!manifest) {
      error("No plugin metadata found.");
      info(
        "Please ensure .paxeer-network-manifest.json exists in your plugin project and contains pluginId.",
      );
      process.exitCode = 1;
      return;
    }

    const pluginId = String(manifest.pluginId || "").trim();
    if (!pluginId) {
      error("pluginId is required in .paxeer-network-manifest.json for update.");
      process.exitCode = 1;
      return;
    }

    try {
      validateCoverSources({
        pluginDir: resolvedPath,
        coverSources: manifest.coverImages ?? [],
        // Check local files before upload: the server would reject them
        // anyway, and cover uploads are rate-limited per hour.
        validateImageContents: true,
      });
    } catch (coverError) {
      error(`Cover image preparation failed: ${coverError.message}`);
      process.exitCode = 1;
      return;
    }

    const payload = buildUpdatePayload(manifest);
    const validation = validateUpdatePayload(payload);

    if (!validation.valid) {
      error("Validation failed. Please fix the following issues:");
      validation.errors.forEach((validationError) =>
        info(`  - ${validationError}`),
      );
      process.exitCode = 1;
      return;
    }

    if (Object.keys(payload).length === 0) {
      warn("No updatable fields found in manifest.");
      info(
        "Supported fields: name, description, tags, coverImages, usagePrompt.",
      );
      process.exitCode = 1;
      return;
    }

    const apiUrl = `${MARKETPLACE_API_PLUGINS_URL}/${encodeURIComponent(pluginId)}`;

    if (argv["dry-run"]) {
      success("Dry-run completed. Update payload is valid.");
      info("PATCH target:");
      console.log(apiUrl);
      info(
        "\nPayload (local cover images will be uploaded only on a real update):",
      );
      console.log(JSON.stringify(payload, null, 2));
      return;
    }

    const coverSources = payload.coverImages;
    if (coverSources) {
      // The cover-images endpoint keys uploads by npmName; sending an empty
      // one would fail server-side with an unhelpful 400.
      const hasLocalCovers = coverSources.some(
        (source) => !isRemoteCoverSource(source),
      );
      if (hasLocalCovers && !String(manifest.npmName || "").trim()) {
        error("npmName is required in the manifest to upload cover images.");
        info(
          "Set npmName in .paxeer-network-manifest.json (usually copied from package.json) and rerun.",
        );
        process.exitCode = 1;
        return;
      }

      try {
        payload.coverImages = await resolveCoverImageUrls({
          pluginDir: resolvedPath,
          npmName: String(manifest.npmName || ""),
          coverSources,
        });
      } catch (coverError) {
        const cause = coverError?.message || String(coverError);
        error(`Cover image preparation failed: ${cause}`);
        process.exitCode = 1;
        return;
      }
    }

    info("Submitting update request...");

    try {
      const response = await authenticatedFetch(apiUrl, {
        method: "PATCH",
        headers: {
          "Content-Type": "application/json",
          Accept: "application/json",
        },
        body: JSON.stringify(payload),
      });

      const responseData = await response.json().catch(() => null);

      if (!response.ok) {
        const { code, message } = getApiErrorInfo(
          responseData,
          response.status,
        );
        printMarketplaceFailure({
          command: "update",
          status: response.status,
          code,
          message,
          responseData,
        });
        process.exitCode = 1;
        return;
      }

      success("Plugin updated successfully!");
      info(`Plugin ID: ${responseData?.id || pluginId}`);
      info(`Status: ${responseData?.status || "N/A"}`);
      if (
        coverSources &&
        rememberCoverImageUrls({
          pluginDir: resolvedPath,
          coverSources,
          coverImageUrls: payload.coverImages,
        })
      ) {
        info(
          "Manifest coverImages now point at the uploaded URLs, so later runs skip re-uploading.",
        );
      }
    } catch (requestError) {
      const cause = requestError?.message || String(requestError);
      error(`Request failed while calling ${apiUrl}: ${cause}`);
      info("Please verify network connectivity and API availability.");
      process.exitCode = 1;
    }
  },
};
