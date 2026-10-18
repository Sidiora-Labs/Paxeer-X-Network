import { FC, PropsWithChildren, useEffect } from "react";
import { defaultNS } from "../constant";
import i18n from "../i18n";
import { LocaleCode, Resources } from "../types";
import { useLocaleCode } from "../hooks/useLocaleCode";

/**
 * Async loader contract for pulling translation resources from an external system.
 * Implementations are expected to return the full message table for the given locale
 * and namespace so that the bundle can be replaced atomically.
 */
export type AsyncResources = (
  lang: LocaleCode,
  ns: string,
) => Promise<Record<string, string>>;

/**
 * Helper that normalizes async resource loading and registration semantics.
 * Uses `addResourceBundle` with `deep` merge and `overwrite` enabled to keep
 * the in-memory bundle in sync with the external source of truth.
 */
const asyncAddResource = async (
  localeCode: LocaleCode,
  resources: AsyncResources,
) => {
  const resource = await resources(localeCode, defaultNS);
  i18n.addResourceBundle(localeCode, defaultNS, resource, true, true);
};

export type ExternalLocaleProviderProps = PropsWithChildren<{
  resources?: Resources | AsyncResources;
}>;

/**
 * ExternalLocaleProvider lets host applications inject i18n resources that live
 * outside of this package (e.g. from another bundle, backend, or runtime loader).
 *
 * - When `resources` is a function, it will be invoked whenever the locale changes
 *   to lazily fetch the latest bundle for that locale.
 * - When `resources` is a static map, all provided locale bundles are registered
 *   synchronously on mount.
 *
 * This component renders no UI; it only manages i18n side effects and simply
 * returns its children.
 */
export const ExternalLocaleProvider: FC<ExternalLocaleProviderProps> = (
  props,
) => {
  const { resources } = props;
  const localeCode = useLocaleCode();

  useEffect(() => {
    // async load resources
    if (typeof resources === "function") {
      asyncAddResource(localeCode, resources);
      return;
    }

    // sync load resources
    if (resources) {
      Object.entries(resources).forEach(([locale, messages]) => {
        i18n.addResourceBundle(locale, defaultNS, messages, true, true);
      });
      return;
    }
  }, [localeCode, resources]);

  return <>{props.children}</>;
};
