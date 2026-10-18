export { API_VERSION } from "./constants.js";
export {
  type LoadedBundle,
  type Manifest,
  type ComponentDocIndexEntry,
  clearLoadBundleCache,
  loadBundle,
  readEntityJson,
} from "./bundle.js";
export {
  type ResolveAiDocsRepoRootOptions,
  aiDocsGeneratedRoot,
  assertSafeRelPath,
  manifestExistsAtRepoRoot,
  resolveAiDocsRepoRoot,
  resolveBundledSyntheticRepoRoot,
  resolveRepoRoot,
} from "./paths.js";
export type {
  Citation,
  DocsMetaOk,
  DocsResult,
  MatchKind,
  NarrativeHit,
} from "./types.js";
export type {
  NarrativeQuery,
  NarrativeRetrieval,
} from "./narrative/narrativeRetrieval.js";
export {
  QmdNarrativeRetrieval,
  clearQmdStoreCache,
} from "./narrative/qmdAdapter.js";
export { buildMetaOk, elapsedMs, errResult, okResult } from "./envelope.js";
export { resolveEntityByArtifactKind } from "./entityResolve.js";
export {
  getComponent,
  getHook,
  getPackageSurface,
  getReleaseContext,
  getType,
  type PackageSurfaceData,
  type ReleaseContextData,
} from "./exactLookup.js";
export { getComponentDoc, resolveComponentDocAbsPath } from "./componentDoc.js";
export {
  getGuardrails,
  getRecipe,
  getWorkflow,
  type NarrativeDocBody,
} from "./narrativeDocs.js";
export {
  searchDocs,
  type SearchDocsData,
  type SearchDocsInput,
} from "./searchDocs.js";
export {
  createAiDocsFacade,
  type AiDocsFacade,
  type AiDocsFacadeOptions,
} from "./facade.js";
export { createClientAdapterRegistry } from "./install/adapterRegistry.js";
export {
  builtinClientAdapters,
  defaultClientAdapterRegistry,
} from "./install/clients.js";
export type { ClientAdapterRegistry } from "./install/types.js";
export {
  buildGithubBlobUrl,
  buildGithubRawUrl,
  fetchSdkSource,
  type FetchSdkSourceInput,
  type SdkGithubSourceData,
  type SdkGithubSourceExcerpt,
} from "./fetchSdkSource.js";
