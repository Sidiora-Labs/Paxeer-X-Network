import { type FC, useEffect } from "react";
import { I18nextProvider } from "react-i18next";
import { defaultNS } from "../constant";
import { useRegisterExternalResources } from "../hooks/useRegisterExternalResources";
import i18n from "../i18n";
import type { AsyncResources, LocaleCode, Resources } from "../types";
import {
  LanguageProvider,
  type LanguageProviderProps,
} from "./languageProvider";

export type LocaleProviderProps = LanguageProviderProps & {
  /** Active locale; when set and bundles exist, syncs i18n language via `changeLanguage`. */
  locale?: LocaleCode;
  /** Flat key-value messages for `defaultNS`; used together with `locale` when `resources` is not provided. */
  resource?: Record<string, string>;
  /**
   * Preload locale bundles: static `Resources` map, or an async loader (same contract as
   * `ExternalLocaleProvider`). When set, takes precedence over `locale` + `resource`.
   */
  resources?: Resources | AsyncResources;
};

export const LocaleProvider: FC<LocaleProviderProps> = (props) => {
  const { children, locale, resource, resources, ...languageProviderProps } =
    props;
  useRegisterExternalResources(resources);

  useEffect(() => {
    if (!resources && resource && locale) {
      i18n.addResourceBundle(locale, defaultNS, resource, true, true);
    }
  }, [locale, resource, resources]);

  useEffect(() => {
    // change language when locale changed
    if (locale && locale !== i18n.language) {
      i18n.changeLanguage(locale);
    }
  }, [locale]);

  return (
    <LanguageProvider {...languageProviderProps}>
      <I18nextProvider i18n={i18n} defaultNS={defaultNS}>
        {children}
      </I18nextProvider>
    </LanguageProvider>
  );
};
