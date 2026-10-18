/** Valid Marketplace plugin tags (must match @dex-creator/schemas MarketplaceTags). */
const VALID_TAGS = [
  "UI",
  "Indicator",
  "Order Entry",
  "Trading",
  "Chart",
  "Portfolio",
  "Analytics",
  "Market",
  "OrderBook",
  "Deposit",
  "Withdraw",
  "Transfer",
  "Swap",
  "Liquidity",
  "Staking",
  "Rewards",
  "Tool",
  "Widget",
  "AI",
];

const MAX_TAGS = 5;
const MIN_TAGS = 1;
const MAX_COVER_IMAGES = 5;
const MIN_COVER_IMAGES = 1;
const MAX_NAME_LENGTH = 120;
const MAX_DESCRIPTION_LENGTH = 500;
const MAX_NPM_NAME_LENGTH = 214;
const MAX_USAGE_PROMPT_LENGTH = 8192;

/**
 * Regex patterns matching @dex-creator/schemas. The repo URL pattern allows
 * dots in owner/repo names and tolerates a trailing slash or `.git` suffix,
 * both of which the backend strips.
 */
const NPM_NAME_REGEX =
  /^(?:@[a-z0-9-*~][a-z0-9-*._~]*\/)?[a-z0-9-~][a-z0-9-._~]*$/;
const GITHUB_URL_REGEX = /^https:\/\/github\.com\/[\w.-]+\/[\w.-]+\/?$/;
const PLUGIN_ID_REGEX = /^[a-zA-Z][a-zA-Z0-9-]*$/;

/**
 * Split tags into valid and invalid buckets against the Marketplace whitelist.
 * @param {string[]} tags
 * @returns {{ validTags: string[], invalidTags: string[] }}
 */
function partitionTags(tags) {
  const validTags = tags.filter((tag) => VALID_TAGS.includes(tag));
  const invalidTags = tags.filter((tag) => !VALID_TAGS.includes(tag));
  return { validTags, invalidTags };
}

/**
 * Validate submission payload against backend schema rules
 * (@dex-creator/schemas MarketplaceSubmitSchema).
 * @returns {{ valid: boolean, errors: string[] }}
 */
function validateSubmission({
  npmName,
  repoUrl,
  pluginId,
  name,
  description,
  tags,
  coverImages,
  usagePrompt,
}) {
  const errors = [];

  if (!npmName) {
    errors.push("npmName is required (set in package.json)");
  } else if (typeof npmName !== "string") {
    errors.push("npmName must be a string (set in package.json)");
  } else if (npmName.trim().length > MAX_NPM_NAME_LENGTH) {
    errors.push(
      `npmName is too long (${npmName.trim().length} chars), maximum is ${MAX_NPM_NAME_LENGTH}`,
    );
  } else if (!NPM_NAME_REGEX.test(npmName)) {
    errors.push(`npmName "${npmName}" is not a valid npm package name`);
  }

  if (!repoUrl) {
    errors.push(
      "repoUrl is required (configure git remote or set in manifest)",
    );
  } else if (!GITHUB_URL_REGEX.test(repoUrl)) {
    errors.push(
      `repoUrl must be a valid GitHub URL (https://github.com/<owner>/<repo>), got: ${repoUrl}`,
    );
  }

  // Optional: the backend derives the plugin ID from the npm name when omitted.
  if (pluginId && !PLUGIN_ID_REGEX.test(pluginId)) {
    errors.push(
      `pluginId must start with a letter and contain only letters, digits, or hyphens, got: ${pluginId}`,
    );
  }

  if (!name || name.trim().length === 0) {
    errors.push("name is required (set in manifest or answer the prompt)");
  } else if (name.trim().length > MAX_NAME_LENGTH) {
    errors.push(
      `name is too long (${name.trim().length} chars), maximum is ${MAX_NAME_LENGTH}`,
    );
  }

  if (!description || description.trim().length === 0) {
    errors.push(
      "description is required (set in manifest/package.json or answer the prompt)",
    );
  } else if (description.trim().length > MAX_DESCRIPTION_LENGTH) {
    errors.push(
      `description is too long (${description.trim().length} chars), maximum is ${MAX_DESCRIPTION_LENGTH}`,
    );
  }

  if (tags.length < MIN_TAGS) {
    errors.push(
      `At least ${MIN_TAGS} tag is required (e.g., UI, Trading, Tool)`,
    );
  }
  if (tags.length > MAX_TAGS) {
    errors.push(`Too many tags (${tags.length}), maximum is ${MAX_TAGS}`);
  }

  if (!coverImages || coverImages.length < MIN_COVER_IMAGES) {
    errors.push(
      `At least ${MIN_COVER_IMAGES} cover image is required (list local files under coverImages in the manifest; they are uploaded automatically)`,
    );
  }
  if (coverImages && coverImages.length > MAX_COVER_IMAGES) {
    errors.push(
      `Too many cover images (${coverImages.length}), maximum is ${MAX_COVER_IMAGES}`,
    );
  }

  if (usagePrompt && usagePrompt.length > MAX_USAGE_PROMPT_LENGTH) {
    errors.push(
      `usagePrompt is too long (${usagePrompt.length} chars), maximum is ${MAX_USAGE_PROMPT_LENGTH}`,
    );
  }

  return { valid: errors.length === 0, errors };
}

