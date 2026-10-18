import { createStore, extractSnippet, type QMDStore } from "@tobilu/qmd";
import fs from "node:fs";
import path from "node:path";
import type { LoadedBundle } from "../bundle.js";
import type { NarrativeHit, NarrativeQuery } from "./narrativeRetrieval.js";
import { NarrativeRetrieval } from "./narrativeRetrieval.js";

let storeCache: { dbPath: string; store: QMDStore } | null = null;
let docMetaCache: {
  indexPath: string;
  map: Record<string, NarrativeFrontmatter>;
} | null = null;

/**
 * Release cached QMD DB (e.g. after tests or switching PAXEER_NETWORK_AI_DOCS_REPO_ROOT).
 */
export function clearQmdStoreCache(): void {
  void storeCache?.store.close().catch(() => {});
  storeCache = null;
  docMetaCache = null;
}

function resolveQmdDbPath(bundle: LoadedBundle): string {
  const qmdMeta = bundle.manifest.qmd;
  const indexRel =
    qmdMeta && typeof qmdMeta.indexPath === "string"
      ? qmdMeta.indexPath
      : "qmd/index.sqlite";
  return path.join(bundle.generatedRoot, indexRel);
}

async function getStore(bundle: LoadedBundle): Promise<QMDStore | null> {
  const dbPath = resolveQmdDbPath(bundle);
  if (!fs.existsSync(dbPath)) return null;
  if (storeCache?.dbPath === dbPath) return storeCache.store;
  if (storeCache) {
    await storeCache.store.close().catch(() => {});
    storeCache = null;
  }
  const store = await createStore({ dbPath });
  storeCache = { dbPath, store };
  return store;
}

function hitToNarrativeHit(
  row: {
    docid: string;
    filepath: string;
    displayPath: string;
    title: string;
    body?: string;
    score: number;
  },
  query: string,
): NarrativeHit {
  const text =
    row.body && row.body.length > 0
      ? extractSnippet(row.body, query, 800).snippet
      : row.title;
  return {
    id: row.docid,
    path: row.displayPath || row.filepath,
    heading: row.title,
    text,
    score: row.score,
  };
}

/** Row shape shared by {@link QMDStore.searchLex} and {@link QMDStore.searchVector}. */
type QmdHitRow = {
  docid: string;
  filepath: string;
  displayPath: string;
  title: string;
  body?: string;
  score: number;
};

type NarrativeFrontmatter = {
  kind?: string;
  domain?: string;
  docType?: string;
  package?: string;
  intentTags?: string[];
  lang?: string;
};

