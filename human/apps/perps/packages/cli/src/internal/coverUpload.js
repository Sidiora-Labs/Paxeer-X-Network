const path = require("path");
const fs = require("fs");
const { authenticatedFetch } = require("./auth");
const { MARKETPLACE_API_BASE_URL } = require("./constants");
const { getApiErrorInfo } = require("../shared");
const { MAX_COVER_IMAGES } = require("./marketplaceSchema");
const { updateManifest } = require("./manifest");

const PNG_SIGNATURE = Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]);
const COVER_DIMENSIONS = new Set(["1280x800", "640x400"]);

function readJpegDimensions(buffer) {
  if (buffer.length < 4 || buffer[0] !== 0xff || buffer[1] !== 0xd8) {
    return null;
  }

  let offset = 2;
  while (offset + 3 < buffer.length) {
    while (offset < buffer.length && buffer[offset] !== 0xff) offset += 1;
    while (offset < buffer.length && buffer[offset] === 0xff) offset += 1;
    if (offset >= buffer.length) return null;

    const marker = buffer[offset++];
    if (marker === 0xd9 || marker === 0xda) return null;
    if (marker >= 0xd0 && marker <= 0xd7) continue;
    if (offset + 1 >= buffer.length) return null;

    const segmentLength = buffer.readUInt16BE(offset);
    if (segmentLength < 2 || offset + segmentLength > buffer.length) {
      return null;
    }

    const isStartOfFrame =
      (marker >= 0xc0 && marker <= 0xc3) ||
      (marker >= 0xc5 && marker <= 0xc7) ||
      (marker >= 0xc9 && marker <= 0xcb) ||
      (marker >= 0xcd && marker <= 0xcf);
    if (isStartOfFrame && segmentLength >= 7) {
      return {
        width: buffer.readUInt16BE(offset + 5),
        height: buffer.readUInt16BE(offset + 3),
      };
    }
    offset += segmentLength;
  }
  return null;
}

function readWebpDimensions(buffer) {
  if (
    buffer.length < 16 ||
    buffer.toString("ascii", 0, 4) !== "RIFF" ||
    buffer.toString("ascii", 8, 12) !== "WEBP"
  ) {
    return null;
  }

  const chunk = buffer.toString("ascii", 12, 16);
  if (chunk === "VP8X" && buffer.length >= 30) {
    return {
      width: 1 + buffer[24] + (buffer[25] << 8) + (buffer[26] << 16),
      height: 1 + buffer[27] + (buffer[28] << 8) + (buffer[29] << 16),
    };
  }

  if (chunk === "VP8 " && buffer.length >= 30) {
    const frame = buffer.indexOf(Buffer.from([0x9d, 0x01, 0x2a]), 20);
    if (frame >= 0 && frame + 7 < buffer.length) {
      return {
        width: buffer.readUInt16LE(frame + 3),
        height: buffer.readUInt16LE(frame + 5),
      };
    }
  }

  if (chunk === "VP8L" && buffer.length >= 25 && buffer[20] === 0x2f) {
    const bits =
      buffer[21] | (buffer[22] << 8) | (buffer[23] << 16) | (buffer[24] << 24);
    return {
      width: (bits & 0x3fff) + 1,
      height: ((bits >>> 14) & 0x3fff) + 1,
    };
  }

  return null;
}

function inspectCoverImage(filePath) {
  const buffer = fs.readFileSync(filePath);
  let mimeType = null;
  let dimensions = null;

  if (buffer.subarray(0, 8).equals(PNG_SIGNATURE) && buffer.length >= 24) {
    mimeType = "image/png";
    dimensions = {
      width: buffer.readUInt32BE(16),
      height: buffer.readUInt32BE(20),
    };
  } else if (buffer[0] === 0xff && buffer[1] === 0xd8) {
    mimeType = "image/jpeg";
    dimensions = readJpegDimensions(buffer);
  } else if (buffer.toString("ascii", 0, 4) === "RIFF") {
    mimeType = "image/webp";
    dimensions = readWebpDimensions(buffer);
  }

  if (!mimeType || !dimensions) {
    throw new Error(
      `Cover image is not a supported PNG, JPEG, or WebP file: ${filePath}`,
    );
  }

  const key = `${dimensions.width}x${dimensions.height}`;
  if (!COVER_DIMENSIONS.has(key)) {
    throw new Error(
      `Cover image must be exactly 1280 x 800 or 640 x 400 pixels: ${filePath} (${key})`,
    );
  }
}

/** Match dex-creator's MarketplaceCoverImageUrl, including local GCS hosting. */
function isManagedCoverImageUrl(value) {
  const hosts = ["/assets"];
  const override = process.env.GCS_PUBLIC_BASE_URL?.trim().replace(/\/+$/, "");
  if (override) hosts.push(override);
  try {
    const url = new URL(value);
    return (
      hosts.includes(url.origin) && url.pathname.startsWith("/marketplace/")
    );
  } catch {
    return false;
  }
}

