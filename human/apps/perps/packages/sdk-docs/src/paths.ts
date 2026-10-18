import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

/**
 * Resolve monorepo root from env or by walking up for `pnpm-workspace.yaml`.
 */
export function resolveRepoRoot(cwd = process.cwd()): string {
  const fromEnv = process.env.PAXEER_NETWORK_REPO_ROOT;
  if (fromEnv && fs.existsSync(path.join(fromEnv, "pnpm-workspace.yaml"))) {
    return path.resolve(fromEnv);
  }
  let cur = path.resolve(cwd);
  for (let i = 0; i < 8; i++) {
    if (fs.existsSync(path.join(cur, "pnpm-workspace.yaml"))) return cur;
    const parent = path.dirname(cur);
    if (parent === cur) break;
    cur = parent;
  }
  throw new Error(
    "Could not find repo root (pnpm-workspace.yaml). Set PAXEER_NETWORK_REPO_ROOT or run from the monorepo.",
  );
}

export function aiDocsGeneratedRoot(repoRoot: string) {
  return path.join(repoRoot, "apps/ai-docs/generated");
}

/**
 * Monorepo layout: `{root}/apps/ai-docs/generated/manifest.json`.
 * Packaged flat layout: `{root}/manifest.json` next to `indexes/`.
 */
export function manifestExistsAtRepoRoot(repoRoot: string): boolean {
  const nested = path.join(
    repoRoot,
    "apps",
    "ai-docs",
    "generated",
    "manifest.json",
  );
  if (fs.existsSync(nested)) return true;
  const flatManifest = path.join(repoRoot, "manifest.json");
  const flatIdIndex = path.join(repoRoot, "indexes", "id-index.json");
  return fs.existsSync(flatManifest) && fs.existsSync(flatIdIndex);
}

export type ResolveAiDocsRepoRootOptions = {
  /**
   * `import.meta.url` from a facade module under `dist/` so packaged `bundled/` can be found
   * next to `dist/` (see sync:bundle layout).
   */
  moduleUrl?: string;
};

/**
 * Resolves the synthetic "repo root" where `apps/ai-docs/generated/manifest.json` exists.
 *
 * Precedence:
 * 1. `PAXEER_NETWORK_AI_DOCS_REPO_ROOT` — monorepo root, or a flat `bundled/` dir (`manifest.json` + `indexes/`)
 * 2. `PAXEER_NETWORK_REPO_ROOT` — when it is the monorepo root and manifest exists
 * 3. Walk from cwd for `pnpm-workspace.yaml` (monorepo) when manifest exists
 * 4. Packaged bundle: `{package}/bundled` (flat copy from `sync:bundle`)
 */
export function resolveAiDocsRepoRoot(
  opts?: ResolveAiDocsRepoRootOptions,
): string {
  const explicit = process.env.PAXEER_NETWORK_AI_DOCS_REPO_ROOT;
  if (explicit) {
    const r = path.resolve(explicit);
    if (!manifestExistsAtRepoRoot(r)) {
      throw new Error(
        `PAXEER_NETWORK_AI_DOCS_REPO_ROOT must be a monorepo root with apps/ai-docs/generated, or a flat bundle dir with manifest.json + indexes/ (checked under ${r})`,
      );
    }
    return r;
  }

  const envRepo = process.env.PAXEER_NETWORK_REPO_ROOT;
  if (envRepo) {
    const r = path.resolve(envRepo);
    if (
      fs.existsSync(path.join(r, "pnpm-workspace.yaml")) &&
      manifestExistsAtRepoRoot(r)
    ) {
      return r;
    }
  }

  try {
    const r = resolveRepoRoot();
    if (manifestExistsAtRepoRoot(r)) return r;
  } catch {
    /* not inside monorepo */
  }

  if (opts?.moduleUrl) {
    const bundledRoot = resolveBundledSyntheticRepoRoot(opts.moduleUrl);
    if (bundledRoot && manifestExistsAtRepoRoot(bundledRoot))
      return bundledRoot;
  }

  throw new Error(
    "Could not resolve AI docs data root. Options: set PAXEER_NETWORK_AI_DOCS_REPO_ROOT to a tree with apps/ai-docs/generated, work in a monorepo after `pnpm --filter @paxeer-network/ai-docs generate`, or run `pnpm --filter @paxeer-network/sdk-docs sync:bundle` before using the published package.",
  );
}

/**
 * When the package is installed from npm, `bundled/` (flat generated copy) sits beside `dist/`.
 *
 * @param moduleUrl - typically `import.meta.url` from `dist/chunk-*.js` under `dist/`
 */
export function resolveBundledSyntheticRepoRoot(
  moduleUrl: string,
): string | null {
  const distDir = path.dirname(fileURLToPath(moduleUrl));
  const bundledRoot = path.resolve(distDir, "..", "bundled");
  return manifestExistsAtRepoRoot(bundledRoot) ? bundledRoot : null;
}

/** Reject path traversal in user-supplied relative paths */
export function assertSafeRelPath(rel: string) {
  if (rel.includes("..")) {
    throw new Error("INVALID_PATH_TRAVERSAL");
  }
}
