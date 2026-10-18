import { useEffect } from "react";
import { registerResources } from "../resourceBundles";
import { registerExternalResourcePreloader } from "../resourceBundles/externalResources";
import type { AsyncResources, LocaleCode, Resources } from "../types";
import { useLocaleCode } from "./useLocaleCode";

type AsyncResourceLoadState = {
  loadedLocales: Set<LocaleCode>;
  pendingLoads: Map<LocaleCode, Promise<void>>;
};

const asyncResourceLoadCache = new WeakMap<
  AsyncResources,
  AsyncResourceLoadState
>();

function getAsyncResourceLoadState(
  resources: AsyncResources,
): AsyncResourceLoadState {
  let state = asyncResourceLoadCache.get(resources);

  if (!state) {
    state = {
      loadedLocales: new Set<LocaleCode>(),
      pendingLoads: new Map<LocaleCode, Promise<void>>(),
    };
    asyncResourceLoadCache.set(resources, state);
  }

  return state;
}

function loadAsyncResources(
  resources: AsyncResources,
  localeCode: LocaleCode,
): Promise<void> {
  const state = getAsyncResourceLoadState(resources);

  if (state.loadedLocales.has(localeCode)) {
    return Promise.resolve();
  }

  const pendingLoad = state.pendingLoads.get(localeCode);

  if (pendingLoad) {
    return pendingLoad;
  }

  const loadPromise = registerResources(resources, localeCode)
    .then(() => {
      state.loadedLocales.add(localeCode);
    })
    .finally(() => {
      state.pendingLoads.delete(localeCode);
    });

  state.pendingLoads.set(localeCode, loadPromise);

  return loadPromise;
}

/**
 * Registers host-provided i18n resources into the shared i18n instance whenever
 * the active locale or `resources` reference changes.
 *
 * - When `resources` is a function, it is registered as a preloader for language
 *   switches and also invoked for the current locale after mount.
 * - When `resources` is a static map, all provided locale bundles are registered.
 *
 * Prefer a stable `resources` reference (e.g. `useCallback` for loaders, module
 * scope or `useMemo` for static maps) to avoid unnecessary re-registration.
 */

export function useRegisterExternalResources(
  resources?: Resources | AsyncResources,
) {
  const localeCode = useLocaleCode();

  useEffect(() => {
    if (typeof resources === "function") {
      // Register the async loader for future language switches. The patched
      // i18n.changeLanguage path calls these preloaders before committing the
      // new language, so plugin/host extension bundles are ready when the UI
      // re-renders in the target locale.
      return registerExternalResourcePreloader((lang) =>
        loadAsyncResources(resources, lang),
      );
    }
  }, [resources]);

  useEffect(() => {
    if (typeof resources === "function") {
      // Load the locale that is already active for this provider. This covers
      // initial mount, detected browser language, direct i18n.changeLanguage()
      // calls, and providers that appear after the app has already changed
      // languages. This effect does not block render or use Suspense.
      loadAsyncResources(resources, localeCode).catch((error) => {
        console.warn("[i18n] failed to register external resources", error);
      });
    } else {
      registerResources(resources, localeCode);
    }
  }, [localeCode, resources]);
}