/** True for http(s) cover sources; everything else is treated as a local path. */
function isRemoteCoverSource(source) {
  return /^https?:\/\//i.test(source);
}

/** Validate every source before starting any uploads. */
function validateCoverSources({
  pluginDir,
  coverSources,
  validateImageContents = false,
}) {
  if (!Array.isArray(coverSources)) {
    throw new Error("coverImages must be an array");
  }
  if (coverSources.length > MAX_COVER_IMAGES) {
    throw new Error(`Too many cover images, maximum is ${MAX_COVER_IMAGES}`);
  }
  const localFiles = [];
  for (const source of coverSources) {
    if (typeof source !== "string" || !source.trim()) {
      throw new Error("Each cover image must be a non-empty string");
    }
    if (isRemoteCoverSource(source)) {
      if (!isManagedCoverImageUrl(source)) {
        throw new Error(
          `Invalid cover URL: ${source}. Use a local file or a Marketplace-hosted URL under /marketplace/.`,
        );
      }
      continue;
    }
    const filePath = path.resolve(pluginDir, source);
    try {
      if (!fs.statSync(filePath).isFile()) throw new Error("Not a file");
      fs.accessSync(filePath, fs.constants.R_OK);
    } catch {
      throw new Error(`Cover image is not a readable file: ${filePath}`);
    }
    localFiles.push(filePath);
  }
  if (validateImageContents) {
    for (const filePath of localFiles) {
      inspectCoverImage(filePath);
    }
  }
}

/**
 * Resolve manifest cover entries into marketplace-hosted URLs. Managed URLs
 * pass through; local paths are uploaded to the cover-images endpoint which
 * returns the managed URL the marketplace schemas require.
 * @param {object} params
 * @param {string} params.pluginDir
 * @param {string} params.npmName
 * @param {string[]} params.coverSources
 * @returns {Promise<string[]>}
 */
async function resolveCoverImageUrls({ pluginDir, npmName, coverSources }) {
  validateCoverSources({ pluginDir, coverSources });
  const urls = [];
  for (const source of coverSources) {
    if (isRemoteCoverSource(source)) {
      urls.push(source);
      continue;
    }

    const filePath = path.isAbsolute(source)
      ? source
      : path.join(pluginDir, source);
    if (!fs.existsSync(filePath)) {
      throw new Error(`Cover image not found: ${filePath}`);
    }

    const form = new FormData();
    form.append("npmName", npmName);
    form.append(
      "file",
      new Blob([fs.readFileSync(filePath)]),
      path.basename(filePath),
    );

    const response = await authenticatedFetch(
      `${MARKETPLACE_API_BASE_URL}/cover-images`,
      { method: "POST", body: form },
    );
    const data = await response.json().catch(() => ({}));
    if (!response.ok || !data?.url) {
      const { message: errorMessage } = getApiErrorInfo(data, response.status);
      const retryAfter = Number(data?.retryAfter);
      const retryHint =
        response.status === 429 && Number.isFinite(retryAfter) && retryAfter > 0
          ? ` (retry after ${Math.ceil(retryAfter)}s)`
          : "";
      throw new Error(
        `Cover upload failed for ${path.basename(filePath)}: ${errorMessage}${retryHint}`,
      );
    }
    urls.push(data.url);
  }
  return urls;
}

/**
 * Replace local cover paths in the manifest with the managed URLs returned by
 * the upload, so later runs reuse them instead of re-uploading (the
 * Marketplace caps cover uploads per hour and expires unlinked ones).
 *
 * Call this only after the submit/update request succeeded: uploads that were
 * never linked to a plugin are cleaned up server-side, so persisting their
 * URLs would leave the manifest pointing at images that will disappear.
 * @returns {boolean} true when the manifest was rewritten
 */
function rememberCoverImageUrls({ pluginDir, coverSources, coverImageUrls }) {
  const unchanged =
    coverImageUrls.length === coverSources.length &&
    coverImageUrls.every((url, index) => url === coverSources[index]);
  if (unchanged) {
    return false;
  }

  try {
    updateManifest(pluginDir, { coverImages: coverImageUrls });
    return true;
  } catch (error) {
    const message = error?.message || String(error);
    if (/Manifest file not found/i.test(message)) {
      // Metadata may have come from package.json, so there is nothing to persist.
      return false;
    }
    const manifestPath = path.join(pluginDir, ".paxeer-network-manifest.json");
    console.warn(
      `Could not persist uploaded cover image URLs to ${manifestPath}: ${message}`,
    );
    return false;
  }
}

module.exports = {
  resolveCoverImageUrls,
  validateCoverSources,
  rememberCoverImageUrls,
  isRemoteCoverSource,
};
