import fs from "node:fs";
import path from "node:path";
import type { LoadedBundle } from "./bundle.js";
import { readEntityJson } from "./bundle.js";
import {
  resolveEntityByArtifactKind,
  type SymbolIndexEntry,
} from "./entityResolve.js";
import { errResult, okResult } from "./envelope.js";
import type { Citation, DocsResult } from "./types.js";

type JsonEntity = { id?: string; name?: string; artifactKind?: string };

function findInShard(
  generatedRoot: string,
  jsonRelPath: string,
  entityId: string,
): JsonEntity | undefined {
  const rows = readEntityJson<JsonEntity>(generatedRoot, jsonRelPath);
  return rows.find((r) => r.id === entityId);
}

function entityCitation(
  entry: { sourcePath: string; entityId: string },
  via: string,
): Citation {
  return {
    path: entry.sourcePath,
    why: via,
    id: entry.entityId,
  };
}

/**
 * Converts an interceptor target path (e.g. `Trading.OrderEntry.SubmitSection`) into PascalCase candidates.
 * This gives us stable fallback names to probe props types when component entities are absent.
 */
function interceptorTargetCandidates(query: string): {
  componentName: string;
  propsTypeCandidates: string[];
} | null {
  const trimmed = query.trim();
  if (!trimmed.includes(".")) return null;
  const rawParts = trimmed
    .split(".")
    .map((p) => p.trim())
    .filter(Boolean);
  if (rawParts.length < 2) return null;

  const parts = rawParts.map((part) =>
    part
      .replace(/[^A-Za-z0-9]+/g, " ")
      .split(/\s+/)
      .filter(Boolean)
      .map((s) => s.charAt(0).toUpperCase() + s.slice(1))
      .join(""),
  );
  const tailComponent = parts.slice(1).join("");
  const leafComponent = parts[parts.length - 1] ?? "";
  const componentName = tailComponent || leafComponent;
  if (!componentName) return null;

  const propsTypeCandidates = [
    ...new Set([`${componentName}Props`, `${leafComponent}Props`]),
  ];
  return { componentName, propsTypeCandidates };
}

/**
 * Resolves a likely props type from an interceptor target when no component entity exists in indexes.
 */
function resolveInterceptorPropsFallback(
  bundle: LoadedBundle,
  query: string,
): {
  typeEntityId: string;
  typeEntry: SymbolIndexEntry;
  componentName: string;
  propsTypeName: string;
} | null {
  const candidates = interceptorTargetCandidates(query);
  if (!candidates) return null;

  for (const typeName of candidates.propsTypeCandidates) {
    const resolvedType = resolveEntityByArtifactKind(bundle, typeName, "type");
    if (resolvedType.ok) {
      return {
        typeEntityId: resolvedType.entityId,
        typeEntry: resolvedType.entry,
        componentName: candidates.componentName,
        propsTypeName: typeName,
      };
    }
  }
  return null;
}

/**
 * Exact structured lookup for a React component record (props metadata, etc.).
 */
