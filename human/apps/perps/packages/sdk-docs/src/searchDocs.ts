import type { LoadedBundle } from "./bundle.js";
import { errResult, okResult } from "./envelope.js";
import type { PackageSurfaceData } from "./exactLookup.js";
import type { NarrativeRetrieval } from "./narrative/narrativeRetrieval.js";
import type { Citation, DocsResult, NarrativeHit } from "./types.js";

export type SearchDocsInput = {
  query: string;
  k?: number;
  kinds?: string[];
  packages?: string[];
};

export type SearchDocsData = {
  narrativeHits: NarrativeHit[];
  /** Set when the query equals a known id-index key (agent can call getComponent/getType/getHook next). */
  matchedEntityId?: string;
  /** The artifactKind of the matched entity (component, type, hook, function). */
  matchedEntityKind?: string;
  /** When the first whitespace-delimited token maps to exactly one keyword-index entry. */
  keywordSingletonId?: string;
  /** The artifactKind of the keyword singleton entity. */
  keywordSingletonKind?: string;
  /** Query variants used by retrieval (English-only normalization in current phase). */
  queryVariants?: string[];
  /** Inferred package candidates from user intent. */
  inferredPackages?: string[];
  /** Lightweight package surface hints for likely next exact lookup calls. */
  packageSurfaceHints?: Array<{
    packageName: string;
    exportsPreview: string[];
    componentIdsPreview: string[];
  }>;
};

function looksLikeEntityId(q: string): boolean {
  return /^[a-z][a-z0-9_]*\.[A-Za-z0-9_.]+$/.test(q);
}

function normalizeEnglishQuery(q: string): string {
  // Keep this conservative to avoid semantic drift; primary quality gains come from metadata/ranking.
  return q
    .toLowerCase()
    .replace(/[?.,!;:()[\]{}"']/g, " ")
    .replace(/\s+/g, " ")
    .trim();
}

function inferPackagesFromQuery(query: string): string[] {
  const q = ` ${normalizeEnglishQuery(query)} `;
  const out = new Set<string>();
  // Intent mapping for layout/plugin retrieval guidance.
  if (q.includes(" layout ")) out.add("@paxeer-network/layout-core");
  if (
    q.includes(" plugin ") ||
    q.includes(" interceptor ") ||
    q.includes(" injectable ")
  ) {
    out.add("@paxeer-network/plugin-core");
  }
  return [...out];
}

function buildPackageSurfaceHints(
  bundle: LoadedBundle,
  inferredPackages: string[],
): SearchDocsData["packageSurfaceHints"] {
  return inferredPackages
    .map((name) => {
      const row = bundle.packageIndex[name] as PackageSurfaceData | undefined;
      if (!row || !Array.isArray(row.exports)) return null;
      return {
        packageName: name,
        exportsPreview: row.exports.slice(0, 8),
        componentIdsPreview: Array.isArray(row.componentIds)
          ? row.componentIds.slice(0, 8)
          : [],
      };
    })
    .filter((x): x is NonNullable<typeof x> => x !== null);
}

/**
 * Narrative search via QMD only; optional id/keyword hints from in-memory JSON indexes first.
 */
export async function searchDocs(
  bundle: LoadedBundle,
  qmdNarrative: NarrativeRetrieval,
  input: SearchDocsInput,
  started: number,
): Promise<DocsResult<SearchDocsData>> {
  const query = input.query.trim();
  if (!query) {
    return errResult(
      "INVALID_INPUT",
      "search query must be non-empty.",
      undefined,
      bundle,
    ) as DocsResult<SearchDocsData>;
  }

  const k = Math.min(50, Math.max(1, input.k ?? 8));
  const normalizedQuery = normalizeEnglishQuery(query);
  const queryVariants = [...new Set([query, normalizedQuery].filter(Boolean))];
  const inferredPackages = inferPackagesFromQuery(query);
  const effectivePackages =
    input.packages && input.packages.length > 0
      ? input.packages
      : inferredPackages;
  const baseQ = {
    query,
    k,
    kinds: input.kinds,
    packages: effectivePackages,
    queryVariants,
  };

  let matchedEntityId: string | undefined;
  let matchedEntityKind: string | undefined;
  if (looksLikeEntityId(query) && bundle.idIndex[query]) {
    matchedEntityId = query;
    const idMeta = bundle.idIndex[query] as { kind?: string } | undefined;
    matchedEntityKind = idMeta?.kind;
  }

  let keywordSingletonId: string | undefined;
  let keywordSingletonKind: string | undefined;
  const firstTok = query.toLowerCase().split(/\s+/)[0] ?? "";
  if (firstTok.length > 1) {
    const ids = bundle.keywordIndex[firstTok];
    if (ids?.length === 1) {
      keywordSingletonId = ids[0];
      const idMeta = bundle.idIndex[ids[0]!] as { kind?: string } | undefined;
      keywordSingletonKind = idMeta?.kind;
    }
  }

  const narrativeHits = await qmdNarrative.search(baseQ);

  const citations: Citation[] = narrativeHits.slice(0, 5).map((h) => ({
    path: h.path,
    id: h.id,
    why: "narrative-hit",
    ...(h.heading ? {} : {}),
  }));

  return okResult(
    bundle,
    {
      narrativeHits,
      ...(matchedEntityId !== undefined ? { matchedEntityId } : {}),
      ...(matchedEntityKind !== undefined ? { matchedEntityKind } : {}),
      ...(keywordSingletonId !== undefined ? { keywordSingletonId } : {}),
      ...(keywordSingletonKind !== undefined ? { keywordSingletonKind } : {}),
      ...(queryVariants.length > 0 ? { queryVariants } : {}),
      ...(inferredPackages.length > 0 ? { inferredPackages } : {}),
      ...(inferredPackages.length > 0
        ? {
            packageSurfaceHints: buildPackageSurfaceHints(
              bundle,
              inferredPackages,
            ),
          }
        : {}),
    },
    "narrative",
    citations,
    started,
  );
}
