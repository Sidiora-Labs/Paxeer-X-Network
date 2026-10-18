const degit = require("degit");
const path = require("path");
const fs = require("fs-extra");
const Handlebars = require("handlebars");
const fg = require("fast-glob");
const { execSync } = require("child_process");

// ============================================================================
// Core API
// ============================================================================

/**
 * Download a template from a GitHub repository using degit.
 * @param {string} repo - e.g. "PaxeerNetworkNetwork/paxeer-network-plugin-template"
 * @param {string} targetDir - local directory to clone into
 * @param {object} options
 * @param {boolean} options.force - overwrite existing files (default: true)
 */
async function downloadTemplate(repo, targetDir, options = {}) {
  const { force = true } = options;
  console.log(`\n  Downloading template from https://github.com/${repo}...`);
  const emitter = degit(repo, { cache: false, force });
  await emitter.clone(targetDir);
  console.log("  Template download completed");
}

/**
 * Replace all Handlebars template variables in file contents.
 * @param {string} dir - directory to scan
 * @param {object} vars - key-value pairs for replacement
 */
async function replaceTemplateVars(dir, vars) {
  const files = await fg(["**/*", "**/.*"], {
    cwd: dir,
    absolute: true,
    ignore: ["**/node_modules/**", "**/.git/**"],
  });

  await Promise.all(
    files.map(async (filePath) => {
      if (isBinaryFile(filePath)) return;
      const content = await fs.readFile(filePath, "utf-8");
      const template = Handlebars.compile(content);
      const result = template(vars);
      await fs.writeFile(filePath, result);
    }),
  );
}

/**
 * Rename files and directories that contain __name__ to the actual name.
 * @param {string} dir - directory to scan
 * @param {string} nameVar - the name to replace __name__ with (PascalCase)
 */
async function renameTemplateFiles(dir, nameVar) {
  // Get directories first (deepest first to rename children before parents)
  const dirs = await fg(["**/*", "**/.*"], {
    cwd: dir,
    absolute: true,
    ignore: ["**/node_modules/**", "**/.git/**"],
    onlyDirectories: true,
  });
  dirs.sort((a, b) => b.split(path.sep).length - a.split(path.sep).length);

  // Then get files
  const files = await fg(["**/*", "**/.*"], {
    cwd: dir,
    absolute: true,
    ignore: ["**/node_modules/**", "**/.git/**"],
    onlyDirectories: false,
    onlyFiles: true,
  });

  for (const dirPath of dirs) {
    await renamePath(dirPath, nameVar);
  }

  for (const filePath of files) {
    await renamePath(filePath, nameVar);
  }
}

/**
 * Run npm install in the target directory.
 * @param {string} dir - directory to install deps
 * @param {boolean} skipInstall - if true, skip installation
 */
function installDeps(dir, skipInstall = false) {
  if (skipInstall) {
    console.log("  Skipping dependency installation");
    return;
  }
  console.log("\n  Installing dependencies...");
  execSync("npm install", { cwd: dir, stdio: "inherit" });
}

// ============================================================================
// Full Generation Pipeline
// ============================================================================

/**
 * Check if a directory exists and is non-empty.
 * @param {string} dirPath
 * @returns {Promise<boolean>}
 */
async function isNonEmptyDir(dirPath) {
  if (!(await fs.pathExists(dirPath))) return false;
  const stat = await fs.stat(dirPath);
  if (!stat.isDirectory()) return false;
  const entries = await fs.readdir(dirPath);
  const visible = entries.filter((e) => !e.startsWith("."));
  return visible.length > 0;
}

/**
 * Verify a GitHub repo exists before attempting clone.
 * Uses `git ls-remote` which fails fast if repo doesn't exist.
 * @param {string} repo - e.g. "PaxeerNetworkNetwork/paxeer-network-plugin-template"
 * @returns {Promise<boolean>}
 */
