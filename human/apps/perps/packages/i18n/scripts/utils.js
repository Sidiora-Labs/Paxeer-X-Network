const fs = require("fs-extra");

async function checkFileExists(filePath) {
  if (await fs.exists(filePath)) {
    throw new Error(`${filePath} already exists, please modify file path`);
  }
}

const i18nValidErrors = {
  string: "Value must be a string",
  empty: "Value cannot be empty",
  interpolation: "Invalid interpolation",
  mismatchedClosingTag: (tagName) => `Mismatched closing tag: </${tagName}>`,
  unclosedTag: (tagName) => `Unclosed tag: <${tagName}>`,
};

/**
 * Validate the value of i18n
 * @param {string} value - The value to validate
 * @returns {Object} - The validation result
 */
function validateI18nValue(value) {
  if (typeof value !== "string") {
    return { valid: false, error: i18nValidErrors.string };
  }

  // 1. check if value is empty
  if (value.trim() === "") {
    return { valid: false, error: i18nValidErrors.empty };
  }

  // 2. check if placeholder format is correct (allow `{{variable}}`)
  // allow `{{variable}}` or `{{ variable }}`
  const interpolationRegex = /{{\s*[\w.-]+\s*}}/g;
  const strippedInterpolation = value.replace(interpolationRegex, "");
  if (
    strippedInterpolation.includes("{{") ||
    strippedInterpolation.includes("}}")
  ) {
    return { valid: false, error: i18nValidErrors.interpolation };
  }
  // mistaken single-brace placeholders like `{user.name}` (should be `{{user.name}}`)
  if (/\{\s*[\w.-]+\s*\}/.test(strippedInterpolation)) {
    return { valid: false, error: i18nValidErrors.interpolation };
  }
  // `{` without a closing `}` on the same line segment (typo vs `{{`)
  if (/\{[^}]*$/.test(strippedInterpolation)) {
    return { valid: false, error: i18nValidErrors.interpolation };
  }

  // 3. check if HTML tags are correctly closed (supports attributes, e.g. <script async src="...">)
  const tagRegex = /<(\/?)\s*([a-zA-Z0-9][\w-]*)[^>]*>/g;
  let stack = [];
  let match;

  while ((match = tagRegex.exec(value)) !== null) {
    const [, slashPrefix, tagName] = match;
    const fullTag = match[0];
    const isClosing = slashPrefix === "/";
    const isSelfClosing = !isClosing && /\/\s*>$/.test(fullTag.trim());

    if (isSelfClosing) {
      continue;
    }
    if (isClosing) {
      if (stack.length === 0 || stack.pop() !== tagName) {
        return {
          valid: false,
          error: i18nValidErrors.mismatchedClosingTag(tagName),
        };
      }
    } else {
      stack.push(tagName);
    }
  }

  if (stack.length > 0) {
    return { valid: false, error: i18nValidErrors.unclosedTag(stack.pop()) };
  }

  return { valid: true, error: null };
}

async function findJsonFiles(dir) {
  const files = await fs.readdir(dir);
  return files.filter((file) => file.endsWith(".json"));
}

module.exports = {
  checkFileExists,
  validateI18nValue,
  i18nValidErrors,
  findJsonFiles,
};
