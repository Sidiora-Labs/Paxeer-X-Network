import fs from "node:fs";
import path from "node:path";
import type { LoadedBundle } from "./bundle.js";
import { resolveEntityByArtifactKind } from "./entityResolve.js";
import { errResult, okResult } from "./envelope.js";
import type { NarrativeDocBody } from "./narrativeDocs.js";
import { resolveRepoRoot } from "./paths.js";
import type { DocsResult } from "./types.js";

/**
 * Resolve absolute path to component markdown (curated under repo root, generated under generated root).
 */
export function resolveComponentDocAbsPath(
  bundle: LoadedBundle,
  entityId: string,
): string | null {
  const row = bundle.componentDocIndex[entityId];
  if (!row) return null;
  if (row.source === "curated") {
    try {
      return path.join(resolveRepoRoot(), row.mdPath);
    } catch {
      return null;
    }
  }
  return path.join(bundle.generatedRoot, row.mdPath);
}

/**
 * Full component markdown for examples / narrative (JSON props stay on getComponent).
 */
export function getComponentDoc(
  bundle: LoadedBundle,
  query: string,
  started: number,
): DocsResult<NarrativeDocBody> {
  const resolved = resolveEntityByArtifactKind(bundle, query, "component");
  if (!resolved.ok) {
    return errResult(
      resolved.code,
      resolved.message,
      resolved.hint,
      bundle,
    ) as DocsResult<NarrativeDocBody>;
  }
  const row = bundle.componentDocIndex[resolved.entityId];
  if (!row) {
    return errResult(
      "NOT_FOUND",
      `No component markdown index entry for "${resolved.entityId}".`,
      "Run pnpm --filter @paxeer-network/ai-docs generate.",
      bundle,
    );
  }
  const abs = resolveComponentDocAbsPath(bundle, resolved.entityId);
  if (!abs || !fs.existsSync(abs)) {
    return errResult(
      "NOT_FOUND",
      `Component markdown file missing (${row.mdPath}).`,
      "Run generate from the monorepo root, or sync bundled artifacts.",
      bundle,
    );
  }
  const text = fs.readFileSync(abs, "utf8");
  const pathForCitation =
    path.relative(bundle.repoRoot, abs).replace(/\\/g, "/") || row.mdPath;
  return okResult(
    bundle,
    { path: pathForCitation, text, sections: [], source: "repo-markdown" },
    "narrative",
    [{ path: pathForCitation, why: "component-doc-index + file" }],
    started,
  );
}
