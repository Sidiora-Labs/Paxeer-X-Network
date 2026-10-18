import fs from "node:fs";
import path from "node:path";
import { z } from "zod";
import { resolveAiDocsRepoRoot } from "./paths.js";

/** Used to locate packaged `bundled/` when `loadBundle()` is called without an explicit repo root. */
const BUNDLE_LOADER_MODULE_URL = import.meta.url;

export type ComponentDocIndexEntry = {
  entityId: string;
  mdPath: string;
  package: string;
  name: string;
  source: "curated" | "generated";
};

export type Manifest = {
  schemaVersion: string;
  gitSha: string;
  generatedAt: string;
  releaseVersion: string | null;
  artifactKinds: string[];
  roots: { markdownRoot: string; generatedRoot: string };
  indexPaths: {
    idIndex: string;
    symbolIndex: string;
    packageIndex: string;
    keywordIndex: string;
    componentDocIndex?: string;
    /** @deprecated removed from generator */
    chunkIndex?: string;
  };
  qmd?: Record<string, unknown>;
  analysisStats: {
    totalSymbols: number;
    fullyResolved: number;
    degradedCount: number;
  };
};

const ManifestSchema = z
  .object({
    schemaVersion: z.string(),
    gitSha: z.string(),
    generatedAt: z.string(),
    releaseVersion: z.string().nullable(),
    artifactKinds: z.array(z.string()),
    roots: z.object({
      markdownRoot: z.string(),
      generatedRoot: z.string(),
    }),
    indexPaths: z.object({
      idIndex: z.string(),
      symbolIndex: z.string(),
      packageIndex: z.string(),
      keywordIndex: z.string(),
      componentDocIndex: z.string().optional(),
      chunkIndex: z.string().optional(),
    }),
    qmd: z.record(z.string(), z.unknown()).optional(),
    analysisStats: z.object({
      totalSymbols: z.number(),
      fullyResolved: z.number(),
      degradedCount: z.number(),
    }),
  })
  .passthrough();

/** Runtime currently supports major schema version 1 generated artifacts. */
const SUPPORTED_MANIFEST_SCHEMA_MAJOR = 1;

/** Parses semantic or integer schema versions and returns major number when possible. */
function parseManifestSchemaMajor(schemaVersion: string): number | null {
  if (/^\d+$/.test(schemaVersion)) return Number(schemaVersion);
  const semverMatch = /^(\d+)\./.exec(schemaVersion);
  return semverMatch ? Number(semverMatch[1]) : null;
}

/** Enforces that generated artifact schema stays compatible with this runtime. */
function assertManifestCompatibility(manifest: Manifest): void {
  const major = parseManifestSchemaMajor(manifest.schemaVersion);
  if (major === null) {
    throw new Error(
      `Unsupported manifest schemaVersion "${manifest.schemaVersion}" — expected major ${SUPPORTED_MANIFEST_SCHEMA_MAJOR}`,
    );
  }
  if (major !== SUPPORTED_MANIFEST_SCHEMA_MAJOR) {
    throw new Error(
      `Incompatible manifest schema major ${major}; runtime supports ${SUPPORTED_MANIFEST_SCHEMA_MAJOR}. Regenerate artifacts with the matching ai-docs version.`,
    );
  }
}

export type LoadedBundle = {
  repoRoot: string;
  generatedRoot: string;
  manifest: Manifest;
  idIndex: Record<string, unknown>;
  symbolIndex: Record<
    string,
    {
      entityId: string;
      artifactKind: string;
      jsonPath: string;
      sourcePath: string;
      package: string;
    }
  >;
  packageIndex: Record<string, unknown>;
  keywordIndex: Record<string, string[]>;
  componentDocIndex: Record<string, ComponentDocIndexEntry>;
};

let _cache: LoadedBundle | null = null;

/** Older manifests used repo-root paths; strip so joins stay under `generatedRoot`. */
const LEGACY_GENERATED_PREFIX = "apps/ai-docs/generated/";

