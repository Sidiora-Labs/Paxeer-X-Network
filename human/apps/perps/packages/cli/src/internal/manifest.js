const fs = require("fs");
const path = require("path");
const { execSync } = require("child_process");
const { CLI_BIN_NAME } = require("./constants");
const { parseGithubRepoUrl } = require("./formatting");

const MANIFEST_FILENAME = ".paxeer-network-manifest.json";

/**
 * Read repoUrl from git remote origin. SSH remotes are normalized to the
 * canonical https form the Marketplace requires.
 * @param {string} [cwd] - Directory to read the remote from; defaults to the
 * process working directory.
 */
function getRepoUrl(cwd) {
  try {
    const remoteUrl = execSync("git remote get-url origin", {
      encoding: "utf-8",
      stdio: ["pipe", "pipe", "pipe"],
      windowsHide: true,
      cwd: cwd || undefined,
    }).trim();

    const github = parseGithubRepoUrl(remoteUrl);
    if (github?.url.startsWith("https://github.com/")) {
      return github.url;
    }

    return remoteUrl.replace(/\.git$/, "");
  } catch (e) {
    return null;
  }
}

/**
 * Read and parse package.json, returning null when missing or malformed.
 */
function readPackageJson(packageJsonPath) {
  try {
    return JSON.parse(fs.readFileSync(packageJsonPath, "utf-8"));
  } catch (e) {
    return null;
  }
}

/**
 * Generate `.paxeer-network-manifest.json` in the plugin directory. Fields mirror
 * the Marketplace submit schema, which requires name, description, at least
 * one tag, and at least one cover image.
 * @param {string} pluginDir - Plugin root directory
 * @param {Object} pluginInfo - pluginId, name, description, tags, etc.
 */
function generateManifest(pluginDir, pluginInfo = {}) {
  const packageJsonPath = path.join(pluginDir, "package.json");

  if (!fs.existsSync(packageJsonPath)) {
    throw new Error("package.json not found in plugin directory");
  }

  const packageJson = readPackageJson(packageJsonPath);

  const manifest = {
    npmName: packageJson?.name || null,
    pluginId: pluginInfo.pluginId || null,
    repoUrl: pluginInfo.repoUrl || getRepoUrl(pluginDir) || null,
    name: pluginInfo.name || null,
    description: pluginInfo.description || packageJson?.description || null,
    tags: pluginInfo.tags || [],
    // Local paths are resolved against the plugin directory and uploaded to
    // the Marketplace cover-images endpoint on submit.
    coverImages: pluginInfo.coverImages || [],
    // Omitted unless explicitly set: `update` sends usagePrompt: null for a
    // stored null, which would clear the server-side value on every run.
    ...(pluginInfo.usagePrompt ? { usagePrompt: pluginInfo.usagePrompt } : {}),
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
  };

  const manifestPath = path.join(pluginDir, MANIFEST_FILENAME);
  fs.writeFileSync(manifestPath, JSON.stringify(manifest, null, 2));

  return manifest;
}

/**
 * Read `.paxeer-network-manifest.json` from the plugin directory.
 * @param {string} pluginDir - Plugin root directory
 * @returns {Object|null} Manifest object, or null when missing
 */
function readManifest(pluginDir) {
  const manifestPath = path.join(pluginDir, MANIFEST_FILENAME);

  if (!fs.existsSync(manifestPath)) {
    return null;
  }

  try {
    return JSON.parse(fs.readFileSync(manifestPath, "utf-8"));
  } catch (e) {
    console.warn(
      `Warning: Failed to parse ${MANIFEST_FILENAME}: ${e.message}. Ignoring corrupted manifest.`,
    );
    return null;
  }
}

/**
 * Resolve plugin metadata for submit: use `.paxeer-network-manifest.json` when present,
 * otherwise derive from `package.json` and git (manual plugins may skip the manifest).
 * @param {string} pluginDir - Plugin root directory
 * @returns {Object|null} Manifest-shaped object, or null if no manifest and no package.json
 */
function resolvePluginManifest(pluginDir) {
  const fromFile = readManifest(pluginDir);
  if (fromFile) {
    return fromFile;
  }

  const packageJsonPath = path.join(pluginDir, "package.json");
  if (!fs.existsSync(packageJsonPath)) {
    return null;
  }

  const packageJson = readPackageJson(packageJsonPath);

  return {
    npmName: packageJson?.name || null,
    pluginId: null,
    repoUrl: getRepoUrl(pluginDir),
    // The npm name is a poor display name, so leave it unset and let submit
    // prompt for one.
    name: null,
    description: packageJson?.description || null,
    tags: [],
    coverImages: [],
  };
}

/**
 * Update fields in an existing manifest file.
 * @param {string} pluginDir - Plugin root directory
 * @param {Object} updates - Fields to merge into the manifest
 */
function updateManifest(pluginDir, updates) {
  const manifest = readManifest(pluginDir);

  if (!manifest) {
    throw new Error(
      `Manifest file not found. Run '${CLI_BIN_NAME} create plugin' first.`,
    );
  }

  const updatedManifest = {
    ...manifest,
    ...updates,
    updatedAt: new Date().toISOString(),
  };

  const manifestPath = path.join(pluginDir, MANIFEST_FILENAME);
  fs.writeFileSync(manifestPath, JSON.stringify(updatedManifest, null, 2));

  return updatedManifest;
}

module.exports = {
  MANIFEST_FILENAME,
  getRepoUrl,
  generateManifest,
  readManifest,
  resolvePluginManifest,
  updateManifest,
};