async function verifyGitHubRepo(repo) {
  console.log(`  Checking repository https://github.com/${repo}...`);
  try {
    execSync(`git ls-remote https://github.com/${repo} HEAD`, {
      cwd: process.cwd(),
      stdio: "pipe",
      timeout: 15000,
    });
    return true;
  } catch (err) {
    if (err.status === 128) {
      throw new Error(
        `Repository "https://github.com/${repo}" not found or is not accessible. ` +
          "Please verify the repository exists and you have access to it.",
      );
    }
    if (
      err.status === null &&
      err.message &&
      err.message.includes("timed out")
    ) {
      throw new Error(
        `Network timeout while connecting to https://github.com/${repo}. ` +
          "Please check your internet connection.",
      );
    }
    throw new Error(
      `Failed to access repository https://github.com/${repo}: ${err.message}`,
    );
  }
}

/**
 * Run the full template generation pipeline.
 * @param {object} config
 * @param {string} config.repo - GitHub repo (e.g. "PaxeerNetworkNetwork/paxeer-network-plugin-template")
 * @param {string} config.targetDir - local output directory
 * @param {object} config.vars - template variables
 * @param {string} config.vars.name - the PascalCase name (e.g. "MyPlugin")
 * @param {boolean} config.skipInstall
 */
async function generateFromTemplate(config) {
  const { repo, targetDir, vars, skipInstall = false } = config;

  // Ensure target dir is empty or does not exist
  if (await isNonEmptyDir(targetDir)) {
    throw new Error(
      `Target directory "${targetDir}" is not empty. Please use an empty directory or a non-existent path.`,
    );
  }

  // Create the target directory if it doesn't exist
  await fs.ensureDir(targetDir);

  // Step 0: Verify repo exists
  await verifyGitHubRepo(repo);

  // Step 1: Download
  await downloadTemplate(repo, targetDir);

  // Step 2: Replace content
  await replaceTemplateVars(targetDir, vars);

  // Step 3: Rename files
  await renameTemplateFiles(targetDir, vars.name);

  // Step 4: Install deps
  installDeps(targetDir, skipInstall);

  console.log(`\n  Plugin generated at: ${targetDir}`);
}

// ============================================================================
// Helpers
// ============================================================================

/**
 * Determine if a file is binary (should skip template processing).
 */
function isBinaryFile(filePath) {
  const ext = path.extname(filePath).toLowerCase();
  const binaryExts = [
    ".png",
    ".jpg",
    ".jpeg",
    ".gif",
    ".bmp",
    ".ico",
    ".woff",
    ".woff2",
    ".ttf",
    ".eot",
    ".otf",
    ".pdf",
    ".zip",
    ".tar",
    ".gz",
    ".rar",
  ];
  return binaryExts.includes(ext);
}

/**
 * Rename a file or directory if its name contains __name__.
 */
async function renamePath(filePath, nameVar) {
  const dir = path.dirname(filePath);
  const basename = path.basename(filePath);

  if (basename.includes("__name__")) {
    const newBasename = basename.replace(/__name__/g, nameVar);
    const newPath = path.join(dir, newBasename);
    await fs.move(filePath, newPath);
  }
}

/**
 * Convert a string to PascalCase.
 */
function toPascalCase(str) {
  return str
    .replace(/[-_\s]+(.)/g, (_, c) => c.toUpperCase())
    .replace(/^(.)/, (c) => c.toUpperCase());
}

/**
 * Convert a string to kebab-case.
 */
function toKebabCase(str) {
  return str
    .replace(/([a-z])([A-Z])/g, "$1-$2")
    .replace(/[\s_]+/g, "-")
    .toLowerCase();
}

/**
 * Convert a string to camelCase.
 */
function toCamelCase(str) {
  return str
    .replace(/[-_\s]+(.)/g, (_, c) => c.toUpperCase())
    .replace(/^(.)/, (c) => c.toLowerCase());
}

/**
 * Validate that a name is in PascalCase.
 */
function validateName(name) {
  if (!name || typeof name !== "string") {
    return { valid: false, error: "Name cannot be empty" };
  }
  if (!/^[A-Z][A-Za-z0-9]*$/.test(name)) {
    return {
      valid: false,
      error: "Name must be in PascalCase (e.g. MyPlugin)",
    };
  }
  return { valid: true };
}

module.exports = {
  downloadTemplate,
  replaceTemplateVars,
  renameTemplateFiles,
  installDeps,
  generateFromTemplate,
  isNonEmptyDir,
  verifyGitHubRepo,
  isBinaryFile,
  toPascalCase,
  toKebabCase,
  toCamelCase,
  validateName,
};
