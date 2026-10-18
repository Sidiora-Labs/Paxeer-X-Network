import fs from "node:fs";
import path from "node:path";
import type { LoadedBundle } from "./bundle.js";
import { errResult, okResult } from "./envelope.js";
import { resolveRepoRoot } from "./paths.js";
import type { DocsResult } from "./types.js";

export type NarrativeDocBody = {
  path: string;
  /** Full file text (no chunk-index fallback). */
  text: string;
  sections: Array<{
    id: string;
    heading: string;
    text: string;
    startLine?: number;
    endLine?: number;
  }>;
  source: "repo-markdown";
};

function normalizeSlug(raw: string): string {
  const s = raw.trim().replace(/\.md$/i, "");
  if (!s || s.includes("..") || s.includes("/") || s.includes("\\")) {
    return "";
  }
  return s;
}

/**
 * Converts a slug-ish input to a comparison token for fuzzy matching.
 * Keeps only alphanumeric chars so `plugin-create`, `plugin_create`,
 * and `plugin create` are treated as equivalent.
 */
function normalizeLooseToken(raw: string): string {
  return raw.toLowerCase().replace(/[^a-z0-9]+/g, "");
}

function markdownRoot(bundle: LoadedBundle): string {
  const mr = bundle.manifest.roots?.markdownRoot ?? "apps/ai-docs";
  return mr;
}

/**
 * Lists markdown basenames from a folder under apps/ai-docs.
 * Returns [] when repo root is unavailable, keeping callers resilient
 * in bundled/non-monorepo runtimes.
 */
function listMarkdownBasenames(
  bundle: LoadedBundle,
  subdir: "workflows" | "recipes",
): string[] {
  try {
    const root = resolveRepoRoot();
    const absDir = path.join(root, markdownRoot(bundle), subdir);
    if (!fs.existsSync(absDir) || !fs.statSync(absDir).isDirectory()) {
      return [];
    }
    return fs
      .readdirSync(absDir)
      .filter((name) => name.toLowerCase().endsWith(".md"))
      .map((name) => name.replace(/\.md$/i, ""));
  } catch {
    return [];
  }
}

/**
 * Resolves a user input into a concrete markdown basename.
 * Strategy:
 * 1) exact basename match
 * 2) normalized-token exact match (separator-insensitive)
 * 3) unique partial-token match
 */
function resolveMarkdownSlug(
  input: string,
  available: string[],
): { ok: true; slug: string } | { ok: false; message: string } {
  if (available.includes(input)) {
    return { ok: true, slug: input };
  }
  const inputToken = normalizeLooseToken(input);
  if (!inputToken) {
    return {
      ok: false,
      message: "Invalid input after normalization.",
    };
  }

  const tokenMatches = available.filter(
    (entry) => normalizeLooseToken(entry) === inputToken,
  );
  if (tokenMatches.length === 1) {
    return { ok: true, slug: tokenMatches[0] };
  }
  if (tokenMatches.length > 1) {
    return {
      ok: false,
      message: `Ambiguous input. Candidates: ${tokenMatches.join(", ")}`,
    };
  }

  const partialMatches = available.filter((entry) => {
    const token = normalizeLooseToken(entry);
    return token.includes(inputToken) || inputToken.includes(token);
  });
  if (partialMatches.length === 1) {
    return { ok: true, slug: partialMatches[0] };
  }
  if (partialMatches.length > 1) {
    return {
      ok: false,
      message: `Ambiguous input. Candidates: ${partialMatches.join(", ")}`,
    };
  }
  return {
    ok: false,
    message: "No matching document slug found.",
  };
}

/**
 * Read markdown under `apps/ai-docs/...` from the resolved monorepo root.
 */
