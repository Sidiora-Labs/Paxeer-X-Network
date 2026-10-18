import { LocaleEnum } from "./constant";
import i18n from "./i18n";
import type { LocaleCode } from "./types";

/**
 * Whether the active UI language uses right-to-left layout for `document.dir`.
 * Matches primary subtags and common regional variants (e.g. `fa-IR`, `he-IL`).
 */
export const isRTLDirectionLanguage = (lang?: string) =>
  /^(?:ar|fa|he)(?:-|$)/i.test(lang ?? "");

export const syncDocumentDirection = (lang?: string) => {
  if (typeof document === "undefined") {
    return;
  }

  document.documentElement.setAttribute(
    "dir",
    isRTLDirectionLanguage(lang) ? "rtl" : "ltr",
  );
};

/**
 * transform browser language to you given i18n locale codes
 * @param lang - browser language
 * @param localeCodes - locale codes to check
 * @param defaultLang - default locale code
 * @example
 * parseI18nLang('en-US') => 'en'
 * parseI18nLang('zh-CN') => 'zh'
 * parseI18nLang('zh-TW') => 'zh'
 * parseI18nLang('ja') => 'ja'
 * */
export function parseI18nLang(
  lang: string,
  localeCodes?: LocaleCode[],
  defaultLang?: LocaleCode,
) {
  localeCodes = localeCodes || Object.values(LocaleEnum);
  defaultLang = defaultLang || LocaleEnum.en;

  const regex = /^([a-z]{2})/i;
  const match = lang?.match(regex);

  if (!match) {
    return defaultLang;
  }

  const matchLang = match[1];

  if (localeCodes.includes(lang)) {
    return lang;
  }

  if (localeCodes.includes(matchLang)) {
    return matchLang;
  }

  return defaultLang;
}

/**
 * remove lang prefix from pathname
 * @param pathname - pathname to remove lang prefix
 * @param localeCodes - locale codes to check
 * @example
 * removeLangPrefix('/en/perp/PERP_ETH_USDC') => '/perp/PERP_ETH_USDC'
 * removeLangPrefix('/en/markets') => '/markets'
 * removeLangPrefix('/perp/PERP_ETH_USDC') => '/perp/PERP_ETH_USDC'
 * removeLangPrefix('/markets') => '/markets'
 */
export function removeLangPrefix(pathname: string, localeCodes?: string[]) {
  const localePath = getLocalePathFromPathname(pathname, localeCodes);

  return localePath
    ? pathname.replace(new RegExp(`^/${localePath}(?=/)`), "")
    : pathname;
}

/**
 * get locale path from pathname
 * @param pathname - pathname to get locale path
 * @param localeCodes - locale codes to check
 * @example
 * getLocalePathFromPathname('/en/perp/PERP_ETH_USDC') => 'en'
 * getLocalePathFromPathname('/perp/PERP_ETH_USDC') => null
 * getLocalePathFromPathname('/en/markets') => 'en'
 * getLocalePathFromPathname('/markets') => null
 */
export function getLocalePathFromPathname(
  pathname: string,
  localeCodes?: string[],
) {
  const locale = pathname.split("/")[1];
  localeCodes = localeCodes || Object.values(LocaleEnum);
  return localeCodes.includes(locale as LocaleEnum) ? locale : null;
}

/**
 * Generate a localized path with proper locale prefix and search parameters
 *
 * This function ensures that the returned path includes the appropriate locale prefix.
 * If the path already contains a valid locale prefix, it returns the path as-is.
 * Otherwise, it prepends the specified locale or falls back to the current i18n language.
 *
 * @param params - Configuration object for path generation
 * @param params.path - The base pathname (e.g., '/markets', '/perp/PERP_ETH_USDC')
 * @param params.locale - Optional locale code to use as prefix. If not provided, uses i18n.language
 * @param params.search - Optional search query string. If not provided, uses window.location.search
 *
 * @returns A complete URL path with locale prefix and search parameters
 *
 * @example
 * generatePath({ path: '/markets' }) => '/en/markets?tab=spot'
 * generatePath({ path: '/en/markets', search: '?tab=futures' }) => '/en/markets?tab=futures'
 * generatePath({ path: '/perp/PERP_ETH_USDC', locale: 'zh' }) => '/zh/perp/PERP_ETH_USDC'
 * generatePath({ path: '/en/perp/PERP_ETH_USDC' }) => '/en/perp/PERP_ETH_USDC'
 */
export function generatePath(params: {
  path: string;
  locale?: string;
  search?: string;
}) {
  const { path, locale, search } = params;
  const searchUrl =
    search || (typeof window !== "undefined" ? window.location.search : "");

  let localePath = getLocalePathFromPathname(path);

  // If path already contains a valid locale prefix, return it unchanged
  if (localePath) {
    return `${path}${searchUrl}`;
  }

  // Use provided locale or fall back to current i18n language
  localePath = locale || parseI18nLang(i18n.language);

  // Prepend locale prefix to path
  return `/${localePath}${path}${searchUrl}`;
}