/**
 * Validate update payload against marketplace schema constraints.
 * @param {object} payload
 * @returns {{ valid: boolean, errors: string[] }}
 */
function validateUpdatePayload(payload) {
  const errors = [];

  for (const field of ["name", "description"]) {
    if (!(field in payload)) {
      continue;
    }

    const value = payload[field];
    if (typeof value !== "string") {
      errors.push(`${field} must be a string`);
      continue;
    }

    if (value.trim().length === 0) {
      errors.push(`${field} must not be empty`);
      continue;
    }

    const maxLength =
      field === "name" ? MAX_NAME_LENGTH : MAX_DESCRIPTION_LENGTH;
    if (value.trim().length > maxLength) {
      errors.push(
        `${field} is too long (${value.trim().length} chars), maximum is ${maxLength}`,
      );
    }
  }

  if (payload.tags && payload.tags.length > MAX_TAGS) {
    errors.push(
      `Too many tags (${payload.tags.length}), maximum is ${MAX_TAGS}`,
    );
  }

  if (payload.coverImages && payload.coverImages.length > MAX_COVER_IMAGES) {
    errors.push(
      `Too many cover images (${payload.coverImages.length}), maximum is ${MAX_COVER_IMAGES}`,
    );
  }

  if ("usagePrompt" in payload) {
    if (
      payload.usagePrompt !== null &&
      typeof payload.usagePrompt !== "string"
    ) {
      errors.push("usagePrompt must be a string or null");
    } else if (
      typeof payload.usagePrompt === "string" &&
      payload.usagePrompt.length > MAX_USAGE_PROMPT_LENGTH
    ) {
      errors.push(
        `usagePrompt is too long (${payload.usagePrompt.length} chars), maximum is ${MAX_USAGE_PROMPT_LENGTH}`,
      );
    }
  }

  return { valid: errors.length === 0, errors };
}

module.exports = {
  VALID_TAGS,
  MAX_TAGS,
  MIN_TAGS,
  MAX_COVER_IMAGES,
  MIN_COVER_IMAGES,
  MAX_NAME_LENGTH,
  MAX_DESCRIPTION_LENGTH,
  MAX_NPM_NAME_LENGTH,
  MAX_USAGE_PROMPT_LENGTH,
  NPM_NAME_REGEX,
  GITHUB_URL_REGEX,
  PLUGIN_ID_REGEX,
  partitionTags,
  validateSubmission,
  validateUpdatePayload,
};