export function getComponent(
  bundle: LoadedBundle,
  query: string,
  started: number,
): DocsResult<Record<string, unknown>> {
  const resolved = resolveEntityByArtifactKind(bundle, query, "component");
  if (!resolved.ok) {
    if (resolved.code === "NOT_FOUND") {
      const fallback = resolveInterceptorPropsFallback(bundle, query);
      if (fallback) {
        const typeRecord = findInShard(
          bundle.generatedRoot,
          fallback.typeEntry.jsonPath,
          fallback.typeEntityId,
        );
        if (typeRecord) {
          return okResult(
            bundle,
            {
              id: `component.synthetic.${fallback.componentName}`,
              name: fallback.componentName,
              displayName: fallback.componentName,
              artifactKind: "component",
              resolutionLevel: "interceptor-props-fallback",
              sourceKind: "type-fallback",
              propsTypeId: fallback.typeEntityId,
              propsTypeName: fallback.propsTypeName,
              propsType: typeRecord,
            } as Record<string, unknown>,
            "exact",
            [
              entityCitation(
                {
                  sourcePath: fallback.typeEntry.sourcePath,
                  entityId: fallback.typeEntityId,
                },
                "interceptor target fallback via type metadata",
              ),
            ],
            started,
          );
        }
      }
    }
    return errResult(
      resolved.code,
      resolved.message,
      resolved.hint,
      bundle,
    ) as DocsResult<Record<string, unknown>>;
  }
  const { entityId, entry } = resolved;
  const record = findInShard(bundle.generatedRoot, entry.jsonPath, entityId);
  if (!record) {
    return errResult(
      "NOT_FOUND",
      `Component "${entityId}" missing from shard ${entry.jsonPath}.`,
      "Regenerate AI docs (pnpm --filter @paxeer-network/ai-docs generate).",
      bundle,
    );
  }
  const resLevel = (record as { resolutionLevel?: string }).resolutionLevel;
  return okResult(
    bundle,
    record as Record<string, unknown>,
    "exact",
    [
      entityCitation(
        { sourcePath: entry.sourcePath, entityId },
        "symbol-index + json/components",
      ),
    ],
    started,
    resLevel === "syntax-only"
      ? { resolutionLevel: "syntax-only" as const }
      : undefined,
  );
}

/**
 * Exact structured lookup for a type / enum record.
 */
export function getType(
  bundle: LoadedBundle,
  query: string,
  started: number,
): DocsResult<Record<string, unknown>> {
  const resolved = resolveEntityByArtifactKind(bundle, query, "type");
  if (!resolved.ok) {
    return errResult(
      resolved.code,
      resolved.message,
      resolved.hint,
      bundle,
    ) as DocsResult<Record<string, unknown>>;
  }
  const { entityId, entry } = resolved;
  const record = findInShard(bundle.generatedRoot, entry.jsonPath, entityId);
  if (!record) {
    return errResult(
      "NOT_FOUND",
      `Type "${entityId}" missing from shard ${entry.jsonPath}.`,
      "Regenerate AI docs (pnpm --filter @paxeer-network/ai-docs generate).",
      bundle,
    );
  }
  return okResult(
    bundle,
    record as Record<string, unknown>,
    "exact",
    [
      entityCitation(
        { sourcePath: entry.sourcePath, entityId },
        "symbol-index + json/types",
      ),
    ],
    started,
  );
}

/**
 * Exact structured lookup for a React hook record (params, returns, jsDoc).
 */
export function getHook(
  bundle: LoadedBundle,
  query: string,
  started: number,
): DocsResult<Record<string, unknown>> {
  const resolved = resolveEntityByArtifactKind(bundle, query, "hook");
  if (!resolved.ok) {
    return errResult(
      resolved.code,
      resolved.message,
      resolved.hint,
      bundle,
    ) as DocsResult<Record<string, unknown>>;
  }
  const { entityId, entry } = resolved;
  const record = findInShard(bundle.generatedRoot, entry.jsonPath, entityId);
  if (!record) {
    return errResult(
      "NOT_FOUND",
      `Hook "${entityId}" missing from shard ${entry.jsonPath}.`,
      "Regenerate AI docs (pnpm --filter @paxeer-network/ai-docs generate).",
      bundle,
    );
  }
  const resLevel = (record as { resolutionLevel?: string }).resolutionLevel;
  return okResult(
    bundle,
    record as Record<string, unknown>,
    "exact",
    [
      entityCitation(
        { sourcePath: entry.sourcePath, entityId },
        "symbol-index + json/hooks",
      ),
    ],
    started,
    resLevel === "syntax-only"
      ? { resolutionLevel: "syntax-only" as const }
      : undefined,
  );
}

export type PackageSurfaceData = {
  packageName: string;
  exports: string[];
  componentIds: string[];
  hookIds: string[];
  typeIds: string[];
};

