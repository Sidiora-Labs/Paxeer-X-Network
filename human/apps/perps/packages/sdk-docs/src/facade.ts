import type { LoadedBundle } from "./bundle.js";
import { getComponentDoc } from "./componentDoc.js";
import {
  getComponent,
  getHook,
  getPackageSurface,
  getReleaseContext,
  getType,
  type PackageSurfaceData,
  type ReleaseContextData,
} from "./exactLookup.js";
import {
  fetchSdkSource,
  type FetchSdkSourceInput,
  type SdkGithubSourceData,
} from "./fetchSdkSource.js";
import type { NarrativeRetrieval } from "./narrative/narrativeRetrieval.js";
import { QmdNarrativeRetrieval } from "./narrative/qmdAdapter.js";
import {
  getGuardrails,
  getRecipe,
  getWorkflow,
  type NarrativeDocBody,
} from "./narrativeDocs.js";
import {
  searchDocs,
  type SearchDocsData,
  type SearchDocsInput,
} from "./searchDocs.js";
import type { DocsResult } from "./types.js";

export type AiDocsFacadeOptions = {
  /** Override QMD retrieval; defaults to {@link QmdNarrativeRetrieval}. */
  qmdNarrative?: NarrativeRetrieval;
};

/**
 * paxeer-network-facing docs access object: exact entity lookup, narrative docs, and QMD-backed search.
 */
export type AiDocsFacade = {
  readonly bundle: LoadedBundle;
  getComponent: (query: string) => DocsResult<Record<string, unknown>>;
  getHook: (query: string) => DocsResult<Record<string, unknown>>;
  getType: (query: string) => DocsResult<Record<string, unknown>>;
  getPackageSurface: (packageName: string) => DocsResult<PackageSurfaceData>;
  getReleaseContext: () => DocsResult<ReleaseContextData>;
  getWorkflow: (slug: string) => DocsResult<NarrativeDocBody>;
  getRecipe: (name: string) => DocsResult<NarrativeDocBody>;
  getGuardrails: () => DocsResult<NarrativeDocBody>;
  getComponentDoc: (query: string) => DocsResult<NarrativeDocBody>;
  searchDocs: (input: SearchDocsInput) => Promise<DocsResult<SearchDocsData>>;
  /** Fetches source from GitHub (PaxeerNetworkNetwork/js-sdk) at manifest ref or env overrides. */
  fetchSdkSource: (
    input: FetchSdkSourceInput,
  ) => Promise<DocsResult<SdkGithubSourceData>>;
};

/**
 * @param bundle - Typically from {@link loadBundle}; reused for all calls.
 */
export function createAiDocsFacade(
  bundle: LoadedBundle,
  opts?: AiDocsFacadeOptions,
): AiDocsFacade {
  const qmdNarrative = opts?.qmdNarrative ?? new QmdNarrativeRetrieval(bundle);

  return {
    bundle,
    getComponent: (query: string) =>
      getComponent(bundle, query, performance.now()),
    getHook: (query: string) => getHook(bundle, query, performance.now()),
    getType: (query: string) => getType(bundle, query, performance.now()),
    getPackageSurface: (packageName: string) =>
      getPackageSurface(bundle, packageName, performance.now()),
    getReleaseContext: () => getReleaseContext(bundle, performance.now()),
    getWorkflow: (slug: string) => getWorkflow(bundle, slug, performance.now()),
    getRecipe: (name: string) => getRecipe(bundle, name, performance.now()),
    getGuardrails: () => getGuardrails(bundle, performance.now()),
    getComponentDoc: (query: string) =>
      getComponentDoc(bundle, query, performance.now()),
    searchDocs: (input: SearchDocsInput) =>
      searchDocs(bundle, qmdNarrative, input, performance.now()),
    fetchSdkSource: (input: FetchSdkSourceInput) =>
      fetchSdkSource(bundle, input, performance.now()),
  };
}

export type {
  SearchDocsInput,
  SearchDocsData,
  NarrativeDocBody,
  PackageSurfaceData,
  ReleaseContextData,
  FetchSdkSourceInput,
  SdkGithubSourceData,
};
