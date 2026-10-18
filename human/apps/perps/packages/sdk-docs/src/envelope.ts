import type { LoadedBundle } from "./bundle.js";
import { API_VERSION } from "./constants.js";
import type { Citation, DocsMetaOk, DocsResult, MatchKind } from "./types.js";

export type BuildMetaOkInput<T> = {
  bundle: LoadedBundle;
  matchKind: MatchKind;
  queryMs: number;
  citations: Citation[];
  resolutionLevel?: DocsMetaOk<T>["resolutionLevel"];
  degradedReason?: DocsMetaOk<T>["degradedReason"];
  deprecations?: DocsMetaOk<T>["deprecations"];
};

/**
 * Fills version fields from the loaded manifest for every successful tool response.
 */
export function buildMetaOk<T>(input: BuildMetaOkInput<T>): DocsMetaOk<T> {
  const {
    bundle,
    matchKind,
    queryMs,
    citations,
    resolutionLevel,
    degradedReason,
    deprecations,
  } = input;
  return {
    gitSha: bundle.manifest.gitSha,
    generatedAt: bundle.manifest.generatedAt,
    queryMs,
    matchKind,
    apiVersion: API_VERSION,
    citations,
    ...(resolutionLevel !== undefined ? { resolutionLevel } : {}),
    ...(degradedReason !== undefined ? { degradedReason } : {}),
    ...(deprecations !== undefined ? { deprecations } : {}),
  };
}

/** @param started - `performance.now()` or `Date.now()` at query start */
export function elapsedMs(started: number): number {
  return Math.round(Math.max(0, performance.now() - started));
}

/**
 * Successful {@link DocsResult} with manifest-backed metadata.
 */
export function okResult<T>(
  bundle: LoadedBundle,
  data: T,
  matchKind: MatchKind,
  citations: Citation[],
  started: number,
  extra?: Pick<
    DocsMetaOk<T>,
    "resolutionLevel" | "degradedReason" | "deprecations"
  >,
): DocsResult<T> {
  return {
    ok: true,
    data,
    meta: buildMetaOk({
      bundle,
      matchKind,
      queryMs: elapsedMs(started),
      citations,
      ...extra,
    }),
  };
}

type ErrCode = NonNullable<
  Extract<DocsResult<never>, { ok: false }>["error"]
>["code"];

/**
 * Failed {@link DocsResult}; includes manifest version when bundle is available.
 */
export function errResult<T = never>(
  code: ErrCode,
  message: string,
  hint: string | undefined,
  bundle: LoadedBundle | null,
): DocsResult<T> {
  return {
    ok: false,
    error: { code, message, ...(hint !== undefined ? { hint } : {}) },
    meta: {
      ...(bundle
        ? {
            gitSha: bundle.manifest.gitSha,
            generatedAt: bundle.manifest.generatedAt,
          }
        : {}),
      apiVersion: API_VERSION,
    },
  };
}