/**
 * Maps common user shorthand to canonical paxeer-network package names.
 */
const PACKAGE_ALIASES: Record<string, string> = {
  layout: "@paxeer-network/layout-core",
  "layout-core": "@paxeer-network/layout-core",
  plugin: "@paxeer-network/plugin-core",
  "plugin-core": "@paxeer-network/plugin-core",
  ui: "@paxeer-network/ui",
};

function normalizePkgName(name: string): string {
  return name.trim().toLowerCase();
}

function resolvePackageName(
  bundle: LoadedBundle,
  packageName: string,
): { resolved?: string; didYouMean?: string[] } {
  const raw = packageName.trim();
  if (!raw) return {};
  if (bundle.packageIndex[raw]) return { resolved: raw };
  const normalized = normalizePkgName(raw);
  const aliasHit = PACKAGE_ALIASES[normalized];
  if (aliasHit && bundle.packageIndex[aliasHit]) {
    return { resolved: aliasHit };
  }

  const all = Object.keys(bundle.packageIndex);
  const ranked = all
    .map((k) => ({
      key: k,
      score:
        (k.toLowerCase().includes(normalized) ? 3 : 0) +
        (normalized.includes(k.toLowerCase()) ? 2 : 0),
    }))
    .filter((x) => x.score > 0)
    .sort((a, b) => b.score - a.score)
    .slice(0, 3)
    .map((x) => x.key);
  return { didYouMean: ranked.length ? ranked : undefined };
}

/**
 * Returns the indexed export surface for an npm package name (e.g. `@paxeer-network/ui`).
 */
export function getPackageSurface(
  bundle: LoadedBundle,
  packageName: string,
  started: number,
): DocsResult<PackageSurfaceData> {
  const name = packageName.trim();
  if (!name) {
    return errResult(
      "INVALID_INPUT",
      "packageName must be non-empty.",
      undefined,
      bundle,
    );
  }
  const { resolved, didYouMean } = resolvePackageName(bundle, name);
  const lookupName = resolved ?? name;
  const row = bundle.packageIndex[lookupName] as PackageSurfaceData | undefined;
  if (!row || !Array.isArray(row.exports)) {
    return errResult(
      "NOT_FOUND",
      `No package index entry for "${name}".`,
      didYouMean?.length
        ? `Did you mean: ${didYouMean.join(", ")}`
        : "Use the full package name such as @paxeer-network/layout-core.",
      bundle,
    );
  }
  const data: PackageSurfaceData = {
    packageName: lookupName,
    exports: row.exports,
    componentIds: Array.isArray(row.componentIds) ? row.componentIds : [],
    hookIds: Array.isArray(row.hookIds) ? row.hookIds : [],
    typeIds: Array.isArray(row.typeIds) ? row.typeIds : [],
  };
  return okResult(
    bundle,
    data,
    "exact",
    [{ path: name, why: "package-index" }],
    started,
  );
}

export type ReleaseContextData = {
  manifest: LoadedBundle["manifest"];
  buildStamp: { gitSha: string; generatedAt: string } | null;
};

/**
 * Manifest + optional `build-stamp.json` for release / provenance context.
 */
export function getReleaseContext(
  bundle: LoadedBundle,
  started: number,
): DocsResult<ReleaseContextData> {
  const stampPath = path.join(bundle.generatedRoot, "build-stamp.json");
  let buildStamp: ReleaseContextData["buildStamp"] = null;
  if (fs.existsSync(stampPath)) {
    try {
      buildStamp = JSON.parse(fs.readFileSync(stampPath, "utf8")) as {
        gitSha: string;
        generatedAt: string;
      };
    } catch {
      buildStamp = null;
    }
  }
  return okResult(
    bundle,
    { manifest: bundle.manifest, buildStamp },
    "manifest",
    [{ path: "manifest.json", why: "release-context" }],
    started,
  );
}