function normalizePackageNeedle(p: string): string {
  return p
    .toLowerCase()
    .replace(/^@paxeer-network\.network\//, "")
    .replace(/^@/, "");
}

function normalizeToken(t: string): string {
  return t.toLowerCase().trim();
}

function parseFrontmatter(filepath: string): NarrativeFrontmatter {
  try {
    if (!fs.existsSync(filepath)) return {};
    const text = fs.readFileSync(filepath, "utf8");
    if (!text.startsWith("---\n")) return {};
    const end = text.indexOf("\n---\n", 4);
    if (end <= 4) return {};
    const block = text.slice(4, end);
    const meta: NarrativeFrontmatter = {};
    for (const line of block.split("\n")) {
      const idx = line.indexOf(":");
      if (idx <= 0) continue;
      const key = line.slice(0, idx).trim();
      const rawValue = line.slice(idx + 1).trim();
      if (!rawValue) continue;
      if (key === "domain") meta.domain = rawValue;
      if (key === "kind") meta.kind = rawValue;
      if (key === "docType") meta.docType = rawValue;
      if (key === "package") meta.package = rawValue;
      if (key === "lang") meta.lang = rawValue.toLowerCase();
      if (key === "intentTags") {
        // Support simple CSV style for compatibility with existing markdown.
        meta.intentTags = rawValue
          .replace(/^\[|\]$/g, "")
          .split(",")
          .map((x) => normalizeToken(x))
          .filter(Boolean);
      }
    }
    return meta;
  } catch {
    return {};
  }
}

function resolveDocMetaIndexPath(bundle: LoadedBundle): string | null {
  const qmd = bundle.manifest.qmd as
    | { docMetaIndexPath?: string; indexPath?: string }
    | undefined;
  const rel = qmd?.docMetaIndexPath;
  if (typeof rel !== "string" || !rel) return null;
  const abs = path.join(bundle.generatedRoot, rel);
  return fs.existsSync(abs) ? abs : null;
}

function getDocMetaMap(
  bundle: LoadedBundle,
): Record<string, NarrativeFrontmatter> | null {
  const idx = resolveDocMetaIndexPath(bundle);
  if (!idx) return null;
  if (docMetaCache?.indexPath === idx) return docMetaCache.map;
  try {
    const map = JSON.parse(fs.readFileSync(idx, "utf8")) as Record<
      string,
      NarrativeFrontmatter
    >;
    docMetaCache = { indexPath: idx, map };
    return map;
  } catch {
    return null;
  }
}

function readDocMeta(
  bundle: LoadedBundle,
  filepath: string,
  displayPath: string,
): NarrativeFrontmatter {
  const map = getDocMetaMap(bundle);
  if (map) {
    const hit = map[filepath] ?? map[displayPath];
    if (hit) return hit;
  }
  // Fallback for backward compatibility when no meta index is present.
  return parseFrontmatter(filepath);
}

function toIntentTokens(query: string): string[] {
  return significantTokens(query).map((t) => normalizeToken(t));
}

function packageMatches(
  metaPackage: string | undefined,
  displayPath: string,
  packages: string[] | undefined,
): boolean {
  if (!packages?.length) return true;
  const needles = packages.map((p) => normalizePackageNeedle(p));
  const candidate = (metaPackage ?? "").toLowerCase();
  const p = displayPath.toLowerCase();
  return needles.some((n) => {
    if (!n) return false;
    return candidate.includes(n) || p.includes(n);
  });
}

function kindMatches(
  meta: NarrativeFrontmatter,
  displayPath: string,
  kinds: string[] | undefined,
): boolean {
  if (!kinds?.length) return true;
  const kindNeedles = kinds.map((k) => normalizeToken(k));
  const hay =
    `${meta.kind ?? ""} ${meta.docType ?? ""} ${meta.domain ?? ""} ${displayPath}`.toLowerCase();
  return kindNeedles.some((k) => hay.includes(k));
}

function isEnglishDoc(meta: NarrativeFrontmatter): boolean {
  // en-only phase: allow untagged docs for backward compatibility.
  if (!meta.lang) return true;
  return meta.lang === "en";
}

function computeIntentBoost(
  row: QmdHitRow,
  meta: NarrativeFrontmatter,
  intentTokens: string[],
): number {
  const p = (row.displayPath || row.filepath).toLowerCase();
  const title = row.title.toLowerCase();
  const domain = (meta.domain ?? "").toLowerCase();
  const kind = (meta.kind ?? "").toLowerCase();
  const tags = (meta.intentTags ?? []).map((x) => x.toLowerCase());
  let boost = 0;
  for (const tok of intentTokens) {
    if (tok.length < 2) continue;
    if (title.includes(tok)) boost += 0.12;
    if (kind.includes(tok)) boost += 0.1;
    if (domain.includes(tok)) boost += 0.1;
    if (tags.some((t) => t.includes(tok))) boost += 0.14;
    if (p.includes(tok)) boost += 0.06;
  }
  // Prefer authoritative docs for layout/plugin implementation questions.
  if (p.includes("packages/plugin-core/doc/guide.md")) boost += 0.12;
  if (p.includes("packages/plugin-core/doc/tech.md")) boost += 0.12;
  if (p.includes("packages/plugin-core/doc/injectable-targets.md"))
    boost += 0.12;
  if (p.includes("packages/layout-core/doc/layout_customization.md"))
    boost += 0.12;
  const isWorkflowDoc =
    p.includes("apps/ai-docs/workflows/") ||
    p.includes("paxeer-network-ai-docs/workflows/");
  if (isWorkflowDoc) {
    // Baseline uplift so curated workflows are easier to surface.
    boost += 0.24;
  }
  // Elevate explicit workflow docs for "how-to/integration/steps" style intent.
  const asksHowTo = intentTokens.some((t) =>
    ["how", "integrate", "integration", "steps", "workflow"].includes(t),
  );
  const asksPluginFlow = intentTokens.some((t) =>
    ["plugin", "layout", "host", "provider"].includes(t),
  );
  if (asksHowTo && kind === "workflow") {
    boost += 0.5;
  }
  if (asksHowTo && asksPluginFlow && isWorkflowDoc) {
    boost += 0.35;
  }
  return boost;
}

function fuseRows(
  bundle: LoadedBundle,
  lexRows: QmdHitRow[],
  vecRows: QmdHitRow[],
  query: string,
  q: NarrativeQuery,
): QmdHitRow[] {
  const lexById = new Map(lexRows.map((r) => [r.docid, r]));
  const vecById = new Map(vecRows.map((r) => [r.docid, r]));
  const ids = new Set<string>([...lexById.keys(), ...vecById.keys()]);
  const out: QmdHitRow[] = [];
  const intentTokens = toIntentTokens(query);

  for (const id of ids) {
    const lex = lexById.get(id);
    const vec = vecById.get(id);
    const base = lex ?? vec;
    if (!base) continue;
    const displayPath = base.displayPath || base.filepath;
    const meta = readDocMeta(bundle, base.filepath, displayPath);
    if (!isEnglishDoc(meta)) continue;
    if (!packageMatches(meta.package, displayPath, q.packages)) continue;
    if (!kindMatches(meta, displayPath, q.kinds)) continue;
    // Weighted fusion: lex is usually more precise for docs query intent.
    const fusedScore =
      (lex?.score ?? 0) * 0.65 +
      (vec?.score ?? 0) * 0.35 +
      computeIntentBoost(base, meta, intentTokens);
    out.push({
      ...base,
      score: fusedScore,
    });
  }

  return out.sort((a, b) => b.score - a.score);
}

/**
 * Whitespace tokens that still contain a letter or digit after QMD-style stripping.
 * Used to approximate OR-style recall: QMD's lex layer ANDs all terms, which often returns zero rows.
 */
function significantTokens(query: string): string[] {
  return query
    .trim()
    .split(/\s+/)
    .filter((t) => t.length > 0 && /[\p{L}\p{N}]/u.test(t));
}

/**
 * QMD-backed narrative search: BM25 + optional vector similarity, with per-token BM25 fallback.
 *
 * Rationale: {@link QMDStore.searchLex} builds an FTS5 query that ANDs terms; multi-word English
 * queries often miss. Merging {@link QMDStore.searchVector} helps when `qmd:index` + `embed` ran;
 * token-wise BM25 improves recall without loading embedding models when vectors are absent.
 */
export class QmdNarrativeRetrieval implements NarrativeRetrieval {
  constructor(private bundle: LoadedBundle) {}

  async search(q: NarrativeQuery): Promise<NarrativeHit[]> {
    const store = await getStore(this.bundle);
    if (!store) return [];

    const k = q.k;
    /** Pull extra candidates before filtering so `packages` / merging still yield `k` hits. */
    const fetchLimit = Math.min(120, Math.max(k * 3, 24));

    let rows: QmdHitRow[] = [];

    try {
      const variants = q.queryVariants?.length ? q.queryVariants : [q.query];
      const pairRows = await Promise.all(
        variants.map(async (variant) => {
          const [lexRows, vecRows] = await Promise.all([
            store
              .searchLex(variant, { limit: fetchLimit })
              .catch(() => [] as QmdHitRow[]),
            store
              .searchVector(variant, { limit: fetchLimit })
              .catch(() => [] as QmdHitRow[]),
          ]);
          return { lexRows, vecRows };
        }),
      );
      const lexRows = pairRows.flatMap((x) => x.lexRows as QmdHitRow[]);
      const vecRows = pairRows.flatMap((x) => x.vecRows as QmdHitRow[]);
      rows = fuseRows(this.bundle, lexRows, vecRows, q.query, q);

      // Per-token BM25: OR-like recall vs QMD's default AND of all terms.
      if (rows.length < k) {
        const tokens = significantTokens(q.query);
        if (tokens.length > 1) {
          for (const tok of tokens) {
            if (rows.length >= fetchLimit) break;
            const part = await store
              .searchLex(tok, { limit: fetchLimit })
              .catch(() => [] as QmdHitRow[]);
            const fusedFallback = fuseRows(
              this.bundle,
              [...rows],
              part as QmdHitRow[],
              q.query,
              q,
            );
            rows = mergedDedupByIdScored([...rows, ...fusedFallback]);
          }
        }
      }

      // Package/kind constraints are already applied in metadata-aware fuseRows.
      rows = rows.slice(0, k);
      return rows.map((r) => {
        const displayPath = r.displayPath || r.filepath;
        const meta = readDocMeta(this.bundle, r.filepath, displayPath);
        const hit = hitToNarrativeHit(r, q.query);
        return {
          ...hit,
          ...(meta.domain ? { domain: meta.domain } : {}),
          ...(meta.docType ? { docType: meta.docType } : {}),
          ...(meta.package ? { packageName: meta.package } : {}),
          ...(meta.intentTags ? { intentTags: meta.intentTags } : {}),
          ...(meta.lang ? { lang: meta.lang } : {}),
        };
      });
    } catch {
      return [];
    }
  }
}

function mergedDedupByIdScored(rows: QmdHitRow[]): QmdHitRow[] {
  const best = new Map<string, QmdHitRow>();
  for (const r of rows) {
    const cur = best.get(r.docid);
    if (!cur || r.score > cur.score) {
      best.set(r.docid, r);
    }
  }
  return [...best.values()].sort((a, b) => b.score - a.score);
}