function relUnderGeneratedRoot(rel: string): string {
  return rel.startsWith(LEGACY_GENERATED_PREFIX)
    ? rel.slice(LEGACY_GENERATED_PREFIX.length)
    : rel;
}

/**
 * Clears the in-memory bundle cache (e.g. after switching `PAXEER_NETWORK_AI_DOCS_REPO_ROOT` in tests).
 */
export function clearLoadBundleCache(): void {
  _cache = null;
}

/**
 * @param repoRoot - Monorepo root (contains `apps/ai-docs/generated/manifest.json`) or any directory
 *   that is a full copy of `generated/` (e.g. `bundled/` after `sync:bundle`). When omitted, uses
 *   {@link resolveAiDocsRepoRoot}.
 */
export function loadBundle(repoRoot?: string): LoadedBundle {
  const root =
    repoRoot ?? resolveAiDocsRepoRoot({ moduleUrl: BUNDLE_LOADER_MODULE_URL });
  if (_cache && _cache.repoRoot === root) return _cache;

  const nestedManifest = path.join(
    root,
    "apps",
    "ai-docs",
    "generated",
    "manifest.json",
  );
  const flatManifest = path.join(root, "manifest.json");

  let generatedRoot: string;
  let manifestPath: string;

  if (fs.existsSync(nestedManifest)) {
    generatedRoot = path.dirname(nestedManifest);
    manifestPath = nestedManifest;
  } else if (fs.existsSync(flatManifest)) {
    const flatIdIndex = path.join(root, "indexes", "id-index.json");
    if (!fs.existsSync(flatIdIndex)) {
      throw new Error(
        `Invalid AI docs data dir at ${root} — expected indexes/id-index.json next to manifest.json`,
      );
    }
    generatedRoot = root;
    manifestPath = flatManifest;
  } else {
    throw new Error(
      `Missing AI docs manifest under ${root} — run pnpm --filter @paxeer-network/ai-docs generate (monorepo) or pnpm --filter @paxeer-network/sdk-docs sync:bundle`,
    );
  }

  const manifest = ManifestSchema.parse(
    JSON.parse(fs.readFileSync(manifestPath, "utf8")),
  ) as Manifest;
  assertManifestCompatibility(manifest);
  const readIdx = (rel: string) =>
    JSON.parse(
      fs.readFileSync(
        path.join(generatedRoot, relUnderGeneratedRoot(rel)),
        "utf8",
      ),
    ) as Record<string, unknown>;

  const idIndex = readIdx(
    manifest.indexPaths.idIndex,
  ) as LoadedBundle["idIndex"];
  const symbolIndex = readIdx(
    manifest.indexPaths.symbolIndex,
  ) as LoadedBundle["symbolIndex"];
  const packageIndex = readIdx(
    manifest.indexPaths.packageIndex,
  ) as LoadedBundle["packageIndex"];
  const keywordIndex = readIdx(
    manifest.indexPaths.keywordIndex,
  ) as LoadedBundle["keywordIndex"];
  let componentDocIndex: LoadedBundle["componentDocIndex"] = {};
  const docRel = manifest.indexPaths.componentDocIndex;
  if (docRel) {
    componentDocIndex = readIdx(docRel) as LoadedBundle["componentDocIndex"];
  }
  _cache = {
    repoRoot: root,
    generatedRoot,
    manifest,
    idIndex,
    symbolIndex,
    packageIndex,
    keywordIndex,
    componentDocIndex,
  };
  return _cache;
}

/**
 * @param generatedRoot - {@link LoadedBundle.generatedRoot} (the `apps/ai-docs/generated` folder or a synced `bundled/` copy)
 * @param jsonRelPath - e.g. symbol index `jsonPath` (`json/hooks.json`)
 */
export function readEntityJson<T>(
  generatedRoot: string,
  jsonRelPath: string,
): T[] {
  const abs = path.join(generatedRoot, relUnderGeneratedRoot(jsonRelPath));
  return JSON.parse(fs.readFileSync(abs, "utf8")) as T[];
}
