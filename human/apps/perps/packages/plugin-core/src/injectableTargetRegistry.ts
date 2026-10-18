/**
 * Static registry of injectable target paths. Used so that __PAXEER_NETWORK_INTERCEPTOR_TARGETS_REGISTRY__
 * and useInterceptorTargets can list all interceptable slots (injectables) as well as plugin interceptors.
 * Module-level Set only; no React, no subscriptions — zero performance impact.
 */

const REGISTERED_PATHS = new Set<string>();

/**
 * Registers an injectable target path. Called from injectable() at component definition time (module load).
 * O(1); does not run in render path.
 */
export function registerInjectableTarget(path: string): void {
  REGISTERED_PATHS.add(path);
}

/**
 * Returns a snapshot of all paths registered via registerInjectableTarget.
 * Path count is typically small; no caching needed.
 */
export function getRegisteredInjectableTargets(): string[] {
  return Array.from(REGISTERED_PATHS);
}
