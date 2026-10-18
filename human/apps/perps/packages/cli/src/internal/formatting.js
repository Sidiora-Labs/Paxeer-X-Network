/**
 * Convert unknown plugin payload into a list shape safely.
 * This keeps CLI output stable across minor API response changes.
 */
function normalizePlugins(data) {
  if (Array.isArray(data)) {
    return data;
  }

  if (Array.isArray(data?.data)) {
    return data.data;
  }

  if (Array.isArray(data?.plugins)) {
    return data.plugins;
  }

  if (Array.isArray(data?.items)) {
    return data.items;
  }

  if (Array.isArray(data?.results)) {
    return data.results;
  }

  if (Array.isArray(data?.data?.items)) {
    return data.data.items;
  }

  if (Array.isArray(data?.data?.plugins)) {
    return data.data.plugins;
  }

  if (Array.isArray(data?.data?.results)) {
    return data.data.results;
  }

  return [];
}

/**
 * Keep table columns compact so terminal output stays readable.
 */
function truncate(value, maxLength = 64) {
  const text = String(value ?? "-");
  if (text.length <= maxLength) {
    return text;
  }
  return `${text.slice(0, maxLength - 3)}...`;
}

const ANSI_PATTERN = /\x1B(?:\[[0-9;]*m|\][^\x07\x1B]*(?:\x07|\x1B\\))/g;

/**
 * Strip ANSI/OSC escape codes for visible-width calculations.
 */
function stripAnsi(value) {
  return String(value ?? "").replace(ANSI_PATTERN, "");
}

/**
 * Visible character length (ignores terminal styling/link escape codes).
 */
function visibleLength(value) {
  return stripAnsi(value).length;
}

/**
 * OSC 8 hyperlink for terminals that support clickable links (iTerm2, VS Code, etc.).
 * Falls back to plain text when stdout is not a TTY.
 */
function terminalLink(text, url) {
  const label = String(text ?? "");
  const href = String(url ?? "").trim();

  if (!label || !href || label === "-") {
    return label || "-";
  }

  if (!process.stdout.isTTY) {
    return label;
  }

  const osc = "\x1b]";
  const st = "\x1b\\";
  return `${osc}8;;${href}${st}${label}${osc}8;;${st}`;
}

/**
 * npm registry URL for a scoped or unscoped package name.
 */
function getNpmPackageUrl(packageName) {
  const name = String(packageName || "").trim();
  if (!name || name === "-") {
    return null;
  }

  return `https://www.npmjs.com/package/${encodeURIComponent(name)}`;
}

/**
 * Parse GitHub repo URL into a display label and canonical https URL.
 * @param {string | null | undefined} repoUrl
 * @returns {{ label: string, url: string } | null}
 */
function parseGithubRepoUrl(repoUrl) {
  const raw = String(repoUrl || "").trim();
  if (!raw) {
    return null;
  }

  const httpsMatch = raw.match(
    /^https?:\/\/github\.com\/([^/]+\/[^/]+?)(?:\.git)?\/?$/i,
  );
  if (httpsMatch) {
    const slug = httpsMatch[1];
    return { label: slug, url: `https://github.com/${slug}` };
  }

  const sshMatch = raw.match(/^git@github\.com:([^/]+\/[^/]+?)(?:\.git)?$/i);
  if (sshMatch) {
    const slug = sshMatch[1];
    return { label: slug, url: `https://github.com/${slug}` };
  }

  if (/^https?:\/\//i.test(raw)) {
    return { label: raw, url: raw };
  }

  return null;
}

function padVisible(value, width) {
  const text = String(value ?? "-");
  const padding = Math.max(0, width - visibleLength(text));
  return `${text}${" ".repeat(padding)}`;
}

/**
 * Render simple aligned table without extra dependencies.
 * @param {Array<Record<string, unknown>>} rows - mapped row objects
 * @param {{ headers: string[], keys: string[] }} columns - column definitions
 */
function renderTable(rows, columns) {
  const { headers, keys } = columns;

  const widths = keys.map((key, index) => {
    const headerWidth = headers[index].length;
    const maxValueWidth = rows.reduce((max, row) => {
      const width = visibleLength(row[key] ?? "-");
      return width > max ? width : max;
    }, 0);
    return Math.max(headerWidth, maxValueWidth);
  });

  const buildLine = (values) =>
    values.map((value, index) => padVisible(value, widths[index])).join("  ");

  const headerLine = buildLine(headers);
  const dividerLine = widths.map((width) => "-".repeat(width)).join("  ");
  const rowLines = rows.map((row) =>
    buildLine(keys.map((key) => row[key] ?? "-")),
  );

  return [headerLine, dividerLine, ...rowLines].join("\n");
}

module.exports = {
  normalizePlugins,
  truncate,
  stripAnsi,
  visibleLength,
  terminalLink,
  getNpmPackageUrl,
  parseGithubRepoUrl,
  renderTable,
};