function tryReadMarkdownFile(
  bundle: LoadedBundle,
  relFromMarkdownRoot: string,
): string | null {
  try {
    const root = resolveRepoRoot();
    const abs = path.join(root, markdownRoot(bundle), relFromMarkdownRoot);
    if (fs.existsSync(abs) && fs.statSync(abs).isFile()) {
      return fs.readFileSync(abs, "utf8");
    }
  } catch {
    /* not in monorepo or PAXEER_NETWORK_REPO_ROOT invalid */
  }
  return null;
}

function readOrNotFound(
  bundle: LoadedBundle,
  relFromMarkdownRoot: string,
  citationPath: string,
  started: number,
  label: string,
): DocsResult<NarrativeDocBody> {
  const repoFull = tryReadMarkdownFile(bundle, relFromMarkdownRoot);
  if (repoFull !== null) {
    return okResult(
      bundle,
      {
        path: citationPath,
        text: repoFull,
        sections: [],
        source: "repo-markdown",
      },
      "narrative",
      [{ path: citationPath, why: `${label} (repo file)` }],
      started,
    );
  }
  return errResult(
    "NOT_FOUND",
    `Missing markdown for ${label} at ${citationPath}.`,
    "Use a monorepo checkout with apps/ai-docs, or copy the workflow/recipe files next to your bundle.",
    bundle,
  );
}

/**
 * Curated workflow markdown (repo file only).
 */
export function getWorkflow(
  bundle: LoadedBundle,
  slug: string,
  started: number,
): DocsResult<NarrativeDocBody> {
  const s = normalizeSlug(slug);
  if (!s) {
    return errResult(
      "INVALID_INPUT",
      "Invalid workflow slug (use a single filename segment without path separators).",
      undefined,
      bundle,
    );
  }
  const available = listMarkdownBasenames(bundle, "workflows");
  const resolved = resolveMarkdownSlug(s, available);
  if (!resolved.ok) {
    return errResult(
      "INVALID_INPUT",
      `Unable to resolve workflow slug "${slug}". ${resolved.message}`,
      available.length
        ? `Try one of: ${available.join(", ")}`
        : "Run in monorepo context so workflow files can be discovered.",
      bundle,
    );
  }
  const rel = path.join("workflows", `${resolved.slug}.md`);
  const citationPath = path.join(markdownRoot(bundle), rel).replace(/\\/g, "/");
  return readOrNotFound(
    bundle,
    rel,
    citationPath,
    started,
    `workflow:${resolved.slug}`,
  );
}

/**
 * Minimal recipe markdown (repo file only).
 */
export function getRecipe(
  bundle: LoadedBundle,
  name: string,
  started: number,
): DocsResult<NarrativeDocBody> {
  const s = normalizeSlug(name);
  if (!s) {
    return errResult(
      "INVALID_INPUT",
      "Invalid recipe name (use a single filename segment without path separators).",
      undefined,
      bundle,
    );
  }
  const available = listMarkdownBasenames(bundle, "recipes");
  const resolved = resolveMarkdownSlug(s, available);
  if (!resolved.ok) {
    return errResult(
      "INVALID_INPUT",
      `Unable to resolve recipe name "${name}". ${resolved.message}`,
      available.length
        ? `Try one of: ${available.join(", ")}`
        : "Run in monorepo context so recipe files can be discovered.",
      bundle,
    );
  }
  const rel = path.join("recipes", `${resolved.slug}.md`);
  const citationPath = path.join(markdownRoot(bundle), rel).replace(/\\/g, "/");
  return readOrNotFound(
    bundle,
    rel,
    citationPath,
    started,
    `recipe:${resolved.slug}`,
  );
}

/**
 * Guardrails document (repo file only).
 */
export function getGuardrails(
  bundle: LoadedBundle,
  started: number,
): DocsResult<NarrativeDocBody> {
  const rel = "guardrails.md";
  const citationPath = path.join(markdownRoot(bundle), rel).replace(/\\/g, "/");
  return readOrNotFound(bundle, rel, citationPath, started, "guardrails");
}
