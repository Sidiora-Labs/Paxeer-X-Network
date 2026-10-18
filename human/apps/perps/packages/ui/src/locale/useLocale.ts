import { useContext, useMemo } from "react";
import { localeValues as defaultLocaleValues } from "./en";
import { Locale, LocaleContext } from "./context";

export type LocaleComponentName = Exclude<keyof Locale, "locale">;

export const useLocale = <C extends LocaleComponentName = LocaleComponentName>(
  componentName: C,
  defaultLocale?: Locale[C] | (() => Locale[C])
): readonly [NonNullable<Locale[C]>, string] => {
  const fullLocale = useContext(LocaleContext);

  const getLocale = useMemo<NonNullable<Locale[C]>>(() => {
    const locale = defaultLocale || defaultLocaleValues[componentName];
    const localeFromContext = fullLocale?.[componentName] || {};
    return {
      ...(typeof locale === "function" ? locale() : locale),
      ...localeFromContext,
    };
  }, [componentName, defaultLocale, fullLocale]);

  const getLocaleCode = useMemo(() => {
    const localeCode = fullLocale?.locale;
    const defaultLocaleCode = defaultLocaleValues.locale;
    return localeCode || defaultLocaleCode;
  }, [fullLocale]);

  return [getLocale, getLocaleCode] as const;
};
